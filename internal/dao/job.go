package dao

import (
	"CR-Agent/internal/model"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type JobStore struct {
	mu       sync.RWMutex
	jobs     map[string]*model.ReviewJob
	revision map[string]int64
	dir      string
}

// Store is the persistence contract used by the review service.
type Store interface {
	Save(*model.ReviewJob) error
	Get(string) (*model.ReviewJob, bool)
	RecordToolCall(string, string, string, string, string, string, string, int64) error
}

// ReviewEventSnapshot contains changed review fields and traces after the
// supplied database cursor. Version lets SSE skip child queries between saves.
type ReviewEventSnapshot struct {
	Job         *model.ReviewJob
	Version     int64
	TraceCursor uint
	Changed     bool
}

// ReviewEventStore is an optional optimized read path used by the SSE endpoint.
type ReviewEventStore interface {
	GetReviewEventSnapshot(id string, afterVersion int64, afterTraceCursor uint) (*ReviewEventSnapshot, bool, error)
}

// TraceDetailStore loads large trace text only when a client asks to inspect it.
type TraceDetailStore interface {
	GetTraceDetails(id, traceID string) (*model.TraceEvent, bool, error)
}

func NewJobStore(dir string) *JobStore {
	return &JobStore{jobs: map[string]*model.ReviewJob{}, revision: map[string]int64{}, dir: dir}
}
func (s *JobStore) Save(j *model.ReviewJob) error {
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.revision[j.ID]++
	s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, j.ID+".json"), b, 0600)
}
func (s *JobStore) Get(id string) (*model.ReviewJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	return reviewJobWithoutTraceDetails(j), true
}
func (s *JobStore) RecordToolCall(string, string, string, string, string, string, string, int64) error {
	return nil
}

func (s *JobStore) GetReviewEventSnapshot(id string, afterVersion int64, afterTraceCursor uint) (*ReviewEventSnapshot, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false, nil
	}
	version := s.revision[id]
	snapshot := &ReviewEventSnapshot{Version: version, TraceCursor: afterTraceCursor}
	if version <= afterVersion {
		return snapshot, true, nil
	}

	updated := reviewJobWithoutTraceDetails(job)
	updated.Trace = make([]model.TraceEvent, 0, len(job.Trace))
	start := int(afterTraceCursor)
	if start > len(job.Trace) {
		start = len(job.Trace)
	}
	for _, trace := range job.Trace[start:] {
		updated.Trace = append(updated.Trace, traceWithoutDetails(trace))
	}
	snapshot.Job = updated
	snapshot.TraceCursor = uint(len(job.Trace))
	snapshot.Changed = true
	return snapshot, true, nil
}

func (s *JobStore) GetTraceDetails(id, traceID string) (*model.TraceEvent, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false, nil
	}
	for _, trace := range job.Trace {
		if trace.ID == traceID {
			traceCopy := trace
			return &traceCopy, true, nil
		}
	}
	return nil, false, nil
}

func reviewJobWithoutTraceDetails(job *model.ReviewJob) *model.ReviewJob {
	copy := *job
	copy.Comments = append([]model.ReviewComment{}, job.Comments...)
	copy.Todos = append([]model.TodoItem{}, job.Todos...)
	copy.TeamEvents = append([]model.TeamEvent{}, job.TeamEvents...)
	copy.Trace = make([]model.TraceEvent, 0, len(job.Trace))
	for _, trace := range job.Trace {
		copy.Trace = append(copy.Trace, traceWithoutDetails(trace))
	}
	return &copy
}

func traceWithoutDetails(trace model.TraceEvent) model.TraceEvent {
	trace.Input = ""
	trace.Output = ""
	trace.Prompt = ""
	trace.ModelReply = ""
	return trace
}
