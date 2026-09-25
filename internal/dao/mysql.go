package dao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"CR-Agent/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type MySQLStore struct {
	db *gorm.DB
}

func OpenMySQL(dsn string) (*MySQLStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("mysql dsn 不能为空")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		PrepareStmt:    true,
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("检查 mysql 连接: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) DB() *gorm.DB { return s.db }

func (s *MySQLStore) Migrate() error { return AutoMigrate(s.db) }

func (s *MySQLStore) CreateJob(ctx context.Context, job *model.DBReviewJob) error {
	return s.db.WithContext(ctx).Create(job).Error
}

func (s *MySQLStore) FindJob(ctx context.Context, id string) (*model.DBReviewJob, error) {
	var job model.DBReviewJob
	err := s.db.WithContext(ctx).
		Where("public_id = ?", id).
		Preload("Comments").
		Preload("Traces", func(db *gorm.DB) *gorm.DB {
			return db.Order("started_at asc, id asc")
		}).
		Preload("Todos").
		First(&job).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *MySQLStore) UpdateJob(ctx context.Context, job *model.DBReviewJob) error {
	return s.db.WithContext(ctx).Save(job).Error
}

func (s *MySQLStore) CreateTrace(ctx context.Context, trace *model.DBTraceEvent) error {
	return s.db.WithContext(ctx).Create(trace).Error
}

func (s *MySQLStore) CreateComment(ctx context.Context, comment *model.DBReviewComment) error {
	return s.db.WithContext(ctx).Create(comment).Error
}

func (s *MySQLStore) CreateToolCall(ctx context.Context, call *model.DBToolCall) error {
	return s.db.WithContext(ctx).Create(call).Error
}

func (s *MySQLStore) Save(job *model.ReviewJob) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		row, err := s.findOrCreateJob(tx, job)
		if err != nil {
			return err
		}
		if err := s.saveComments(tx, row.ID, job); err != nil {
			return err
		}
		if err := s.saveTraces(tx, row.ID, job); err != nil {
			return err
		}
		if err := s.saveTodos(tx, row.ID, job); err != nil {
			return err
		}
		return s.saveTeamEvents(tx, job)
	})
}

func (s *MySQLStore) Get(id string) (*model.ReviewJob, bool) {
	var row model.DBReviewJob
	query := s.db.Model(&model.DBReviewJob{}).
		Select(reviewJobColumns()).
		Where("public_id = ?", id).
		Preload("Comments", func(db *gorm.DB) *gorm.DB {
			return db.Select(reviewCommentColumns())
		}).
		Preload("Traces", func(db *gorm.DB) *gorm.DB {
			return db.Select(reviewTraceMetadataColumns()).Order("started_at asc, id asc")
		}).
		Preload("Todos", func(db *gorm.DB) *gorm.DB {
			return db.Select(reviewTodoColumns())
		})
	if query.First(&row).Error != nil {
		return nil, false
	}
	job := reviewJobFromDB(row)
	var messages []model.DBTeamMessage
	if err := s.db.Select("message_id", "from_agent", "to_agent", "message_type", "job_id", "content", "created_at").
		Where("job_id = ?", id).Order("created_at asc").Find(&messages).Error; err == nil {
		for _, message := range messages {
			job.TeamEvents = append(job.TeamEvents, model.TeamEvent{
				ID:      message.MessageID,
				From:    message.FromAgent,
				To:      message.ToAgent,
				Type:    message.MessageType,
				TaskID:  message.JobID,
				Content: message.Content,
				At:      message.CreatedAt,
			})
		}
	}
	return job, true
}

