package logic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreSavesIndexesAndRecallsRelevantRecords(t *testing.T) {
	root := t.TempDir()
	store := NewMemoryStore(Config{
		MemoryDir:           root,
		MemoryMaxRecall:     5,
		MemoryMaxChars:      1000,
		MemoryConsolidateAt: 10,
	})
	goRecord, saved, err := store.Save(MemoryCandidate{
		Name:        "go-error-wrapping",
		Description: "项目错误必须保留调用上下文",
		Type:        MemoryTypeProject,
		Body:        "Go 服务返回错误时使用 %w 包装原始错误。",
		Scope:       "persistent",
	})
	if err != nil || !saved {
		t.Fatalf("Save = (%#v, %t, %v)", goRecord, saved, err)
	}
	_, saved, err = store.Save(MemoryCandidate{
		Name:        "go-error-wrapping-copy",
		Description: "项目错误必须保留调用上下文",
		Type:        MemoryTypeProject,
		Body:        "Go 服务返回错误时使用 %w 包装原始错误。",
		Scope:       "persistent",
	})
	if err != nil || saved {
		t.Fatalf("duplicate Save = (%t, %v), want not saved", saved, err)
	}
	_, saved, err = store.Save(MemoryCandidate{
		Name:        "release-notes",
		Description: "发布说明位置",
		Type:        MemoryTypeReference,
		Body:        "发布说明维护在 docs/releases。",
		Scope:       "persistent",
	})
	if err != nil || !saved {
		t.Fatalf("second Save = (%t, %v)", saved, err)
	}
	recalled, err := store.Recall("Go error handling should wrap failures")
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled) != 1 || recalled[0].Name != goRecord.Name {
		t.Fatalf("Recall = %#v", recalled)
	}
	index, err := os.ReadFile(filepath.Join(root, "MEMORY.md"))
	if err != nil || !strings.Contains(string(index), "go-error-wrapping") {
		t.Fatalf("index = %q, err = %v", index, err)
	}
}

func TestMemoryStoreRejectsTemporaryAndSensitiveCandidates(t *testing.T) {
	store := NewMemoryStore(Config{MemoryDir: t.TempDir(), MemoryMaxRecall: 5, MemoryMaxChars: 1000, MemoryConsolidateAt: 10})
	for _, candidate := range []MemoryCandidate{
		{Name: "temporary", Description: "本次会话不要写文件", Type: MemoryTypeFeedback, Body: "仅当前任务生效。", Scope: "persistent"},
		{Name: "secret", Description: "credential", Type: MemoryTypeProject, Body: "api_key=not-for-memory", Scope: "persistent"},
		{Name: "wrong-scope", Description: "长期约束", Type: MemoryTypeProject, Body: "这个约束有用。", Scope: "current_task"},
	} {
		_, saved, err := store.Save(candidate)
		if err != nil || saved {
			t.Fatalf("candidate %#v saved=%t err=%v", candidate, saved, err)
		}
	}
	records, err := store.List()
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

func TestParseMemoryCandidatesIgnoresJSONFence(t *testing.T) {
	candidates := parseMemoryCandidates("```json\n[{\"name\":\"go-style\",\"description\":\"Go 风格\",\"type\":\"project\",\"body\":\"使用 gofmt。\",\"scope\":\"persistent\"}]\n```")
	if len(candidates) != 1 || candidates[0].Name != "go-style" {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestMemoryStoreConsolidatesDuplicateRecords(t *testing.T) {
	store := NewMemoryStore(Config{MemoryDir: t.TempDir(), MemoryMaxRecall: 5, MemoryMaxChars: 1000, MemoryConsolidateAt: 10})
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	older := MemoryRecord{
		Name:        "old-go-style",
		Description: "Go 错误保留上下文",
		Type:        MemoryTypeProject,
		Body:        "使用 %w 包装原始错误。",
		UpdatedAt:   time.Now().Add(-time.Hour),
	}
	newer := MemoryRecord{
		Name:        "new-go-style",
		Description: "Go 错误保留上下文",
		Type:        MemoryTypeProject,
		Body:        "使用 %w 包装原始错误。",
		UpdatedAt:   time.Now(),
	}
	if err := store.writeRecord(older); err != nil {
		t.Fatal(err)
	}
	if err := store.writeRecord(newer); err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(); err != nil {
		t.Fatal(err)
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != newer.Name {
		t.Fatalf("records = %#v", records)
	}
}
