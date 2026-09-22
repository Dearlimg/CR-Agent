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
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestReviewHTTPCompletesWithLocalModelAndHarness(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"[]"},"finish_reason":"stop"}]}`))
	}))
	defer provider.Close()
	root := t.TempDir()
	svc := logic.NewService(dao.NewJobStore(filepath.Join(root, "jobs")), logic.Config{
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
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("review did not complete")
}
