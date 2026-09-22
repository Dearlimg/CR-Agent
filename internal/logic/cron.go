package logic

import (
	"CR-Agent/internal/model"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CronDispatcher func(model.CronJob) (string, error)

var errCronAgentBusy = errors.New("审查 Agent 正忙")

// CronScheduler persists only durable schedules. A scheduled item is marked
// pending before it is delivered, which makes a process interruption visible
// and provides at-least-once dispatch semantics without replaying downtime.
type CronScheduler struct {
	path         string
	pollInterval time.Duration
	dispatch     CronDispatcher

	mu       sync.Mutex
	jobs     map[string]model.CronJob
	queue    []string
	running  bool
	stop     chan struct{}
	delivery chan struct{}
}

func NewCronScheduler(path string, pollInterval time.Duration, dispatch CronDispatcher) (*CronScheduler, error) {
	if strings.TrimSpace(path) == "" {
		path = ".cron-jobs.json"
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	scheduler := &CronScheduler{
		path:         path,
		pollInterval: pollInterval,
		dispatch:     dispatch,
		jobs:         map[string]model.CronJob{},
		queue:        []string{},
		delivery:     make(chan struct{}, 1),
	}
	if err := scheduler.load(); err != nil {
		return nil, err
	}
	return scheduler, nil
}

func (s *CronScheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	stop := s.stop
	s.mu.Unlock()

	go s.loop(stop)
}

func (s *CronScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	close(s.stop)
	s.running = false
	s.mu.Unlock()
}

func (s *CronScheduler) Schedule(cron, source, memoryQuery string, recurring, durable bool) (model.CronJob, error) {
	if _, err := parseCron(cron); err != nil {
		return model.CronJob{}, err
	}
	if strings.TrimSpace(source) == "" {
		return model.CronJob{}, fmt.Errorf("定时审查必须提供 source，不能持久化完整 diff")
	}
	id, err := newCronJobID()
	if err != nil {
		return model.CronJob{}, err
	}
	now := time.Now().UTC()
	job := model.CronJob{
		ID:          id,
		Cron:        strings.TrimSpace(cron),
		Source:      strings.TrimSpace(source),
		MemoryQuery: strings.TrimSpace(memoryQuery),
		Recurring:   recurring,
		Durable:     durable,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	if err := s.persistLocked(); err != nil {
		delete(s.jobs, job.ID)
		return model.CronJob{}, err
	}
	return job, nil
}

func (s *CronScheduler) List() []model.CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]model.CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	return jobs
}

func (s *CronScheduler) Get(jobID string) (model.CronJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *CronScheduler) Cancel(jobID string) (model.CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return model.CronJob{}, fmt.Errorf("定时任务 %s 不存在", jobID)
	}
	delete(s.jobs, jobID)
	s.removeQueuedLocked(jobID)
	if err := s.persistLocked(); err != nil {
		s.jobs[jobID] = job
		return model.CronJob{}, err
	}
	return job, nil
}

// Poll records each matching minute exactly once and enqueues pending work.
// It is exported to permit deterministic time-based tests.
func (s *CronScheduler) Poll(now time.Time) error {
	minute := now.In(time.Local).Format("2006-01-02T15:04")
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, job := range s.jobs {
		if job.PendingDelivery || job.LastFired == minute {
			continue
		}
		spec, err := parseCron(job.Cron)
		if err != nil {
			job.LastError = err.Error()
			job.UpdatedAt = time.Now().UTC()
			s.jobs[id] = job
			changed = true
			continue
		}
		if !spec.matches(now.In(time.Local)) {
			continue
		}
		job.PendingDelivery = true
		job.LastFired = minute
		job.LastError = ""
		job.UpdatedAt = time.Now().UTC()
		s.jobs[id] = job
		s.queue = append(s.queue, id)
		changed = true
	}
	if !changed {
		return nil
	}
	return s.persistLocked()
}

// DeliverPending invokes the registered dispatcher once for every queued
// schedule. Failed dispatch remains pending and is retried by the loop.
func (s *CronScheduler) DeliverPending() error {
	select {
	case s.delivery <- struct{}{}:
		defer func() { <-s.delivery }()
	default:
		return nil
	}

	s.mu.Lock()
	queue := append([]string{}, s.queue...)
	s.queue = []string{}
	s.mu.Unlock()

	for _, jobID := range queue {
		s.mu.Lock()
		job, ok := s.jobs[jobID]
		s.mu.Unlock()
		if !ok || !job.PendingDelivery {
			continue
		}
		if s.dispatch == nil {
			s.recordDeliveryFailure(jobID, fmt.Errorf("没有注册定时任务执行器"))
			continue
		}
		backgroundID, err := s.dispatch(job)
		if err != nil {
			s.recordDeliveryFailure(jobID, err)
			continue
		}
		if err := s.recordDeliverySuccess(jobID, backgroundID); err != nil {
			return err
		}
	}
	return nil
}

