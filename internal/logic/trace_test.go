package logic

import (
	"CR-Agent/internal/model"
	"testing"
	"time"
)

func TestTraceRecorderPersistsExplicitSpanBoundaries(t *testing.T) {
	job := &model.ReviewJob{ID: "trace-job", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	started := time.Now().UTC().Add(-150 * time.Millisecond)
	ended := started.Add(125 * time.Millisecond)

	id, duration := recorder.RecordAt(
		"model",
		"deepseek-review",
		"reasoning",
		"prompt",
		"parent-1",
		started,
		ended,
		TraceResult{Output: "ok", ModelReply: "[]"},
	)
	recorder.Flush()

	if len(job.Trace) != 1 {
		t.Fatalf("trace length = %d, want 1", len(job.Trace))
	}
	event := job.Trace[0]
	if event.ID != id || event.ParentID != "parent-1" || event.Kind != "model" || event.Status != "succeeded" {
		t.Fatalf("unexpected trace metadata: %#v", event)
	}
	if !event.StartedAt.Equal(started) || event.EndedAt == nil || !event.EndedAt.Equal(ended) {
		t.Fatalf("unexpected boundaries: started=%v ended=%v", event.StartedAt, event.EndedAt)
	}
	if duration != 125 || event.DurationMs != 125 {
		t.Fatalf("duration = %d, event duration = %d, want 125", duration, event.DurationMs)
	}
}

func TestTraceRecorderPreservesDeniedStatus(t *testing.T) {
	job := &model.ReviewJob{ID: "trace-denied", Trace: []model.TraceEvent{}}
	recorder := newTraceRecorder(job)
	recorder.Record(
		"tool",
		"diff_fetcher",
		"permission",
		"source",
		"",
		TraceResult{Status: "denied", Err: errTraceTestDenied},
	)
	recorder.Flush()

	if job.Trace[0].Status != "denied" {
		t.Fatalf("status = %q, want denied", job.Trace[0].Status)
	}
}

var errTraceTestDenied = traceTestError("permission denied")

type traceTestError string

func (e traceTestError) Error() string { return string(e) }