func (s *MySQLStore) GetReviewEventSnapshot(id string, afterVersion int64, afterTraceCursor uint) (*ReviewEventSnapshot, bool, error) {
	var row model.DBReviewJob
	err := s.db.Model(&model.DBReviewJob{}).
		Select(reviewJobColumns()).
		Where("public_id = ?", id).
		First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	snapshot := &ReviewEventSnapshot{Version: row.Version, TraceCursor: afterTraceCursor}
	if row.Version <= afterVersion {
		return snapshot, true, nil
	}

	job := reviewJobFromDB(row)
	var comments []model.DBReviewComment
	if err := s.db.Select(reviewCommentColumns()).Where("job_id = ?", row.ID).Find(&comments).Error; err != nil {
		return nil, false, err
	}
	for _, comment := range comments {
		job.Comments = append(job.Comments, reviewCommentFromDB(comment))
	}
	var todos []model.DBTodoItem
	if err := s.db.Select(reviewTodoColumns()).Where("job_id = ?", row.ID).Find(&todos).Error; err != nil {
		return nil, false, err
	}
	for _, todo := range todos {
		job.Todos = append(job.Todos, model.TodoItem{
			Content: todo.Content,
			Status:  todo.Status,
			Order:   todo.SortOrder,
		})
	}
	var traces []model.DBTraceEvent
	if err := s.db.Select(reviewTraceMetadataColumns()).
		Where("job_id = ? AND id > ?", row.ID, afterTraceCursor).
		Order("id asc").Find(&traces).Error; err != nil {
		return nil, false, err
	}
	for _, trace := range traces {
		job.Trace = append(job.Trace, traceEventFromDB(trace, false))
		if trace.ID > snapshot.TraceCursor {
			snapshot.TraceCursor = trace.ID
		}
	}
	snapshot.Job = job
	snapshot.Changed = true
	return snapshot, true, nil
}

func (s *MySQLStore) GetTraceDetails(id, traceID string) (*model.TraceEvent, bool, error) {
	var job model.DBReviewJob
	if err := s.db.Select("id").Where("public_id = ?", id).First(&job).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	var trace model.DBTraceEvent
	err := s.db.Select(reviewTraceDetailColumns()).
		Where("job_id = ? AND trace_id = ?", job.ID, traceID).
		First(&trace).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	result := traceEventFromDB(trace, true)
	return &result, true, nil
}

func reviewJobColumns() []string {
	return []string{
		"id", "public_id", "task_id", "background_task_id", "source_url", "status", "review_outcome",
		"review_scope_json", "budget_micros", "spent_micros", "budget_cents", "spent_cents", "error_message",
		"version", "created_at", "started_at", "finished_at", "updated_at",
	}
}

func reviewCommentColumns() []string {
	return []string{
		"id", "job_id", "trace_id", "file", "line", "severity", "confidence", "body", "evidence", "trigger",
		"impact", "suggestion", "verification_status", "verification_reason",
	}
}

func reviewTraceMetadataColumns() []string {
	return []string{
		"id", "job_id", "trace_id", "parent_id", "kind", "status", "round", "retry_count", "input_tokens",
		"output_tokens", "model", "cost_micros", "estimated_cost", "input_price_yuan_per_million",
		"output_price_yuan_per_million", "finish_reason", "origin", "cache_hit", "tool_call_id", "tool_version",
		"input_digest", "tool", "phase", "duration_ms", "started_at", "ended_at", "created_at",
	}
}

func reviewTraceDetailColumns() []string {
	columns := reviewTraceMetadataColumns()
	return append(columns, "input", "output", "prompt", "model_reply")
}

func reviewTodoColumns() []string {
	return []string{"id", "job_id", "content", "status", "sort_order"}
}