func (s *CronScheduler) loop(stop <-chan struct{}) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			_ = s.Poll(now)
			_ = s.DeliverPending()
		}
	}
}

func (s *CronScheduler) recordDeliveryFailure(jobID string, dispatchErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	if errors.Is(dispatchErr, errCronAgentBusy) {
		job.LastError = ""
	} else {
		job.LastError = redact(dispatchErr.Error())
	}
	job.UpdatedAt = time.Now().UTC()
	s.jobs[jobID] = job
	s.queue = append(s.queue, jobID)
	_ = s.persistLocked()
}

func (s *CronScheduler) recordDeliverySuccess(jobID, backgroundID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil
	}
	job.LastBackgroundTaskID = backgroundID
	job.LastError = ""
	job.UpdatedAt = time.Now().UTC()
	if !job.Recurring {
		delete(s.jobs, jobID)
		return s.persistLocked()
	}
	job.PendingDelivery = false
	s.jobs[jobID] = job
	return s.persistLocked()
}

func (s *CronScheduler) removeQueuedLocked(jobID string) {
	queue := make([]string, 0, len(s.queue))
	for _, queuedID := range s.queue {
		if queuedID != jobID {
			queue = append(queue, queuedID)
		}
	}
	s.queue = queue
}

func (s *CronScheduler) load() error {
	content, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取定时任务文件: %w", err)
	}
	jobs := []model.CronJob{}
	if err := json.Unmarshal(content, &jobs); err != nil {
		return fmt.Errorf("解析定时任务文件: %w", err)
	}
	for _, job := range jobs {
		if !job.Durable {
			continue
		}
		if _, err := parseCron(job.Cron); err != nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.Source) == "" {
			continue
		}
		s.jobs[job.ID] = job
		if job.PendingDelivery {
			s.queue = append(s.queue, job.ID)
		}
	}
	return nil
}

func (s *CronScheduler) persistLocked() error {
	jobs := make([]model.CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.Durable {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	content, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("编码定时任务: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil && filepath.Dir(s.path) != "." {
		return fmt.Errorf("创建定时任务目录: %w", err)
	}
	if err := os.WriteFile(s.path, content, 0600); err != nil {
		return fmt.Errorf("写入定时任务: %w", err)
	}
	return nil
}

func newCronJobID() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成定时任务 ID: %w", err)
	}
	return "cron_" + hex.EncodeToString(bytes), nil
}

type cronSpec [5]cronField

type cronField struct {
	values map[int]bool
}

func parseCron(expression string) (cronSpec, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return cronSpec{}, fmt.Errorf("cron 表达式必须包含分钟、小时、日期、月份、星期五个字段")
	}
	ranges := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	var spec cronSpec
	for index, part := range parts {
		field, err := parseCronField(part, ranges[index][0], ranges[index][1])
		if err != nil {
			return cronSpec{}, fmt.Errorf("cron 第 %d 字段无效: %w", index+1, err)
		}
		spec[index] = field
	}
	return spec, nil
}

func parseCronField(input string, min, max int) (cronField, error) {
	field := cronField{values: map[int]bool{}}
	for _, segment := range strings.Split(input, ",") {
		start, end, step, err := parseCronSegment(segment, min, max)
		if err != nil {
			return cronField{}, err
		}
		for value := start; value <= end; value += step {
			field.values[value] = true
		}
	}
	if len(field.values) == 0 {
		return cronField{}, fmt.Errorf("字段不能为空")
	}
	return field, nil
}

func parseCronSegment(segment string, min, max int) (int, int, int, error) {
	base, stepText, hasStep := strings.Cut(strings.TrimSpace(segment), "/")
	step := 1
	if hasStep {
		parsed, err := strconv.Atoi(stepText)
		if err != nil || parsed <= 0 {
			return 0, 0, 0, fmt.Errorf("步长必须为正整数")
		}
		step = parsed
	}
	if base == "*" {
		return min, max, step, nil
	}
	if hasStep {
		return 0, 0, 0, fmt.Errorf("步长仅支持 */N")
	}
	if strings.Contains(base, "-") {
		bounds := strings.Split(base, "-")
		if len(bounds) != 2 {
			return 0, 0, 0, fmt.Errorf("范围格式无效")
		}
		start, startErr := strconv.Atoi(bounds[0])
		end, endErr := strconv.Atoi(bounds[1])
		if startErr != nil || endErr != nil || start < min || end > max || start > end {
			return 0, 0, 0, fmt.Errorf("范围超出 %d-%d", min, max)
		}
		return start, end, 1, nil
	}
	value, err := strconv.Atoi(base)
	if err != nil || value < min || value > max {
		return 0, 0, 0, fmt.Errorf("数值必须在 %d-%d", min, max)
	}
	return value, value, 1, nil
}

func (s cronSpec) matches(now time.Time) bool {
	values := [5]int{now.Minute(), now.Hour(), now.Day(), int(now.Month()), int(now.Weekday())}
	for index, value := range values {
		if !s[index].values[value] {
			return false
		}
	}
	return true
}
