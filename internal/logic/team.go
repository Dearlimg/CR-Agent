package logic

import (
	"CR-Agent/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MessageBus keeps team communication out of model conversations. Events are
// append-only JSONL so a lead can recover reports after an interrupted run.
type MessageBus struct {
	root string
	mu   sync.Mutex
}

func NewMessageBus(root string) *MessageBus {
	if strings.TrimSpace(root) == "" {
		root = ".team-mailboxes"
	}
	return &MessageBus{root: root}
}

func (b *MessageBus) Send(event model.TeamEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	if err := os.MkdirAll(b.root, 0755); err != nil {
		return fmt.Errorf("创建团队收件箱: %w", err)
	}
	content, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("编码团队消息: %w", err)
	}
	path := filepath.Join(b.root, safeMailboxName(event.To)+".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("打开团队收件箱: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(content, '\n')); err != nil {
		return fmt.Errorf("写入团队消息: %w", err)
	}
	return nil
}

func (b *MessageBus) Consume(agent, jobID string) ([]model.TeamEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	path := filepath.Join(b.root, safeMailboxName(agent)+".jsonl")
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []model.TeamEvent{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取团队收件箱: %w", err)
	}
	events := []model.TeamEvent{}
	remaining := []model.TeamEvent{}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event model.TeamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("解析团队消息: %w", err)
		}
		if event.TaskID == jobID {
			events = append(events, event)
		} else {
			remaining = append(remaining, event)
		}
	}
	encoded := []byte{}
	for _, event := range remaining {
		line, err := json.Marshal(event)
		if err != nil {
			return nil, fmt.Errorf("编码保留团队消息: %w", err)
		}
		encoded = append(encoded, append(line, '\n')...)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		return nil, fmt.Errorf("确认团队消息: %w", err)
	}
	return events, nil
}

func (b *MessageBus) Ack([]model.TeamEvent) error { return nil }

func safeMailboxName(name string) string {
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		return "unknown"
	}
	return name
}

type ReviewTeam struct {
	Tasks          TaskRepository
	Bus            TeamMailbox
	Cfg            Config
	MaxConcurrency int
	slots          chan struct{}
	RunWorker      func(context.Context, Config, ReviewSubagent, string, ReviewPromptContext) SubagentResult
}

func NewReviewTeam(tasks TaskRepository, bus TeamMailbox, cfg Config) *ReviewTeam {
	maxConcurrency := cfg.TeamMaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 2
	}
	return &ReviewTeam{
		Tasks:          tasks,
		Bus:            bus,
		Cfg:            cfg,
		MaxConcurrency: maxConcurrency,
		slots:          make(chan struct{}, maxConcurrency),
		RunWorker:      RunReviewSpecialist,
	}
}

// Run starts short-lived specialists for one review. The lead owns the user
// response; specialists only publish a result and an idle notification.
func (t *ReviewTeam) Run(ctx context.Context, jobID, parentTaskID, diff string, prompt ReviewPromptContext) ([]SubagentResult, []model.TeamEvent, error) {
	if t.Tasks == nil || t.Bus == nil || t.RunWorker == nil {
		return nil, nil, fmt.Errorf("团队运行时未初始化")
	}
	specialists := ReviewSpecialists()
	results := make([]SubagentResult, len(specialists))
	semaphore := t.slots
	if semaphore == nil {
		limit := t.MaxConcurrency
		if limit <= 0 {
			limit = 1
		}
		semaphore = make(chan struct{}, limit)
	}
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for index, specialist := range specialists {
		child, err := t.Tasks.Create("专项审查: "+specialist.Name, "父审查任务: "+parentTaskID)
		if err != nil {
			return nil, nil, err
		}
		owner := "team-" + specialist.Name
		if _, err := t.Tasks.Claim(child.ID, owner); err != nil {
			return nil, nil, err
		}
		wg.Add(1)
		go func(i int, a ReviewSubagent, taskID, taskOwner string) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[i] = SubagentResult{Name: a.Name, Error: ctx.Err()}
				return
			}
			result := t.RunWorker(ctx, t.Cfg, a, diff, prompt)
			results[i] = result
			if _, _, err := t.Tasks.Complete(taskID, taskOwner); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
			content := result.Summary
			if result.Error != nil {
				content = "专项审查未完整完成：" + result.Error.Error() + "\n部分候选：\n" + result.Summary
			}
			if err := t.Bus.Send(model.TeamEvent{From: a.Name, To: "lead", Type: "result", TaskID: jobID, Content: content}); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			if err := t.Bus.Send(model.TeamEvent{From: a.Name, To: "lead", Type: "idle_notification", TaskID: jobID, Content: "Waiting for more work."}); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}(index, specialist, child.ID, owner)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	events, err := t.Bus.Consume("lead", jobID)
	if err != nil {
		return nil, nil, err
	}
	if err := t.Bus.Ack(events); err != nil {
		return nil, nil, err
	}
	return results, events, nil
}
