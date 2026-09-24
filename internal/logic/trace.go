package logic

import (
	"CR-Agent/internal/model"
	"context"
	"sync"
	"time"
)

type traceRecorderKey struct{}
type traceParentKey struct{}

type TraceResult struct {
	Status                    string
	Output                    string
	ModelReply                string
	Err                       error
	Round                     int
	RetryCount                int
	InputTokens               int
	OutputTokens              int
	Model                     string
	CostMicros                int64
	EstimatedCost             bool
	InputPriceYuanPerMillion  float64
	OutputPriceYuanPerMillion float64
	FinishReason              string
	Origin                    string
	CacheHit                  bool
	ToolCallID                string
	ToolVersion               string
	InputDigest               string
}

type TraceRecorder struct {
	mu     sync.Mutex
	job    *model.ReviewJob
	events []model.TraceEvent
}

type TraceSpan struct {
	recorder *TraceRecorder
	id       string
	started  time.Time
	kind     string
	name     string
	phase    string
	input    string
	parentID string
	once     sync.Once
	duration int64
}

func newTraceRecorder(job *model.ReviewJob) *TraceRecorder {
	return &TraceRecorder{job: job, events: []model.TraceEvent{}}
}

func withTraceRecorder(ctx context.Context, recorder *TraceRecorder) context.Context {
	return context.WithValue(ctx, traceRecorderKey{}, recorder)
}

func traceRecorderFrom(ctx context.Context) *TraceRecorder {
	if recorder, ok := ctx.Value(traceRecorderKey{}).(*TraceRecorder); ok {
		return recorder
	}
	return nil
}

func withTraceParent(ctx context.Context, parentID string) context.Context {
	return context.WithValue(ctx, traceParentKey{}, parentID)
}

func traceParentFrom(ctx context.Context) string {
	parentID, _ := ctx.Value(traceParentKey{}).(string)
	return parentID
}

func (r *TraceRecorder) Start(kind, name, phase, input, parentID string) *TraceSpan {
	started := time.Now().UTC()
	return &TraceSpan{
		recorder: r,
		id:       id("trace:" + name),
		started:  started,
		kind:     kind,
		name:     name,
		phase:    phase,
		input:    input,
		parentID: parentID,
	}
}

func (r *TraceRecorder) Record(kind, name, phase, input, parentID string, result TraceResult) (string, int64) {
	span := r.Start(kind, name, phase, input, parentID)
	duration := span.End(result)
	return span.id, duration
}

func (r *TraceRecorder) RecordAt(kind, name, phase, input, parentID string, started, ended time.Time, result TraceResult) (string, int64) {
	span := &TraceSpan{
		recorder: r,
		id:       id("trace:" + name),
		started:  started,
		kind:     kind,
		name:     name,
		phase:    phase,
		input:    input,
		parentID: parentID,
	}
	duration := span.endAt(ended, result)
	return span.id, duration
}

func (s *TraceSpan) End(result TraceResult) int64 {
	if s == nil || s.recorder == nil {
		return 0
	}
	ended := time.Now().UTC()
	return s.endAt(ended, result)
}

func (s *TraceSpan) endAt(ended time.Time, result TraceResult) int64 {
	if s == nil || s.recorder == nil {
		return 0
	}
	var duration int64
	s.once.Do(func() {
		duration = ended.Sub(s.started).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		status := result.Status
		if status == "" {
			status = "succeeded"
		}
		output := result.Output
		if result.Err != nil {
			if status == "" || status == "succeeded" {
				status = "failed"
			}
			if output == "" {
				output = result.Err.Error()
			}
		}
		event := model.TraceEvent{
			ID:                        s.id,
			ParentID:                  s.parentID,
			Kind:                      s.kind,
			Status:                    status,
			Round:                     result.Round,
			RetryCount:                result.RetryCount,
			InputTokens:               result.InputTokens,
			OutputTokens:              result.OutputTokens,
			Model:                     result.Model,
			CostMicros:                result.CostMicros,
			EstimatedCost:             result.EstimatedCost,
			InputPriceYuanPerMillion:  result.InputPriceYuanPerMillion,
			OutputPriceYuanPerMillion: result.OutputPriceYuanPerMillion,
			FinishReason:              result.FinishReason,
			Origin:                    result.Origin,
			CacheHit:                  result.CacheHit,
			ToolCallID:                result.ToolCallID,
			ToolVersion:               result.ToolVersion,
			InputDigest:               result.InputDigest,
			Tool:                      s.name,
			Input:                     s.input,
			Output:                    output,
			ModelReply:                result.ModelReply,
			At:                        ended,
			StartedAt:                 s.started,
			EndedAt:                   &ended,
			DurationMs:                duration,
			Phase:                     s.phase,
		}
		s.recorder.mu.Lock()
		s.recorder.events = append(s.recorder.events, event)
		s.recorder.mu.Unlock()
		s.duration = duration
	})
	return duration
}

func (s *TraceSpan) ID() string {
	if s == nil {
		return ""
	}
	return s.id
}

func (s *TraceSpan) DurationMs() int64 {
	if s == nil {
		return 0
	}
	return s.duration
}

func (r *TraceRecorder) Flush() {
	if r == nil || r.job == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return
	}
	r.job.Trace = append(r.job.Trace, r.events...)
	r.events = r.events[:0]
}
