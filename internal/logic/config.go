package logic

import (
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var quotedCredentialValuePattern = regexp.MustCompile(
	`(?i)((?:api[_-]?key|secret|password|token|authorization)["']?\s*(?::=|=|:)\s*)(["'])([^"'\r\n]+)(["'])`,
)

var bareCredentialValuePattern = regexp.MustCompile(
	`(?i)((?:api[_-]?key|secret|password|token|authorization)["']?\s*)(:=|=|:)(\s*)([A-Za-z0-9_\-/+.][A-Za-z0-9_\-/+=.]*)`,
)

var bearerCredentialValuePattern = regexp.MustCompile(`(?i)(\bBearer\s+)([A-Za-z0-9._~+/-]+)`)

type Config struct {
	Port                 string
	PersistenceMode      string
	SkillsDir            string
	MemoryDir            string
	TasksDir             string
	BackgroundTasksDir   string
	CronFile             string
	TeamMailboxDir       string
	TeamMaxConcurrency   int
	ReviewModeOverride   string // Only set by controlled evaluations; empty uses automatic routing.
	ModelMaxRetries      int
	ModelRetryBaseMs     int
	CronPollIntervalMs   int
	MemoryMaxRecall      int
	MemoryMaxChars       int
	MemoryConsolidateAt  int
	ContextCharLimit     int
	ToolResultBudget     int
	LargeResultCharLimit int
	ContextMaxMessages   int
	ContextOutputDir     string
	ContextTranscriptDir string
	WorkflowDir          string
	GoalMaxBlocks        int
	DeepSeekAPIKey       string
	DeepSeekBaseURL      string
	GitHubToken          string
	GitHubAPIBase        string
	MySQLDSN             string
	RedisAddr            string
	RedisPassword        string
	BudgetCents          int
}

func LoadConfig() Config {
	_ = godotenv.Load()
	b := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	n, _ := strconv.Atoi(b("REVIEW_BUDGET_CENTS", "1000"))
	return Config{
		Port:                 b("PORT", "8080"),
		PersistenceMode:      b("PERSISTENCE_MODE", "mysql"),
		SkillsDir:            b("AGENT_SKILLS_DIR", "skills"),
		MemoryDir:            b("AGENT_MEMORY_DIR", ".memory"),
		TasksDir:             b("AGENT_TASKS_DIR", ".tasks"),
		BackgroundTasksDir:   b("AGENT_BACKGROUND_TASKS_DIR", ".background-tasks"),
		CronFile:             b("AGENT_CRON_FILE", ".cron-jobs.json"),
		TeamMailboxDir:       b("AGENT_TEAM_MAILBOX_DIR", ".team-mailboxes"),
		TeamMaxConcurrency:   intEnv(b, "AGENT_TEAM_MAX_CONCURRENCY", 2),
		ModelMaxRetries:      intEnv(b, "MODEL_MAX_RETRIES", 2),
		ModelRetryBaseMs:     intEnv(b, "MODEL_RETRY_BASE_MS", 500),
		CronPollIntervalMs:   intEnv(b, "CRON_POLL_INTERVAL_MS", 1000),
		MemoryMaxRecall:      intEnv(b, "MEMORY_MAX_RECALL", 5),
		MemoryMaxChars:       intEnv(b, "MEMORY_MAX_CHARS", 6000),
		MemoryConsolidateAt:  intEnv(b, "MEMORY_CONSOLIDATE_AT", 10),
		ContextCharLimit:     intEnv(b, "CONTEXT_CHAR_LIMIT", 50000),
		ToolResultBudget:     intEnv(b, "TOOL_RESULT_BUDGET", 200000),
		LargeResultCharLimit: intEnv(b, "LARGE_RESULT_CHAR_LIMIT", 30000),
		ContextMaxMessages:   intEnv(b, "CONTEXT_MAX_MESSAGES", 50),
		ContextOutputDir:     b("CONTEXT_OUTPUT_DIR", ".task_outputs/tool-results"),
		ContextTranscriptDir: b("CONTEXT_TRANSCRIPT_DIR", ".transcripts"),
		WorkflowDir:          b("AGENT_WORKFLOW_DIR", ".workflows"),
		GoalMaxBlocks:        intEnv(b, "GOAL_MAX_BLOCKS", 6),
		DeepSeekAPIKey:       b("DEEPSEEK_API_KEY", ""),
		DeepSeekBaseURL:      b("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		GitHubToken:          b("GITHUB_TOKEN", ""),
		GitHubAPIBase:        b("GITHUB_API_BASE", "https://api.github.com"),
		MySQLDSN:             b("MYSQL_DSN", ""),
		RedisAddr:            b("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:        b("REDIS_PASSWORD", ""),
		BudgetCents:          n,
	}
}

func intEnv(get func(string, string) string, key string, fallback int) int {
	value, err := strconv.Atoi(get(key, strconv.Itoa(fallback)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
func redactReviewInput(s string) string {
	s = quotedCredentialValuePattern.ReplaceAllString(s, "${1}${2}[REDACTED]${4}")
	s = bearerCredentialValuePattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = bareCredentialValuePattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := bareCredentialValuePattern.FindStringSubmatch(match)
		if len(parts) != 5 {
			return "[REDACTED]"
		}
		// Preserve Go identifiers and call expressions. Quoted literals are still
		// masked above, and recognizable provider tokens are masked below.
		if parts[2] == ":=" && simpleCodeExpression(parts[4]) {
			return match
		}
		return parts[1] + parts[2] + parts[3] + "[REDACTED]"
	})
	return providerTokenPattern.ReplaceAllString(s, "[REDACTED]")
}

func redact(s string) string {
	lines := strings.Split(s, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "api_key") || strings.Contains(lower, "api-key") ||
			strings.Contains(lower, "apikey") || strings.Contains(lower, "password") ||
			strings.Contains(lower, "secret") || strings.Contains(lower, "authorization") ||
			strings.Contains(lower, "token") || providerTokenPattern.MatchString(line) ||
			credentialPattern.MatchString(line) {
			lines[index] = "[REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}

func simpleCodeExpression(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}
func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }
func envExample()             { _ = fmt.Sprintf("") }