func reviewJobFromDB(row model.DBReviewJob) *model.ReviewJob {
	startedAt := row.StartedAt
	if startedAt == nil {
		startedAt = &row.CreatedAt
	}
	budgetMicros := row.BudgetMicros
	if budgetMicros <= 0 && row.BudgetCents > 0 {
		budgetMicros = model.YuanToMicros(float64(row.BudgetCents) / 100)
	}
	spentMicros := row.SpentMicros
	if spentMicros <= 0 && row.SpentCents > 0 {
		spentMicros = model.YuanToMicros(float64(row.SpentCents) / 100)
	}
	job := &model.ReviewJob{
		ID:               row.PublicID,
		TaskID:           row.TaskID,
		BackgroundTaskID: row.BackgroundTaskID,
		Status:           row.Status,
		ReviewOutcome:    row.ReviewOutcome,
		Source:           row.SourceURL,
		BudgetMicros:     budgetMicros,
		SpentMicros:      spentMicros,
		BudgetYuan:       model.MicrosToYuan(budgetMicros),
		SpentYuan:        model.MicrosToYuan(spentMicros),
		StartedAt:        *startedAt,
		FinishedAt:       row.FinishedAt,
		UpdatedAt:        row.UpdatedAt,
		Error:            row.ErrorMessage,
		Comments:         []model.ReviewComment{},
		Trace:            []model.TraceEvent{},
		Todos:            []model.TodoItem{},
		TeamEvents:       []model.TeamEvent{},
	}
	if row.ReviewScopeJSON != "" {
		_ = json.Unmarshal([]byte(row.ReviewScopeJSON), &job.ReviewScope)
	}
	for _, comment := range row.Comments {
		job.Comments = append(job.Comments, reviewCommentFromDB(comment))
	}
	for _, trace := range row.Traces {
		job.Trace = append(job.Trace, traceEventFromDB(trace, false))
	}
	for _, todo := range row.Todos {
		job.Todos = append(job.Todos, model.TodoItem{
			Content: todo.Content,
			Status:  todo.Status,
			Order:   todo.SortOrder,
		})
	}
	return job
}

func reviewCommentFromDB(comment model.DBReviewComment) model.ReviewComment {
	return model.ReviewComment{
		File:               comment.File,
		Line:               comment.Line,
		Severity:           comment.Severity,
		Confidence:         comment.Confidence,
		Body:               comment.Body,
		Evidence:           comment.Evidence,
		Trigger:            comment.Trigger,
		Impact:             comment.Impact,
		Suggestion:         comment.Suggestion,
		VerificationStatus: comment.VerificationStatus,
		VerificationReason: comment.VerificationReason,
		TraceID:            comment.TraceID,
	}
}

func traceEventFromDB(trace model.DBTraceEvent, includeDetails bool) model.TraceEvent {
	traceStartedAt := trace.StartedAt
	if traceStartedAt == nil {
		traceStartedAt = &trace.CreatedAt
	}
	event := model.TraceEvent{
		ID:                        trace.TraceID,
		ParentID:                  trace.ParentID,
		Kind:                      trace.Kind,
		Status:                    trace.Status,
		Round:                     trace.Round,
		RetryCount:                trace.RetryCount,
		InputTokens:               trace.InputTokens,
		OutputTokens:              trace.OutputTokens,
		Model:                     trace.Model,
		CostMicros:                trace.CostMicros,
		EstimatedCost:             trace.EstimatedCost,
		InputPriceYuanPerMillion:  trace.InputPriceYuanPerMillion,
		OutputPriceYuanPerMillion: trace.OutputPriceYuanPerMillion,
		FinishReason:              trace.FinishReason,
		Origin:                    trace.Origin,
		CacheHit:                  trace.CacheHit,
		ToolCallID:                trace.ToolCallID,
		ToolVersion:               trace.ToolVersion,
		InputDigest:               trace.InputDigest,
		Tool:                      trace.Tool,
		DurationMs:                trace.DurationMs,
		StartedAt:                 *traceStartedAt,
		EndedAt:                   trace.EndedAt,
		At:                        trace.CreatedAt,
		Phase:                     trace.Phase,
	}
	if includeDetails {
		event.Input = trace.Input
		event.Output = trace.Output
		event.Prompt = trace.Prompt
		event.ModelReply = trace.ModelReply
	}
	return event
}

