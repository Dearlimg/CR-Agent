package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewMemoryExtractionRecoversAfterInvalidTypes(t *testing.T) {
	rounds := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rounds++
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if rounds == 1 {
			foundTypes := false
			for _, message := range request.Messages {
				foundTypes = foundTypes || strings.Contains(message.Content, "type 仅可为 user、feedback、project、reference")
			}
			if !foundTypes {
				t.Error("memory prompt lacks allowed type values")
			}
		}
		message := map[string]any{
			"role":    "assistant",
			"content": `[{"name":"rule","description":"reusable rule","type":"project","body":"Apply the rule.","scope":"persistent"}]`,
		}
		finishReason := "stop"
		if rounds <= 2 {
			if len(request.Tools) != 1 || request.Tools[0].Function.Name != parseMemoryCandidatesJSONTool ||
				!strings.Contains(request.Tools[0].Function.Description, memoryCandidateTypeValues()) {
				t.Errorf("round %d tools=%#v", rounds, request.Tools)
			}
			message["content"] = ""
			message["tool_calls"] = []any{map[string]any{
				"id": fmt.Sprintf("invalid-%d", rounds), "type": "function",
				"function": map[string]any{
					"name": parseMemoryCandidatesJSONTool,
					"arguments": jsonString(map[string]any{
						"json": `[{"name":"rule","description":"rule","type":"project_constraint","body":"rule","scope":"persistent"}]`,
					}),
				},
			}}
			finishReason = "tool_calls"
		} else if len(request.Tools) != 0 {
			t.Errorf("round %d still exposes tools", rounds)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": message, "finish_reason": finishReason}},
		})
	}))
	defer server.Close()

	root := t.TempDir()
	service := NewService(dao.NewJobStore(filepath.Join(root, "jobs")), Config{
		MemoryDir:      filepath.Join(root, "memory"),
		DeepSeekAPIKey: "test-only", DeepSeekBaseURL: server.URL,
	})
	job := &model.ReviewJob{
		ID: "memory-recovery", Source: "inline",
		Comments: []model.ReviewComment{{Confidence: "high", Severity: "high", Body: "reusable rule"}},
	}
	service.extractReviewMemories(context.Background(), job, model.ReviewRequest{Source: "inline"})
	records, err := service.MemoryStore.List()
	if err != nil || len(records) != 1 || records[0].Type != MemoryTypeProject || rounds != 3 {
		t.Fatalf("records=%#v err=%v rounds=%d", records, err, rounds)
	}
}
