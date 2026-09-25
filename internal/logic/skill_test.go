package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillLoaderCatalogAndLoad(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "code-review")
	if err := os.Mkdir(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: code-review\ndescription: Review changed code safely\n---\n# Review\nFull instructions"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	loader := NewSkillLoader(root)
	if err := loader.Scan(); err != nil {
		t.Fatal(err)
	}
	catalog := loader.Catalog()
	if !strings.Contains(catalog, "code-review: Review changed code safely") {
		t.Fatalf("catalog = %q", catalog)
	}
	if strings.Contains(catalog, "Full instructions") {
		t.Fatalf("catalog leaked full skill content: %q", catalog)
	}
	skill, err := loader.Load("code-review")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Content != content {
		t.Fatalf("loaded content = %q", skill.Content)
	}
}

func TestSkillLoaderRejectsUnknownSkill(t *testing.T) {
	loader := NewSkillLoader(t.TempDir())
	if err := loader.Scan(); err != nil {
		t.Fatal(err)
	}
	_, err := loader.Load("../code-review")
	if err == nil || !strings.Contains(err.Error(), "unknown skill") {
		t.Fatalf("Load returned %v, want unknown skill error", err)
	}
}

func TestSkillFrontmatterHandlesWindowsLineEndings(t *testing.T) {
	name, description := parseSkillFrontmatter("\ufeff---\r\nname: semantic-review\r\ndescription: 业务语义审查\r\n---\r\n正文", "fallback")
	if name != "semantic-review" || description != "业务语义审查" {
		t.Fatalf("name=%q description=%q", name, description)
	}
}

func TestReviewPromptLoadsOnlySelectedSkill(t *testing.T) {
	prompt := BuildReviewSubagentPrompt("审查正确性", ReviewPromptContext{
		Catalog:      "- code-review: review; unused catalog text",
		SkillContent: "full skill",
		Memories:     "- [project] error-style: wrap errors",
	}, "diff --git")
	if !strings.Contains(prompt, "full skill") || !strings.Contains(prompt, "wrap errors") ||
		strings.Contains(prompt, "unused catalog text") {
		t.Fatalf("prompt did not include loaded skill: %q", prompt)
	}
	synthesis := BuildReviewSynthesisPrompt(ReviewPromptContext{
		Catalog:      "unused catalog text",
		SkillContent: "full skill",
		Memories:     "unused memory text",
		Evidence:     `{"checks":{"syntax_check":"not_run"}}`,
	}, `[{"file":"a.go","line":1}]`)
	if strings.Contains(synthesis, "full skill") || strings.Contains(synthesis, "unused catalog text") ||
		strings.Contains(synthesis, "unused memory text") || !strings.Contains(synthesis, "syntax_check") {
		t.Fatalf("synthesis prompt contains redundant context or loses evidence: %q", synthesis)
	}
}

func TestReviewPromptEnvelopeSeparatesRulesFromEvidence(t *testing.T) {
	prompt := BuildReviewPromptEnvelope("正确性", ReviewPromptContext{
		SkillContent:           "selected skill",
		Memories:               "remember this repository",
		Evidence:               "preflight status",
		SourceContextAvailable: true,
	}, "diff --git a/a.go b/a.go\n+bug()")
	if !strings.Contains(prompt.System, "selected skill") || !strings.Contains(prompt.System, "正确性") {
		t.Fatalf("selected instructions missing from system: %q", prompt.System)
	}
	for _, data := range []string{"remember this repository", "preflight status", "diff --git", "+bug()"} {
		if strings.Contains(prompt.System, data) || !strings.Contains(prompt.User, data) {
			t.Fatalf("data escaped user layer: %q", data)
		}
	}
	if !strings.Contains(prompt.User, "get_review_context") {
		t.Fatal("available context tool omitted")
	}
}

func TestServiceRecordsLoadedCodeReviewSkill(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "code-review")
	if err := os.Mkdir(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: code-review\ndescription: Review code\n---\n# Review"),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	service := NewService(dao.NewJobStore(t.TempDir()), Config{SkillsDir: root, TasksDir: filepath.Join(root, "tasks"), MemoryDir: filepath.Join(root, "memory"), BackgroundTasksDir: filepath.Join(root, "background")})
	job := &model.ReviewJob{ID: "skill-job", Comments: []model.ReviewComment{}, Trace: []model.TraceEvent{}}
	service.run(context.Background(), job, model.ReviewRequest{Diff: "diff --git a/a.go b/a.go\n+package a"})

	for _, event := range job.Trace {
		if event.Tool == "load_skill" && event.Input == "code-review" && event.Phase == "skill" {
			return
		}
	}
	t.Fatalf("load_skill trace not found: %#v", job.Trace)
}
