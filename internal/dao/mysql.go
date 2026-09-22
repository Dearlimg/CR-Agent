package dao

import (
	"CR-Agent/internal/model"
	"context"
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"time"
)

type MySQLStore struct{ db *gorm.DB }

func OpenMySQL(dsn string) (*MySQLStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("mysql dsn 不能为空")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("连接 mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return &MySQLStore{db: db}, nil
}
func (s *MySQLStore) DB() *gorm.DB   { return s.db }
func (s *MySQLStore) Migrate() error { return AutoMigrate(s.db) }
func (s *MySQLStore) CreateJob(ctx context.Context, j *model.DBReviewJob) error {
	return s.db.WithContext(ctx).Create(j).Error
}
func (s *MySQLStore) FindJob(ctx context.Context, id string) (*model.DBReviewJob, error) {
	var j model.DBReviewJob
	err := s.db.WithContext(ctx).Where("public_id = ?", id).Preload("Comments").Preload("Traces").First(&j).Error
	if err != nil {
		return nil, err
	}
	return &j, nil
}
func (s *MySQLStore) UpdateJob(ctx context.Context, j *model.DBReviewJob) error {
	return s.db.WithContext(ctx).Save(j).Error
}
func (s *MySQLStore) CreateTrace(ctx context.Context, t *model.DBTraceEvent) error {
	return s.db.WithContext(ctx).Create(t).Error
}
func (s *MySQLStore) CreateComment(ctx context.Context, c *model.DBReviewComment) error {
	return s.db.WithContext(ctx).Create(c).Error
}
func (s *MySQLStore) CreateToolCall(ctx context.Context, c *model.DBToolCall) error {
	return s.db.WithContext(ctx).Create(c).Error
}

func (s *MySQLStore) Save(j *model.ReviewJob) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var row model.DBReviewJob
		err := tx.Where("public_id = ?", j.ID).First(&row).Error
		if err == gorm.ErrRecordNotFound {
			row = model.DBReviewJob{PublicID: j.ID, SourceURL: j.Source, Status: j.Status, BudgetCents: 1000, CreatedAt: time.Now()}
		} else if err != nil {
			return err
		}
		row.SourceURL, row.Status, row.SpentCents, row.ErrorMessage, row.UpdatedAt = j.Source, j.Status, j.SpentCents, j.Error, j.UpdatedAt
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := tx.Where("job_id = ?", row.ID).Delete(&model.DBReviewComment{}).Error; err != nil {
			return err
		}
		for _, c := range j.Comments {
			if err := tx.Create(&model.DBReviewComment{JobID: row.ID, TraceID: c.TraceID, File: c.File, Line: c.Line, Severity: c.Severity, Confidence: c.Confidence, Body: c.Body, Status: "open", CreatedAt: time.Now()}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("job_id = ?", row.ID).Delete(&model.DBTraceEvent{}).Error; err != nil {
			return err
		}
		for _, t := range j.Trace {
			if err := tx.Create(&model.DBTraceEvent{JobID: row.ID, TraceID: t.ID, Tool: t.Tool, Phase: t.Phase, Input: t.Input, Output: t.Output, Prompt: t.Prompt, ModelReply: t.ModelReply, DurationMs: t.DurationMs, CreatedAt: t.At}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *MySQLStore) Get(id string) (*model.ReviewJob, bool) {
	var row model.DBReviewJob
	if s.db.Where("public_id = ?", id).Preload("Comments").Preload("Traces").First(&row).Error != nil {
		return nil, false
	}
	j := &model.ReviewJob{ID: row.PublicID, Status: row.Status, Source: row.SourceURL, SpentCents: row.SpentCents, UpdatedAt: row.UpdatedAt, Error: row.ErrorMessage, Comments: []model.ReviewComment{}, Trace: []model.TraceEvent{}}
	for _, c := range row.Comments {
		j.Comments = append(j.Comments, model.ReviewComment{File: c.File, Line: c.Line, Severity: c.Severity, Confidence: c.Confidence, Body: c.Body, TraceID: c.TraceID})
	}
	for _, t := range row.Traces {
		j.Trace = append(j.Trace, model.TraceEvent{ID: t.TraceID, Tool: t.Tool, Phase: t.Phase, Input: t.Input, Output: t.Output, Prompt: t.Prompt, ModelReply: t.ModelReply, DurationMs: t.DurationMs, At: t.CreatedAt})
	}
	return j, true
}
