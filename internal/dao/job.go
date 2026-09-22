package dao

import (
	"CR-Agent/internal/model"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*model.ReviewJob
	dir  string
}

func NewJobStore(dir string) *JobStore {
	return &JobStore{jobs: map[string]*model.ReviewJob{}, dir: dir}
}
func (s *JobStore) Save(j *model.ReviewJob) error {
	s.mu.Lock()
	s.jobs[j.ID] = j
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
	return j, ok
}
