package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

func TestIsNonRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"non-API error", errors.New("network timeout"), false},
		{"200 OK", &googleapi.Error{Code: http.StatusOK}, false},
		{"400 Bad Request", &googleapi.Error{Code: http.StatusBadRequest}, true},
		{"401 Unauthorized", &googleapi.Error{Code: http.StatusUnauthorized}, true},
		{"403 Forbidden", &googleapi.Error{Code: http.StatusForbidden}, true},
		{"404 Not Found", &googleapi.Error{Code: http.StatusNotFound}, true},
		{"429 Too Many Requests", &googleapi.Error{Code: http.StatusTooManyRequests}, false},
		{"500 Internal Server Error", &googleapi.Error{Code: http.StatusInternalServerError}, false},
		{"503 Service Unavailable", &googleapi.Error{Code: http.StatusServiceUnavailable}, false},
		{"wrapped API error", fmt.Errorf("wrap: %w", &googleapi.Error{Code: http.StatusForbidden}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNonRetryable(tt.err); got != tt.expected {
				t.Errorf("IsNonRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestDo_Success(t *testing.T) {
	calls := 0
	op := func() error {
		calls++
		return nil
	}
	err := Do(context.Background(), fastConfig(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDo_RetryableThenSuccess(t *testing.T) {
	calls := 0
	op := func() error {
		calls++
		if calls < 3 {
			return &googleapi.Error{Code: http.StatusTooManyRequests}
		}
		return nil
	}
	err := Do(context.Background(), fastConfig(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_NonRetryable(t *testing.T) {
	calls := 0
	wantErr := &googleapi.Error{Code: http.StatusForbidden}
	op := func() error {
		calls++
		return wantErr
	}
	err := Do(context.Background(), fastConfig(), op)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (non-retryable), got %d", calls)
	}
}

func TestDo_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	op := func() error {
		calls++
		if calls == 2 {
			cancel()
		}
		return &googleapi.Error{Code: http.StatusTooManyRequests}
	}
	err := Do(ctx, fastConfig(), op)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls > 8 {
		t.Errorf("expected at most 8 calls, got %d", calls)
	}
}

func TestDo_MaxAttempts(t *testing.T) {
	calls := 0
	wantErr := &googleapi.Error{Code: http.StatusTooManyRequests}
	op := func() error {
		calls++
		return wantErr
	}
	err := Do(context.Background(), fastConfig(), op)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 8 {
		t.Errorf("expected 8 calls (max attempts), got %d", calls)
	}
}

func fastConfig() Config {
	return Config{
		Initial:     1 * time.Millisecond,
		Max:         5 * time.Millisecond,
		Jitter:      0,
		MaxAttempts: 8,
	}
}