func (s *MySQLStore) RecordToolCall(jobPublicID, traceID, tool, status, input, output, callErr string, durationMs int64) error {
	var job model.DBReviewJob
	if err := s.db.Where("public_id = ?", jobPublicID).First(&job).Error; err != nil {
		return err
	}
	idempotencyKey := jobPublicID + ":" + traceID + ":" + tool
	call := model.DBToolCall{
		JobID:          job.ID,
		TraceID:        traceID,
		IdempotencyKey: idempotencyKey,
		Tool:           tool,
		Status:         status,
		InputHash:      hashText(input),
		Output:         output,
		Error:          callErr,
		DurationMs:     durationMs,
		CreatedAt:      time.Now().UTC(),
	}
	result := s.db.Where("idempotency_key = ?", idempotencyKey).FirstOrCreate(&call)
	if result.Error != nil {
		return fmt.Errorf("记录工具调用: %w", result.Error)
	}
	return nil
}

func (s *MySQLStore) findOrCreateJob(tx *gorm.DB, job *model.ReviewJob) (model.DBReviewJob, error) {
	var row model.DBReviewJob
	err := tx.Where("public_id = ?", job.ID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		now := job.UpdatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		row = model.DBReviewJob{
			PublicID:     job.ID,
			InputHash:    hashText(job.Source),
			BudgetMicros: reviewBudgetMicros(job),
			SpentMicros:  reviewSpentMicros(job),
			CreatedAt:    now,
		}
	} else if err != nil {
		return model.DBReviewJob{}, err
	}
	now := job.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row.TaskID = job.TaskID
	row.BackgroundTaskID = job.BackgroundTaskID
	row.SourceURL = job.Source
	row.Status = job.Status
	row.ReviewOutcome = job.ReviewOutcome
	reviewScope, err := json.Marshal(job.ReviewScope)
	if err != nil {
		return model.DBReviewJob{}, fmt.Errorf("编码审查范围: %w", err)
	}
	row.ReviewScopeJSON = string(reviewScope)
	row.BudgetMicros = reviewBudgetMicros(job)
	row.SpentMicros = reviewSpentMicros(job)
	row.ErrorMessage = job.Error
	if !job.StartedAt.IsZero() {
		startedAt := job.StartedAt
		row.StartedAt = &startedAt
	}
	row.FinishedAt = job.FinishedAt
	row.UpdatedAt = now
	row.Version++
	if row.InputHash == "" {
		row.InputHash = hashText(job.Source)
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.StartedAt == nil {
		startedAt := row.CreatedAt
		row.StartedAt = &startedAt
	}
	if row.ID == 0 {
		if err := tx.Create(&row).Error; err != nil {
			return model.DBReviewJob{}, fmt.Errorf("创建 review job: %w", err)
		}
		return row, nil
	}
	if err := tx.Model(&row).Updates(map[string]any{
		"task_id":            row.TaskID,
		"background_task_id": row.BackgroundTaskID,
		"source_url":         row.SourceURL,
		"status":             row.Status,
		"review_outcome":     row.ReviewOutcome,
		"review_scope_json":  row.ReviewScopeJSON,
		"budget_micros":      row.BudgetMicros,
		"spent_micros":       row.SpentMicros,
		"error_message":      row.ErrorMessage,
		"started_at":         row.StartedAt,
		"finished_at":        row.FinishedAt,
		"updated_at":         row.UpdatedAt,
		"version":            row.Version,
	}).Error; err != nil {
		return model.DBReviewJob{}, fmt.Errorf("更新 review job: %w", err)
	}
	return row, nil
}

func (s *MySQLStore) saveComments(tx *gorm.DB, jobID uint, job *model.ReviewJob) error {
	for _, comment := range job.Comments {
		fingerprint := commentFingerprint(job.ID, comment)
		var row model.DBReviewComment
		err := tx.Where("job_id = ? AND fingerprint = ?", jobID, fingerprint).First(&row).Error
		if err == gorm.ErrRecordNotFound {
			row = model.DBReviewComment{
				JobID:              jobID,
				TraceID:            comment.TraceID,
				Fingerprint:        fingerprint,
				File:               comment.File,
				Line:               comment.Line,
				Severity:           comment.Severity,
				Confidence:         comment.Confidence,
				Body:               comment.Body,
				Evidence:           comment.Evidence,
				Trigger:            comment.Trigger,
				Impact:             comment.Impact,
				Suggestion:         comment.Suggestion,
				VerificationStatus: comment.VerificationStatus,
				VerificationReason: comment.VerificationReason,
				Status:             "open",
				CreatedAt:          time.Now().UTC(),
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("保存 review comment: %w", err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{
			"trace_id":            comment.TraceID,
			"severity":            comment.Severity,
			"confidence":          comment.Confidence,
			"body":                comment.Body,
			"evidence":            comment.Evidence,
			"trigger":             comment.Trigger,
			"impact":              comment.Impact,
			"suggestion":          comment.Suggestion,
			"verification_status": comment.VerificationStatus,
			"verification_reason": comment.VerificationReason,
			"status":              "open",
		}).Error; err != nil {
			return fmt.Errorf("更新 review comment: %w", err)
		}
	}
	return nil
}

func (s *MySQLStore) saveTraces(tx *gorm.DB, jobID uint, job *model.ReviewJob) error {
	for _, trace := range job.Trace {
		var row model.DBTraceEvent
		err := tx.Where("trace_id = ?", trace.ID).First(&row).Error
		if err == gorm.ErrRecordNotFound {
			row = model.DBTraceEvent{
				JobID:                     jobID,
				TraceID:                   trace.ID,
				ParentID:                  trace.ParentID,
				Kind:                      trace.Kind,
				Status:                    trace.Status,
				Round:                     trace.Round,
				RetryCount:                trace.RetryCount,
				InputTokens:               trace.InputTokens,
				OutputTokens:              trace.OutputTokens,
				Model:                     trace.Model,
				CostMicros:                trace.CostMicros,
				EstimatedCost:             trace.EstimatedCost,
				InputPriceYuanPerMillion:  trace.InputPriceYuanPerMillion,
				OutputPriceYuanPerMillion: trace.OutputPriceYuanPerMillion,
				FinishReason:              trace.FinishReason,
				Origin:                    trace.Origin,
				CacheHit:                  trace.CacheHit,
				ToolCallID:                trace.ToolCallID,
				ToolVersion:               trace.ToolVersion,
				InputDigest:               trace.InputDigest,
				Tool:                      trace.Tool,
				Phase:                     trace.Phase,
				Input:                     trace.Input,
				Output:                    trace.Output,
				Prompt:                    trace.Prompt,
				ModelReply:                trace.ModelReply,
				DurationMs:                trace.DurationMs,
				StartedAt:                 traceStartedAt(trace),
				EndedAt:                   trace.EndedAt,
				CreatedAt:                 trace.At,
			}
			if row.Status == "" {
				row.Status = "succeeded"
			}
			if row.Kind == "" {
				row.Kind = legacyTraceKind(trace.Phase)
			}
			if row.CreatedAt.IsZero() {
				row.CreatedAt = time.Now().UTC()
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("保存 trace event: %w", err)
			}
			continue
		}
		if err != nil {
			return err
		}
		kind := trace.Kind
		if kind == "" {
			kind = legacyTraceKind(trace.Phase)
		}
		status := trace.Status
		if status == "" {
			status = "succeeded"
		}
		startedAt := traceStartedAt(trace)
		if err := tx.Model(&row).Updates(map[string]any{
			"parent_id":                     trace.ParentID,
			"kind":                          kind,
			"status":                        status,
			"round":                         trace.Round,
			"retry_count":                   trace.RetryCount,
			"input_tokens":                  trace.InputTokens,
			"output_tokens":                 trace.OutputTokens,
			"model":                         trace.Model,
			"cost_micros":                   trace.CostMicros,
			"estimated_cost":                trace.EstimatedCost,
			"input_price_yuan_per_million":  trace.InputPriceYuanPerMillion,
			"output_price_yuan_per_million": trace.OutputPriceYuanPerMillion,
			"finish_reason":                 trace.FinishReason,
			"origin":                        trace.Origin,
			"cache_hit":                     trace.CacheHit,
			"tool_call_id":                  trace.ToolCallID,
			"tool_version":                  trace.ToolVersion,
			"input_digest":                  trace.InputDigest,
			"tool":                          trace.Tool,
			"phase":                         trace.Phase,
			"input":                         trace.Input,
			"output":                        trace.Output,
			"prompt":                        trace.Prompt,
			"model_reply":                   trace.ModelReply,
			"duration_ms":                   trace.DurationMs,
			"started_at":                    startedAt,
			"ended_at":                      trace.EndedAt,
		}).Error; err != nil {
			return fmt.Errorf("更新 trace event: %w", err)
		}
	}
	return nil
}

func (s *MySQLStore) saveTodos(tx *gorm.DB, jobID uint, job *model.ReviewJob) error {
	for _, todo := range job.Todos {
		var row model.DBTodoItem
		err := tx.Where("job_id = ? AND sort_order = ?", jobID, todo.Order).First(&row).Error
		if err == gorm.ErrRecordNotFound {
			now := time.Now().UTC()
			row = model.DBTodoItem{
				JobID:     jobID,
				Content:   todo.Content,
				Status:    todo.Status,
				SortOrder: todo.Order,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("保存 todo: %w", err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{
			"content":    todo.Content,
			"status":     todo.Status,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return fmt.Errorf("更新 todo: %w", err)
		}
	}
	return nil
}

func (s *MySQLStore) saveTeamEvents(tx *gorm.DB, job *model.ReviewJob) error {
	for _, event := range job.TeamEvents {
		if event.ID == "" {
			event.ID = "legacy-" + hashText(event.From + event.To + event.Type + event.TaskID + event.Content + event.At.String())[:32]
		}
		row := model.DBTeamMessage{
			MessageID:      event.ID,
			JobID:          event.TaskID,
			FromAgent:      event.From,
			ToAgent:        event.To,
			MessageType:    event.Type,
			Content:        event.Content,
			DeliveryStatus: "acked",
			ConsumedAt:     &event.At,
			CreatedAt:      event.At,
		}
		if row.CreatedAt.IsZero() {
			row.CreatedAt = time.Now().UTC()
			row.ConsumedAt = &row.CreatedAt
		}
		result := tx.Where("message_id = ?", row.MessageID).FirstOrCreate(&row)
		if result.Error != nil {
			return fmt.Errorf("保存团队事件: %w", result.Error)
		}
	}
	return nil
}

func commentFingerprint(jobID string, comment model.ReviewComment) string {
	value := fmt.Sprintf("%s|%s|%d|%s|%s|%s", jobID, comment.File, comment.Line, comment.Severity, comment.Confidence, comment.Body)
	return hashText(value)
}

func hashText(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func legacyTraceKind(phase string) string {
	switch phase {
	case "task", "background", "planning", "memory", "context", "skill":
		return "input"
	case "subagent", "reasoning":
		return "model"
	default:
		return "tool"
	}
}

func traceStartedAt(trace model.TraceEvent) *time.Time {
	startedAt := trace.StartedAt
	if startedAt.IsZero() {
		startedAt = trace.At
	}
	return &startedAt
}

func reviewBudgetMicros(job *model.ReviewJob) int64 {
	if job.BudgetMicros > 0 {
		return job.BudgetMicros
	}
	return model.YuanToMicros(job.BudgetYuan)
}

func reviewSpentMicros(job *model.ReviewJob) int64 {
	if job.SpentMicros > 0 {
		return job.SpentMicros
	}
	return model.YuanToMicros(job.SpentYuan)
}
