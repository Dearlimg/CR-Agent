package controller

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type terminalSnapshotStore struct {
	dao.Store
	mu      sync.Mutex
	invalid bool
}

func (s *terminalSnapshotStore) Save(job *model.ReviewJob) error {
	if reviewTerminal(job.Status) {
		s.mu.Lock()
		if job.FinishedAt == nil || !job.UpdatedAt.Equal(*job.FinishedAt) || len(job.Trace) == 0 {
			s.invalid = true
		}
		s.mu.Unlock()
	}
	return s.Store.Save(job)
}

func (s *terminalSnapshotStore) sawInvalidTerminalSave() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invalid
}

func TestReviewHTTPCompletesWithLocalModelAndHarness(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"[]"},"finish_reason":"stop"}]}`))
	}))
	defer provider.Close()
	root := t.TempDir()
	store := &terminalSnapshotStore{Store: dao.NewJobStore(filepath.Join(root, "jobs"))}
	svc := logic.NewService(store, logic.Config{
		SkillsDir: "../../skills", MemoryDir: filepath.Join(root, "memory"), TasksDir: filepath.Join(root, "tasks"),
		BackgroundTasksDir: filepath.Join(root, "background"), TeamMailboxDir: filepath.Join(root, "team"),
		CronFile: filepath.Join(root, "cron.json"), DeepSeekAPIKey: "test-only", DeepSeekBaseURL: provider.URL,
		ContextOutputDir: filepath.Join(root, "results"), ContextTranscriptDir: filepath.Join(root, "history"),
	})
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	router := gin.New()
	NewReviewController(svc).Register(router)
	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatal(health.Body.String())
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/reviews", strings.NewReader(`{"diff":"diff --git a/a.go b/a.go\n+package a"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code < 200 || response.Code >= 300 {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var job model.ReviewJob
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		background, err := svc.Background.Get(job.BackgroundTaskID)
		if err != nil {
			t.Fatal(err)
		}
		if background.Status == model.BackgroundTaskFailed {
			t.Fatalf("background: %#v", background)
		}
		if background.Status == model.BackgroundTaskCompleted {
			completed := httptest.NewRecorder()
			router.ServeHTTP(completed, httptest.NewRequest(http.MethodGet, "/api/reviews/"+job.ID, nil))
			if err := json.Unmarshal(completed.Body.Bytes(), &job); err != nil {
				t.Fatal(err)
			}
			if job.Status != "completed" {
				t.Fatalf("status=%s error=%s", job.Status, job.Error)
			}
			if job.StartedAt.IsZero() || job.FinishedAt == nil {
				t.Fatalf("job boundaries missing: started=%v finished=%v", job.StartedAt, job.FinishedAt)
			}
			if len(job.Trace) == 0 {
				t.Fatal("trace is empty")
			}
			for _, event := range job.Trace {
				if event.StartedAt.IsZero() || event.EndedAt == nil || event.Status == "" || event.Kind == "" {
					t.Fatalf("incomplete trace event: %#v", event)
				}
				if event.DurationMs < 0 || event.EndedAt.Before(event.StartedAt) {
					t.Fatalf("invalid trace duration: %#v", event)
				}
			}
			if store.sawInvalidTerminalSave() {
				t.Fatal("terminal status was saved before final timestamps and trace")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("review did not complete")
}

type sequencedReviewStore struct {
	snapshots []*model.ReviewJob
	reads     int
}

func (s *sequencedReviewStore) Save(*model.ReviewJob) error { return nil }

func (s *sequencedReviewStore) Get(string) (*model.ReviewJob, bool) {
	index := s.reads
	s.reads++
	if index >= len(s.snapshots) {
		index = len(s.snapshots) - 1
	}
	return s.snapshots[index], true
}

func (s *sequencedReviewStore) RecordToolCall(string, string, string, string, string, string, string, int64) error {
	return nil
}

func TestReviewEventsWaitForFinalSnapshot(t *testing.T) {
	started := time.Now().UTC().Add(-time.Second)
	finished := time.Now().UTC()
	store := &sequencedReviewStore{snapshots: []*model.ReviewJob{
		{ID: "review-1", Status: "completed", StartedAt: started},
		{
			ID: "review-1", Status: "completed", StartedAt: started,
			FinishedAt: &finished, UpdatedAt: finished,
			Trace: []model.TraceEvent{{ID: "final-trace", Tool: "task_complete"}},
		},
	}}
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/reviews/review-1/events", nil)
	context.Params = gin.Params{{Key: "id", Value: "review-1"}}
	(&ReviewController{Service: &logic.Service{Store: store}}).events(context)

	if store.reads != 2 {
		t.Fatalf("read count = %d, want 2", store.reads)
	}
	if got := strings.Count(response.Body.String(), "data:"); got != 1 {
		t.Fatalf("SSE events = %d, want 1: %s", got, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"finished_at"`) || !strings.Contains(response.Body.String(), "final-trace") {
		t.Fatalf("SSE did not contain the final snapshot: %s", response.Body.String())
	}
}
