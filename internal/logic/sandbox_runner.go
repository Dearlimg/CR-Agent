package logic

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"CR-Agent/internal/model"
)

const (
	e2bRunnerResultPrefix = "CR_AGENT_E2B_RESULT:"
	e2bOutputMaxChars     = 12000
	e2bMaxTestTimeout     = 600
)

//go:embed e2b_runner.py
var e2bRunnerScript []byte

type sandboxTestCommand struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

type sandboxTestResult struct {
	Status  string
	Message string
	Output  string
	Ran     bool
}

type sandboxRunnerResponse struct {
	Ran     bool   `json:"ran"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

func (s *Service) runSandboxTests(
	ctx context.Context,
	job *model.ReviewJob,
	snapshot *reviewSourceSnapshot,
	recorder *TraceRecorder,
) sandboxTestResult {
	traceInput := "固定 head 不可用"
	if snapshot != nil && snapshot.headSHA != "" {
		traceInput = "head_sha=" + snapshot.headSHA
	}
	span := recorder.Start("tool", "automated_tests", "sandbox", traceInput, "")
	result := sandboxTestResult{Status: "not_run", Message: "未配置 E2B_API_KEY，自动化测试未运行。"}
	switch {
	case strings.TrimSpace(s.Config.E2BAPIKey) == "":
	case s.Loop == nil || s.Loop.Policy == nil || s.Loop.Policy.Decide(PermissionSandboxExec) != PermissionAllow:
		result.Message = "沙箱执行权限未授予，自动化测试未运行。"
	case snapshot == nil:
		result.Message = "当前来源没有可用的 GitHub PR 固定 head，自动化测试未运行。"
	default:
		archive, err := downloadReviewSourceArchive(ctx, snapshot)
		if err != nil {
			result.Status = "incomplete"
			result.Message = "无法安全获取 PR 固定 head 的完整源码，自动化测试未运行。"
			result.Output = redact(err.Error())
			break
		}
		files, err := reviewSourceArchiveFiles(archive)
		if err != nil {
			result.Status = "incomplete"
			result.Message = "无法读取仓库测试配置，自动化测试未运行。"
			result.Output = redact(err.Error())
			break
		}
		commands, err := detectSandboxTestCommands(files)
		if err != nil {
			result.Status = "incomplete"
			result.Message = "读取仓库测试配置失败，自动化测试未运行。"
			result.Output = redact(err.Error())
			break
		}
		if len(commands) == 0 {
			result.Message = "未发现受支持的自动化测试配置，自动化测试未运行。"
			break
		}
		result = runE2BSandboxTests(ctx, s.Config, archive, commands)
	}
	output := result.Message
	if result.Output != "" {
		output += "\n" + result.Output
	}
	output = clipSandboxOutput(redact(output))
	callError := ""
	if result.Status == "failed" || result.Status == "incomplete" {
		callError = result.Message
	}
	span.End(TraceResult{Status: result.Status, Output: output, Origin: "sandbox"})
	_ = s.Store.RecordToolCall(job.ID, span.ID(), "automated_tests", result.Status, traceInput, output, callError, span.DurationMs())
	return result
}

// runE2BSandboxTests writes a normalized, fixed-commit checkout into a remote
// E2B sandbox and invokes only the test commands selected by the host planner.
func runE2BSandboxTests(
	ctx context.Context,
	cfg Config,
	archive []byte,
	commands []sandboxTestCommand,
) sandboxTestResult {
	if cfg.E2BTestTimeoutSeconds <= 0 {
		cfg.E2BTestTimeoutSeconds = 600
	}
	testTimeout := min(cfg.E2BTestTimeoutSeconds, e2bMaxTestTimeout)
	totalTimeout := time.Duration(testTimeout+120) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	archiveFile, err := os.CreateTemp("", "cr-agent-review-source-*.zip")
	if err != nil {
		return sandboxTestResult{Status: "incomplete", Message: "无法创建临时源码归档，自动化测试未运行。"}
	}
	archivePath := archiveFile.Name()
	defer os.Remove(archivePath)
	if _, err := archiveFile.Write(archive); err != nil {
		_ = archiveFile.Close()
		return sandboxTestResult{Status: "incomplete", Message: "无法写入临时源码归档，自动化测试未运行。"}
	}
	if err := archiveFile.Close(); err != nil {
		return sandboxTestResult{Status: "incomplete", Message: "无法完成临时源码归档，自动化测试未运行。"}
	}

	commandsJSON, err := json.Marshal(commands)
	if err != nil {
		return sandboxTestResult{Status: "incomplete", Message: "无法构造沙箱测试计划，自动化测试未运行。"}
	}
	scriptFile, err := os.CreateTemp("", "cr-agent-e2b-runner-*.py")
	if err != nil {
		return sandboxTestResult{Status: "incomplete", Message: "无法准备 E2B SDK 启动器，自动化测试未运行。"}
	}
	scriptPath := scriptFile.Name()
	defer os.Remove(scriptPath)
	if err := scriptFile.Chmod(0o600); err != nil {
		_ = scriptFile.Close()
		return sandboxTestResult{Status: "incomplete", Message: "无法保护 E2B SDK 启动器，自动化测试未运行。"}
	}
	if _, err := scriptFile.Write(e2bRunnerScript); err != nil {
		_ = scriptFile.Close()
		return sandboxTestResult{Status: "incomplete", Message: "无法写入 E2B SDK 启动器，自动化测试未运行。"}
	}
	if err := scriptFile.Close(); err != nil {
		return sandboxTestResult{Status: "incomplete", Message: "无法完成 E2B SDK 启动器，自动化测试未运行。"}
	}

	python := strings.TrimSpace(cfg.E2BPythonExecutable)
	if python == "" {
		python = "python3"
		if runtime.GOOS == "windows" {
			python = "python"
		}
	}
	args := []string{scriptPath, archivePath, string(commandsJSON), fmt.Sprint(testTimeout)}
	command := exec.CommandContext(runCtx, python, args...)
	command.Env = e2bRunnerEnvironment(cfg)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		message := "E2B SDK 调用失败，自动化测试未能完成。"
		if runCtx.Err() != nil {
			message = "E2B 沙箱测试超过宿主执行时限，自动化测试未能完成。"
		} else if _, ok := err.(*exec.Error); ok {
			message = "找不到 Python 运行时，请安装 requirements-sandbox.txt 中的依赖。"
		}
		return sandboxTestResult{
			Status: "incomplete", Message: message,
		}
	}
	response, err := parseSandboxRunnerResponse(stdout.Bytes())
	if err != nil {
		return sandboxTestResult{
			Status: "incomplete", Message: "E2B SDK 未返回可识别的测试结果。",
		}
	}
	return sandboxTestResult{
		Status: response.Status, Message: response.Message,
		Output: response.Output, Ran: response.Ran,
	}
}

func e2bRunnerEnvironment(cfg Config) []string {
	allowed := map[string]string{}
	for _, name := range []string{
		"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP",
		"USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOMEDRIVE", "HOMEPATH",
	} {
		if value, ok := os.LookupEnv(name); ok {
			allowed[name] = value
		}
	}
	allowed["E2B_API_KEY"] = strings.TrimSpace(cfg.E2BAPIKey)
	allowed["E2B_DOMAIN"] = strings.TrimSpace(cfg.E2BDomain)
	allowed["AGS_TEMPLATE"] = strings.TrimSpace(cfg.E2BTemplate)
	allowed["E2B_TEMPLATE"] = strings.TrimSpace(cfg.E2BTemplate)
	environment := make([]string, 0, len(allowed))
	for name, value := range allowed {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func parseSandboxRunnerResponse(output []byte) (sandboxRunnerResponse, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if !strings.HasPrefix(line, e2bRunnerResultPrefix) {
			continue
		}
		var result sandboxRunnerResponse
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, e2bRunnerResultPrefix)), &result); err != nil {
			return sandboxRunnerResponse{}, fmt.Errorf("解析 E2B runner 响应失败")
		}
		if result.Status != "passed" && result.Status != "failed" && result.Status != "incomplete" && result.Status != "not_run" {
			return sandboxRunnerResponse{}, fmt.Errorf("E2B runner 返回无效状态")
		}
		return result, nil
	}
	return sandboxRunnerResponse{}, fmt.Errorf("E2B runner 响应缺少结果标记")
}

func clipSandboxOutput(output string) string {
	output = strings.ToValidUTF8(output, "\uFFFD")
	if len(output) <= e2bOutputMaxChars {
		return output
	}
	clipped := output[:e2bOutputMaxChars]
	for !utf8.ValidString(clipped) {
		clipped = clipped[:len(clipped)-1]
	}
	return clipped + "\n[输出已截断]"
}

func reviewSourceArchiveFiles(archive []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("读取规范化源码归档失败")
	}
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("读取规范化源码归档失败")
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, reviewSourceArchiveMaxFileBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(content) > reviewSourceArchiveMaxFileBytes {
			return nil, fmt.Errorf("读取规范化源码归档文件失败")
		}
		files[file.Name] = content
	}
	return files, nil
}

func detectSandboxTestCommands(files map[string][]byte) ([]sandboxTestCommand, error) {
	commands := make([]sandboxTestCommand, 0, 5)
	hasPythonTests := false
	for filePath := range files {
		name := filepath.Base(filePath)
		if strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py") ||
			strings.HasSuffix(name, "_test.py") {
			hasPythonTests = true
		}
	}
	if _, ok := files["go.mod"]; ok {
		for filePath := range files {
			if strings.HasSuffix(filepath.Base(filePath), "_test.go") {
				commands = append(commands, sandboxTestCommand{Name: "Go", Args: []string{"go", "test", "-count=1", "./..."}})
				break
			}
		}
	}
	if hasPythonTests {
		commands = append(commands, sandboxTestCommand{Name: "Python", Args: []string{"python3", "-m", "pytest", "-q"}})
	}
	if packageData, ok := files["package.json"]; ok {
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(packageData, &manifest); err != nil {
			return nil, fmt.Errorf("package.json 不是有效 JSON")
		}
		if strings.TrimSpace(manifest.Scripts["test"]) != "" {
			commands = append(commands, sandboxTestCommand{Name: "Node.js", Args: []string{"npm", "test"}})
		}
	}
	if _, ok := files["Cargo.toml"]; ok {
		commands = append(commands, sandboxTestCommand{Name: "Rust", Args: []string{"cargo", "test"}})
	}
	if _, ok := files["pom.xml"]; ok {
		args := []string{"mvn", "test", "-B"}
		if _, hasWrapper := files["mvnw"]; hasWrapper {
			args = []string{"./mvnw", "test", "-B"}
		}
		commands = append(commands, sandboxTestCommand{Name: "Maven", Args: args})
	} else if _, hasGradle := files["build.gradle"]; hasGradle {
		args := []string{"gradle", "test", "--no-daemon"}
		if _, hasWrapper := files["gradlew"]; hasWrapper {
			args = []string{"./gradlew", "test", "--no-daemon"}
		}
		commands = append(commands, sandboxTestCommand{Name: "Gradle", Args: args})
	} else if _, hasGradle := files["build.gradle.kts"]; hasGradle {
		args := []string{"gradle", "test", "--no-daemon"}
		if _, hasWrapper := files["gradlew"]; hasWrapper {
			args = []string{"./gradlew", "test", "--no-daemon"}
		}
		commands = append(commands, sandboxTestCommand{Name: "Gradle", Args: args})
	}
	return commands, nil
}
