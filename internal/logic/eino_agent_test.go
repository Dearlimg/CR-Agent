package logic

import (
	"errors"
	"testing"
)

func TestIsRetryableModelError(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "eof", err: errors.New("Post request: EOF"), retryable: true},
		{name: "rate limit", err: errors.New("HTTP 429"), retryable: true},
		{name: "auth", err: errors.New("HTTP 401"), retryable: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isRetryableModelError(test.err); got != test.retryable {
				t.Fatalf("retryable=%v, want %v", got, test.retryable)
			}
		})
	}
}
