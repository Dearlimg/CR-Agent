package logic

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"CR-Agent/internal/model"
)

const reviewCheckpointVersion = 1

const (
	reviewStageAccepted       = "accepted"
	reviewStagePreflight      = "preflight_completed"
	reviewStageContext        = "context_prepared"
	reviewStageCandidates     = "candidates_ready"
	reviewStageFindingContext = "finding_context_prepared"
	reviewStageVerification   = "finding_verification"
	reviewStageFinalizing     = "finalizing"
	reviewStageCompleted      = "completed"
)

type reviewCheckpoint struct {
	Version                 int                      `json:"version"`
	Stage                   string                   `json:"stage"`
	Request                 model.ReviewRequest      `json:"request"`
	Diff                    string                   `json:"sanitized_diff,omitempty"`
	Artifacts               ReviewArtifacts          `json:"artifacts"`
	PromptContext           ReviewPromptContext      `json:"prompt_context"`
	InitialSource           checkpointSource         `json:"initial_source"`
	Tests                   sandboxTestResult        `json:"tests"`
	Candidates              []ReviewFinding          `json:"candidates"`
	ModelTraceID            string                   `json:"model_trace_id,omitempty"`
	CandidateSource         checkpointSource         `json:"candidate_source"`
	SourceContextError      string                   `json:"source_context_error,omitempty"`
	RejectedEvidence        int                      `json:"rejected_evidence"`
	IncompleteReason        string                   `json:"incomplete_reason,omitempty"`
	LastVerificationError   string                   `json:"last_verification_error,omitempty"`
	VerificationCursor      int                      `json:"verification_cursor"`
	VerificationResults     []checkpointVerification `json:"verification_results"`
	MemoryExtractionStarted bool                     `json:"memory_extraction_started"`
	FinalStatus             string                   `json:"final_status,omitempty"`
	FinalOutcome            string                   `json:"final_outcome,omitempty"`
	FinalError              string                   `json:"final_error,omitempty"`
}

type checkpointSource struct {
	Owner   string            `json:"owner,omitempty"`
	Repo    string            `json:"repo,omitempty"`
	HeadSHA string            `json:"head_sha,omitempty"`
	Files   map[string]string `json:"files,omitempty"`
}

type checkpointVerification struct {
	Finding ReviewFinding `json:"finding"`
	Verdict string        `json:"verdict"`
}

func newReviewCheckpoint(request model.ReviewRequest) reviewCheckpoint {
	request = safeCheckpointRequest(request)
	return reviewCheckpoint{
		Version:             reviewCheckpointVersion,
		Stage:               reviewStageAccepted,
		Request:             request,
		Artifacts:           ReviewArtifacts{Files: []ChangedFile{}, DependencyFiles: []string{}, Checks: []PreflightCheck{}, SecretFindings: []SecretFinding{}},
		Candidates:          []ReviewFinding{},
		VerificationResults: []checkpointVerification{},
	}
}

func safeCheckpointRequest(request model.ReviewRequest) model.ReviewRequest {
	request.Source = redactReviewInput(strings.TrimSpace(request.Source))
	request.MemoryQuery = redactReviewInput(request.MemoryQuery)
	request.Goal = redactReviewInput(request.Goal)
	request.Diff = ""
	return request
}

func loadReviewCheckpoint(job *model.ReviewJob) (reviewCheckpoint, error) {
	if strings.TrimSpace(job.CheckpointJSON) == "" {
		return reviewCheckpoint{}, fmt.Errorf("审查任务缺少可恢复检查点")
	}
	var checkpoint reviewCheckpoint
	if err := json.Unmarshal([]byte(job.CheckpointJSON), &checkpoint); err != nil {
		return reviewCheckpoint{}, fmt.Errorf("解析审查检查点: %w", err)
	}
	if checkpoint.Version != reviewCheckpointVersion {
		return reviewCheckpoint{}, fmt.Errorf("不支持的审查检查点版本 %d", checkpoint.Version)
	}
	if reviewStageOrder(checkpoint.Stage) < 0 {
		return reviewCheckpoint{}, fmt.Errorf("审查检查点阶段无效: %q", checkpoint.Stage)
	}
	if checkpoint.Candidates == nil {
		checkpoint.Candidates = []ReviewFinding{}
	}
	if checkpoint.VerificationResults == nil {
		checkpoint.VerificationResults = []checkpointVerification{}
	}
	if checkpoint.VerificationCursor < 0 || checkpoint.VerificationCursor > len(checkpoint.Candidates) ||
		checkpoint.VerificationCursor != len(checkpoint.VerificationResults) {
		return reviewCheckpoint{}, fmt.Errorf("审查检查点的复核游标与结果数量不一致")
	}
	checkpoint.Artifacts.SanitizedDiff = checkpoint.Diff
	return checkpoint, nil
}

func persistReviewCheckpoint(job *model.ReviewJob, checkpoint reviewCheckpoint) error {
	checkpoint.Artifacts.SanitizedDiff = ""
	content, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("编码审查检查点: %w", err)
	}
	job.CheckpointJSON = string(content)
	return nil
}

func checkpointReached(checkpoint reviewCheckpoint, stage string) bool {
	return reviewStageOrder(checkpoint.Stage) >= reviewStageOrder(stage)
}

func reviewStageOrder(stage string) int {
	switch stage {
	case reviewStageAccepted:
		return 0
	case reviewStagePreflight:
		return 1
	case reviewStageContext:
		return 2
	case reviewStageCandidates:
		return 3
	case reviewStageFindingContext:
		return 4
	case reviewStageVerification:
		return 5
	case reviewStageFinalizing:
		return 6
	case reviewStageCompleted:
		return 7
	default:
		return -1
	}
}

func snapshotForCheckpoint(snapshot *reviewSourceSnapshot) checkpointSource {
	if snapshot == nil {
		return checkpointSource{}
	}
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	files := make(map[string]string, len(snapshot.files))
	for path, content := range snapshot.files {
		files[path] = content
	}
	return checkpointSource{Owner: snapshot.owner, Repo: snapshot.repo, HeadSHA: snapshot.headSHA, Files: files}
}

func restoreSnapshotFromCheckpoint(source string, cfg Config, saved checkpointSource) *reviewSourceSnapshot {
	if saved.HeadSHA == "" {
		return nil
	}
	if _, supported := parseReviewGitHubPR(source); !supported {
		return nil
	}
	base, officialAPI, err := reviewSourceAPIBase(cfg.GitHubAPIBase)
	if err != nil {
		return nil
	}
	client := &http.Client{
		Timeout: reviewSourceRequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	token := ""
	if officialAPI {
		token = cfg.GitHubToken
	}
	files := make(map[string]string, len(saved.Files))
	totalSize := 0
	for path, content := range saved.Files {
		files[path] = content
		totalSize += len(content)
	}
	return &reviewSourceSnapshot{
		base: base, owner: saved.Owner, repo: saved.Repo, headSHA: saved.HeadSHA,
		token: token, client: client, files: files, totalSize: totalSize,
	}
}

func requestForResume(checkpoint reviewCheckpoint) model.ReviewRequest {
	request := checkpoint.Request
	request.Diff = checkpoint.Diff
	request.BudgetYuan = 0
	request.BudgetCents = 0
	return request
}
