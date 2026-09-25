package dao

import (
	"CR-Agent/internal/model"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JobStore struct {
	mu       sync.RWMutex
	jobs     map[string]*model.ReviewJob
	revision map[string]int64
	dir      string
	leases   map[string]reviewLease
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

// ReviewRecoveryStore provides durable review enumeration and an execution lease.
type ReviewRecoveryStore interface {
	ListRecoverableReviews() ([]*model.ReviewJob, error)
	TryClaimReview(id, owner string, leaseUntil time.Time) (bool, error)
	ReleaseReview(id, owner string) error
}

func NewJobStore(dir string) *JobStore {
	store := &JobStore{jobs: map[string]*model.ReviewJob{}, revision: map[string]int64{}, dir: dir, leases: map[string]reviewLease{}}
	_ = store.loadJobs()
	return store
}

type reviewLease struct {
	owner string
	until time.Time
}

func (s *JobStore) Save(j *model.ReviewJob) error {
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.revision[j.ID]++
	s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	b, err := json.Marshal(struct {
		*model.ReviewJob
		CheckpointJSON string `json:"checkpoint_json,omitempty"`
		BudgetMicros   int64  `json:"budget_micros,omitempty"`
		SpentMicros    int64  `json:"spent_micros,omitempty"`
		ReservedMicros int64  `json:"reserved_micros,omitempty"`
	}{
		ReviewJob:      j,
		CheckpointJSON: j.CheckpointJSON,
		BudgetMicros:   j.BudgetMicros,
		SpentMicros:    j.SpentMicros,
		ReservedMicros: j.ReservedMicros,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, j.ID+".json"), b, 0600)
}

func (s *JobStore) loadJobs() error {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return err
		}
		var persisted struct {
			model.ReviewJob
			CheckpointJSON string `json:"checkpoint_json"`
			BudgetMicros   int64  `json:"budget_micros"`
			SpentMicros    int64  `json:"spent_micros"`
			ReservedMicros int64  `json:"reserved_micros"`
		}
		if err := json.Unmarshal(content, &persisted); err != nil {
			return err
		}
		job := persisted.ReviewJob
		job.CheckpointJSON = persisted.CheckpointJSON
		job.BudgetMicros = persisted.BudgetMicros
		job.SpentMicros = persisted.SpentMicros
		job.ReservedMicros = persisted.ReservedMicros
		s.jobs[job.ID] = &job
		s.revision[job.ID] = 1
	}
	return nil
}

func (s *JobStore) ListRecoverableReviews() ([]*model.ReviewJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reviews := []*model.ReviewJob{}
	for _, job := range s.jobs {
		if (job.Status == "queued" || job.Status == "running") && job.CheckpointJSON != "" {
			reviews = append(reviews, reviewJobWithoutTraceDetails(job))
		}
	}
	return reviews, nil
}

func (s *JobStore) TryClaimReview(id, owner string, leaseUntil time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	lease, exists := s.leases[id]
	if exists && lease.until.After(now) && lease.owner != owner {
		return false, nil
	}
	s.leases[id] = reviewLease{owner: owner, until: leaseUntil}
	return true, nil
}

func (s *JobStore) ReleaseReview(id, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease, exists := s.leases[id]; exists && lease.owner == owner {
		delete(s.leases, id)
	}
	return nil
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
