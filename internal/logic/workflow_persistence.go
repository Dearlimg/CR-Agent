package logic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"CR-Agent/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkflowLock interface {
	Release() error
}

type WorkflowPersistence interface {
	AcquireLock(runID string) (WorkflowLock, error)
	LoadSnapshot(runID string) (WorkflowSnapshot, error)
	SaveSnapshot(snapshot WorkflowSnapshot) error
	WriteOutput(runID string, value any) error
	AppendEvent(runID string, event WorkflowEvent) error
}

type fileWorkflowPersistence struct {
	root   string
	fileMu sync.Mutex
}

func NewFileWorkflowPersistence(root string) WorkflowPersistence {
	if strings.TrimSpace(root) == "" {
		root = ".workflows"
	}
	return &fileWorkflowPersistence{root: root}
}

func (s *fileWorkflowPersistence) AcquireLock(runID string) (WorkflowLock, error) {
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, runID+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("workflow run %q 已被其他进程占用或锁文件残留", runID)
	}
	return &fileWorkflowLock{file: file, path: path}, nil
}

func (s *fileWorkflowPersistence) LoadSnapshot(runID string) (WorkflowSnapshot, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	content, err := os.ReadFile(filepath.Join(s.root, runID+".json"))
	if err != nil {
		return WorkflowSnapshot{}, err
	}
	var snapshot WorkflowSnapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return WorkflowSnapshot{}, err
	}
	if snapshot.RunID != runID {
		return WorkflowSnapshot{}, fmt.Errorf("workflow snapshot run ID 不匹配")
	}
	journalPath := filepath.Join(s.root, runID+".journal.jsonl")
	journal, journalErr := os.ReadFile(journalPath)
	if journalErr != nil {
		return snapshot, nil
	}
	known := map[string]bool{}
	for _, event := range snapshot.Events {
		known[event.Type+"|"+event.Label+"|"+event.Message] = true
	}
	for _, line := range strings.Split(strings.TrimSpace(string(journal)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event WorkflowEvent
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		key := event.Type + "|" + event.Label + "|" + event.Message
		if known[key] {
			continue
		}
		snapshot.Events = append(snapshot.Events, event)
		known[key] = true
	}
	return snapshot, nil
}

func (s *fileWorkflowPersistence) SaveSnapshot(snapshot WorkflowSnapshot) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(s.root, snapshot.RunID+".json.tmp")
	path := filepath.Join(s.root, snapshot.RunID+".json")
	if err := os.WriteFile(temporary, content, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (s *fileWorkflowPersistence) WriteOutput(runID string, value any) error {
	if value == nil {
		value = map[string]any{}
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.root, runID+".output.json"), content, 0600)
}

func (s *fileWorkflowPersistence) AppendEvent(runID string, event WorkflowEvent) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return err
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(s.root, runID+".journal.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(content, '\n'))
	return err
}

type fileWorkflowLock struct {
	file *os.File
	path string
	once sync.Once
	err  error
}

func (l *fileWorkflowLock) Release() error {
	l.once.Do(func() {
		if err := l.file.Close(); err != nil {
			l.err = err
			return
		}
		if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
			l.err = err
		}
	})
	return l.err
}

type MySQLWorkflowPersistence struct {
	db *gorm.DB
}

func NewMySQLWorkflowPersistence(db *gorm.DB) WorkflowPersistence {
	return &MySQLWorkflowPersistence{db: db}
}

