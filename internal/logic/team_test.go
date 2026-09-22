package logic

import (
	"CR-Agent/internal/model"
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestReviewTeamClaimsSpecialistsAndDeliversEvents(t *testing.T) {
	root := t.TempDir()
	team := NewReviewTeam(
		NewTaskStore(filepath.Join(root, "tasks")),
		NewMessageBus(filepath.Join(root, "mailboxes")),
		Config{},
	)
	team.RunWorker = func(_ context.Context, _ Config, agent ReviewSubagent, _ string, _ ReviewPromptContext) SubagentResult {
		return SubagentResult{Name: agent.Name, Summary: agent.Name + " report"}
	}

	results, events, err := team.Run(context.Background(), "job_1", "task_parent", "diff", ReviewPromptContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || len(events) != 6 {
		t.Fatalf("results=%d events=%d", len(results), len(events))
	}
	for _, event := range events {
		if event.To != "lead" || event.TaskID != "job_1" {
			t.Fatalf("unexpected event: %#v", event)
		}
	}
	tasks, err := team.Tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	for _, task := range tasks {
		if task.Status != model.TaskCompleted {
			t.Fatalf("task not completed: %#v", task)
		}
	}
}

func TestMessageBusKeepsOtherJobsForTheirLeadTurn(t *testing.T) {
	bus := NewMessageBus(t.TempDir())
	for _, jobID := range []string{"job_one", "job_two"} {
		if err := bus.Send(model.TeamEvent{From: "security", To: "lead", Type: "result", TaskID: jobID, Content: jobID}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := bus.Consume("lead", "job_one")
	if err != nil || len(first) != 1 || first[0].TaskID != "job_one" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := bus.Consume("lead", "job_two")
	if err != nil || len(second) != 1 || second[0].TaskID != "job_two" {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestReviewTeamLimitsSpecialistConcurrency(t *testing.T) {
	team := NewReviewTeam(NewTaskStore(t.TempDir()), NewMessageBus(t.TempDir()), Config{TeamMaxConcurrency: 1})
	var active int32
	var peak int32
	team.RunWorker = func(_ context.Context, _ Config, agent ReviewSubagent, _ string, _ ReviewPromptContext) SubagentResult {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&peak)
			if current <= previous || atomic.CompareAndSwapInt32(&peak, previous, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&active, -1)
		return SubagentResult{Name: agent.Name, Summary: "ok"}
	}
	results, _, err := team.Run(context.Background(), "job_limit", "parent", "diff", ReviewPromptContext{})
	if err != nil || len(results) != 3 {
		t.Fatalf("results=%d err=%v", len(results), err)
	}
	if peak != 1 {
		t.Fatalf("peak concurrency=%d, want 1", peak)
	}
}
