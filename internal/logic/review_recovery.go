package logic

import (
	"CR-Agent/internal/dao"
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"time"
)

const (
	reviewRunnerLeaseDuration = 90 * time.Second
	reviewRunnerHeartbeat     = 20 * time.Second
	reviewRecoveryPoll        = 15 * time.Second
)

func (s *Service) launchReview(job *model.ReviewJob, request model.ReviewRequest, recorder *TraceRecorder) error {
	store, ok := s.Store.(dao.ReviewRecoveryStore)
	if !ok {
		return s.Background.Launch(job.BackgroundTaskID, func(ctx context.Context) (string, error) {
			s.runWithTracer(ctx, job, request, recorder)
			if job.Status != "completed" && job.Status != "completed_with_warnings" {
				return "", fmt.Errorf("审查任务失败：%s", job.Error)
			}
			return fmt.Sprintf("审查任务 %s 已完成", job.ID), nil
		})
	}
	owner := id("review-runner:" + job.ID)
	claimed, err := store.TryClaimReview(job.ID, owner, time.Now().UTC().Add(reviewRunnerLeaseDuration))
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("审查任务 %s 已由其他执行器处理", job.ID)
	}
	if err := s.launchClaimedReview(job, request, recorder, owner); err != nil {
		_ = store.ReleaseReview(job.ID, owner)
		return err
	}
	return nil
}

func (s *Service) launchClaimedReview(
	job *model.ReviewJob,
	request model.ReviewRequest,
	recorder *TraceRecorder,
	owner string,
) error {
	store, ok := s.Store.(dao.ReviewRecoveryStore)
	if !ok {
		return fmt.Errorf("当前审查存储不支持恢复租约")
	}
	return s.Background.Launch(job.BackgroundTaskID, func(parent context.Context) (string, error) {
		ctx, cancel := context.WithCancel(parent)
		done := make(chan struct{})
		go s.renewReviewLease(ctx, cancel, done, store, job.ID, owner)
		defer func() {
			cancel()
			<-done
			_ = store.ReleaseReview(job.ID, owner)
		}()
		s.runWithTracer(ctx, job, request, recorder)
		if job.Status != "completed" && job.Status != "completed_with_warnings" {
			return "", fmt.Errorf("审查任务失败：%s", job.Error)
		}
		return fmt.Sprintf("审查任务 %s 已完成", job.ID), nil
	})
}

func (s *Service) renewReviewLease(
	ctx context.Context,
	cancel context.CancelFunc,
	done chan<- struct{},
	store dao.ReviewRecoveryStore,
	jobID string,
	owner string,
) {
	defer close(done)
	ticker := time.NewTicker(reviewRunnerHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			claimed, err := store.TryClaimReview(jobID, owner, time.Now().UTC().Add(reviewRunnerLeaseDuration))
			if err != nil || !claimed {
				cancel()
				return
			}
		}
	}
}

func (s *Service) ResumeReview(jobID string) (*model.ReviewJob, error) {
	job, found := s.Store.Get(jobID)
	if !found {
		return nil, fmt.Errorf("审查任务不存在")
	}
	checkpoint, err := loadReviewCheckpoint(job)
	if err != nil {
		return nil, err
	}
	if checkpoint.Stage == reviewStageCompleted && job.Status != "queued" && job.Status != "running" {
		return nil, fmt.Errorf("审查任务已完成，无需恢复")
	}
	store, ok := s.Store.(dao.ReviewRecoveryStore)
	if !ok {
		return nil, fmt.Errorf("当前审查存储不支持恢复租约")
	}
	owner := id("review-resume:" + job.ID)
	claimed, err := store.TryClaimReview(job.ID, owner, time.Now().UTC().Add(reviewRunnerLeaseDuration))
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, fmt.Errorf("审查任务正在运行，暂不能恢复")
	}
	background, err := s.Background.Create("恢复代码审查 " + job.ID)
	if err != nil {
		_ = store.ReleaseReview(job.ID, owner)
		return nil, err
	}
	job.BackgroundTaskID = background.ID
	job.Status = "queued"
	job.ReviewOutcome = ""
	job.Error = ""
	job.FinishedAt = nil
	job.UpdatedAt = time.Now().UTC()
	if err := s.Store.Save(job); err != nil {
		_ = store.ReleaseReview(job.ID, owner)
		return nil, err
	}
	request := requestForResume(checkpoint)
	request.Source = job.Source
	if err := s.launchClaimedReview(job, request, newTraceRecorder(job), owner); err != nil {
		_ = store.ReleaseReview(job.ID, owner)
		job.Status = "failed"
		job.Error = "启动恢复任务失败：" + err.Error()
		_ = s.Store.Save(job)
		return nil, err
	}
	return job, nil
}

func (s *Service) recoverInterruptedReviews() error {
	store, ok := s.Store.(dao.ReviewRecoveryStore)
	if !ok {
		return nil
	}
	jobs, err := store.ListRecoverableReviews()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		checkpoint, err := loadReviewCheckpoint(job)
		if err != nil {
			setReviewFailure(job, err, err.Error())
			finished := time.Now().UTC()
			job.FinishedAt = &finished
			job.UpdatedAt = finished
			_ = s.Store.Save(job)
			continue
		}
		owner := id("review-recover:" + job.ID)
		claimed, err := store.TryClaimReview(job.ID, owner, time.Now().UTC().Add(reviewRunnerLeaseDuration))
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		background, err := s.Background.Create("恢复代码审查 " + job.ID)
		if err != nil {
			_ = store.ReleaseReview(job.ID, owner)
			return err
		}
		job.BackgroundTaskID = background.ID
		job.Status = "queued"
		job.ReviewOutcome = ""
		job.Error = ""
		job.FinishedAt = nil
		job.UpdatedAt = time.Now().UTC()
		if err := s.Store.Save(job); err != nil {
			_ = store.ReleaseReview(job.ID, owner)
			return err
		}
		request := requestForResume(checkpoint)
		request.Source = job.Source
		if err := s.launchClaimedReview(job, request, newTraceRecorder(job), owner); err != nil {
			_ = store.ReleaseReview(job.ID, owner)
			return err
		}
	}
	return nil
}

func (s *Service) startReviewRecoveryLoop() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if s.recoveryCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.recoveryCancel = cancel
	go func() {
		ticker := time.NewTicker(reviewRecoveryPoll)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.recoverInterruptedReviews()
			}
		}
	}()
}
