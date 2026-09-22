package logic

import (
	"CR-Agent/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestCronSchedulerDeliversRecurringJobOncePerMinute(t *testing.T) {
	delivered := []model.CronJob{}
	scheduler, err := NewCronScheduler(
		filepath.Join(t.TempDir(), "cron.json"),
		time.Second,
		func(job model.CronJob) (string, error) {
			delivered = append(delivered, job)
			return "bg_test", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	job, err := scheduler.Schedule("*/5 * * * *", "https://example.com/review.diff", "Go", true, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 22, 10, 15, 20, 0, time.Local)
	if err := scheduler.Poll(now); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.DeliverPending(); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 || delivered[0].ID != job.ID {
		t.Fatalf("delivered = %#v", delivered)
	}
	if err := scheduler.Poll(now.Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.DeliverPending(); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 {
		t.Fatalf("same minute delivered %d times", len(delivered))
	}
	recorded, ok := scheduler.Get(job.ID)
	if !ok || recorded.PendingDelivery || recorded.LastBackgroundTaskID != "bg_test" {
		t.Fatalf("recorded = %#v, ok = %v", recorded, ok)
	}
}

func TestCronSchedulerDeletesOneTimeAndReloadsDurableJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.json")
	dispatcher := func(model.CronJob) (string, error) { return "bg_once", nil }
	scheduler, err := NewCronScheduler(path, time.Second, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	oneTime, err := scheduler.Schedule("0 9 * * 1-5", "https://example.com/once.diff", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := scheduler.Schedule("0 9 * * 1-5", "https://example.com/durable.diff", "", true, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 21, 9, 0, 0, 0, time.Local)
	if err := scheduler.Poll(now); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.DeliverPending(); err != nil {
		t.Fatal(err)
	}
	if _, ok := scheduler.Get(oneTime.ID); ok {
		t.Fatal("one-time job was not deleted after delivery")
	}
	reloaded, err := NewCronScheduler(path, time.Second, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if restored, ok := reloaded.Get(durable.ID); !ok || restored.Source != durable.Source {
		t.Fatalf("restored = %#v, ok = %v", restored, ok)
	}
}

func TestCronExpressionValidation(t *testing.T) {
	for _, expression := range []string{"* * * * *", "*/15 9-17 * * 1,2,3,4,5", "0 0 1 1 *"} {
		if _, err := parseCron(expression); err != nil {
			t.Fatalf("parseCron(%q): %v", expression, err)
		}
	}
	for _, expression := range []string{"* * * *", "*/0 * * * *", "60 * * * *", "1-0 * * * *", "1/2 * * * *"} {
		if _, err := parseCron(expression); err == nil {
			t.Fatalf("parseCron(%q) unexpectedly succeeded", expression)
		}
	}
}