func (s *MySQLWorkflowPersistence) AcquireLock(runID string) (WorkflowLock, error) {
	owner := id("workflow-lock-" + runID)
	now := time.Now().UTC()
	leaseUntil := now.Add(time.Hour)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var lease model.DBWorkflowLease
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ?", runID).First(&lease).Error
		if err == gorm.ErrRecordNotFound {
			return tx.Create(&model.DBWorkflowLease{
				RunID:      runID,
				Owner:      owner,
				LeaseUntil: leaseUntil,
				CreatedAt:  now,
				UpdatedAt:  now,
			}).Error
		}
		if err != nil {
			return err
		}
		if lease.LeaseUntil.After(now) && lease.Owner != "" {
			return fmt.Errorf("workflow run %q 已被其他进程占用", runID)
		}
		return tx.Model(&lease).Updates(map[string]any{
			"owner":       owner,
			"lease_until": leaseUntil,
			"updated_at":  now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &mysqlWorkflowLock{db: s.db, runID: runID, owner: owner}, nil
}

func (s *MySQLWorkflowPersistence) LoadSnapshot(runID string) (WorkflowSnapshot, error) {
	var row model.DBAgentRun
	if err := s.db.Where("run_id = ?", runID).First(&row).Error; err != nil {
		return WorkflowSnapshot{}, err
	}
	args := map[string]any{}
	if row.ArgsJSON != "" {
		if err := json.Unmarshal([]byte(row.ArgsJSON), &args); err != nil {
			return WorkflowSnapshot{}, err
		}
	}
	task := WorkflowTask{
		ID:         row.RunID,
		Workflow:   row.WorkflowName,
		Status:     WorkflowStatus(row.Status),
		CreatedAt:  row.CreatedAt,
		StartedAt:  row.StartedAt,
		FinishedAt: row.FinishedAt,
		Error:      row.ErrorMessage,
		AgentCount: row.AgentCount,
		TokenUsage: row.TokenUsage,
		NotifiedAt: row.NotifiedAt,
	}
	if row.ResultJSON != "" {
		if err := json.Unmarshal([]byte(row.ResultJSON), &task.Result); err != nil {
			return WorkflowSnapshot{}, err
		}
	}
	var eventRows []model.DBWorkflowEvent
	if err := s.db.Where("run_id = ?", runID).Order("event_seq asc").Find(&eventRows).Error; err != nil {
		return WorkflowSnapshot{}, err
	}
	events := make([]WorkflowEvent, 0, len(eventRows))
	for _, event := range eventRows {
		events = append(events, WorkflowEvent{
			Type:      event.EventType,
			RunID:     runID,
			Workflow:  row.WorkflowName,
			Phase:     event.Phase,
			Label:     event.Label,
			Message:   event.Message,
			Status:    WorkflowStatus(event.Status),
			Timestamp: event.CreatedAt,
		})
	}
	return WorkflowSnapshot{RunID: runID, Workflow: row.WorkflowName, Args: args, Task: task, Events: events}, nil
}

func (s *MySQLWorkflowPersistence) SaveSnapshot(snapshot WorkflowSnapshot) error {
	args, err := json.Marshal(snapshot.Args)
	if err != nil {
		return err
	}
	result := []byte{}
	if snapshot.Task.Result != nil {
		result, err = json.Marshal(snapshot.Task.Result)
		if err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	var row model.DBAgentRun
	err = s.db.Where("run_id = ?", snapshot.RunID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		row = model.DBAgentRun{
			RunID:        snapshot.RunID,
			RunType:      "workflow",
			WorkflowName: snapshot.Workflow,
			Status:       string(snapshot.Task.Status),
			ArgsJSON:     string(args),
			ResultJSON:   string(result),
			ErrorMessage: snapshot.Task.Error,
			AgentCount:   snapshot.Task.AgentCount,
			TokenUsage:   snapshot.Task.TokenUsage,
			NotifiedAt:   snapshot.Task.NotifiedAt,
			CreatedAt:    snapshot.Task.CreatedAt,
			StartedAt:    snapshot.Task.StartedAt,
			FinishedAt:   snapshot.Task.FinishedAt,
			UpdatedAt:    now,
		}
		return s.db.Create(&row).Error
	}
	if err != nil {
		return err
	}
	row.Status = string(snapshot.Task.Status)
	row.ArgsJSON = string(args)
	row.ResultJSON = string(result)
	row.ErrorMessage = snapshot.Task.Error
	row.AgentCount = snapshot.Task.AgentCount
	row.TokenUsage = snapshot.Task.TokenUsage
	row.NotifiedAt = snapshot.Task.NotifiedAt
	row.StartedAt = snapshot.Task.StartedAt
	row.FinishedAt = snapshot.Task.FinishedAt
	row.UpdatedAt = now
	row.Version++
	return s.db.Save(&row).Error
}

func (s *MySQLWorkflowPersistence) WriteOutput(runID string, value any) error {
	if value == nil {
		value = map[string]any{}
	}
	content, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.db.Model(&model.DBAgentRun{}).Where("run_id = ?", runID).Updates(map[string]any{
		"result_json": string(content),
		"updated_at":  time.Now().UTC(),
	}).Error
}

func (s *MySQLWorkflowPersistence) AppendEvent(runID string, event WorkflowEvent) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var existing model.DBWorkflowEvent
		if err := tx.Where("run_id = ? AND event_type = ? AND label = ? AND message = ?", runID, event.Type, event.Label, event.Message).First(&existing).Error; err == nil {
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		var last model.DBWorkflowEvent
		lastSeq := int64(0)
		if err := tx.Where("run_id = ?", runID).Order("event_seq desc").First(&last).Error; err == nil {
			lastSeq = last.EventSeq
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		row := model.DBWorkflowEvent{
			RunID:     runID,
			EventSeq:  lastSeq + 1,
			EventType: event.Type,
			Phase:     event.Phase,
			Label:     event.Label,
			Message:   event.Message,
			Status:    string(event.Status),
			CallKey:   event.Label,
			CreatedAt: event.Timestamp,
		}
		if row.CreatedAt.IsZero() {
			row.CreatedAt = time.Now().UTC()
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if event.Type != "agent_result" {
			return nil
		}
		callResult := model.DBWorkflowCallResult{
			RunID:      runID,
			CallKey:    event.Label,
			Status:     "completed",
			ResultJSON: event.Message,
			CreatedAt:  row.CreatedAt,
			UpdatedAt:  row.CreatedAt,
		}
		return tx.Where("run_id = ? AND call_key = ?", runID, event.Label).FirstOrCreate(&callResult).Error
	})
}

type mysqlWorkflowLock struct {
	db    *gorm.DB
	runID string
	owner string
	once  sync.Once
	err   error
}

func (l *mysqlWorkflowLock) Release() error {
	l.once.Do(func() {
		l.err = l.db.Model(&model.DBWorkflowLease{}).
			Where("run_id = ? AND owner = ?", l.runID, l.owner).
			Updates(map[string]any{"owner": "", "lease_until": time.Now().UTC(), "updated_at": time.Now().UTC()}).Error
	})
	return l.err
}
