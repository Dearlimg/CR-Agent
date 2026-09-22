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
