package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

func TestInternalDateToDate(t *testing.T) {
	// 2020-01-02 03:04:05 UTC in milliseconds
	ts := int64(1577936645000)
	got := internalDateToDate(ts)
	// convert to local date for expected
	expected := time.UnixMilli(ts).In(time.Local).Format("2006-01-02")
	if got != expected {
		t.Fatalf("expected %s got %s", expected, got)
	}

	if internalDateToDate(0) != "" {
		t.Fatalf("expected empty string for zero timestamp")
	}
}

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
			if got := isNonRetryable(tt.err); got != tt.expected {
				t.Errorf("isNonRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestRetryWithBackoff_Success(t *testing.T) {
	calls := 0
	op := func() error {
		calls++
		return nil
	}
	err := retryWithBackoff(context.Background(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryWithBackoff_RetryableThenSuccess(t *testing.T) {
	calls := 0
	op := func() error {
		calls++
		if calls < 3 {
			return &googleapi.Error{Code: http.StatusTooManyRequests}
		}
		return nil
	}
	err := retryWithBackoff(context.Background(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryWithBackoff_NonRetryable(t *testing.T) {
	calls := 0
	wantErr := &googleapi.Error{Code: http.StatusForbidden}
	op := func() error {
		calls++
		return wantErr
	}
	err := retryWithBackoff(context.Background(), op)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (non-retryable), got %d", calls)
	}
}

func TestRetryWithBackoff_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	op := func() error {
		calls++
		if calls == 2 {
			cancel()
		}
		return &googleapi.Error{Code: http.StatusTooManyRequests}
	}
	err := retryWithBackoff(ctx, op)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls > 8 {
		t.Errorf("expected at most 8 calls, got %d", calls)
	}
}

func TestRetryWithBackoff_MaxAttempts(t *testing.T) {
	calls := 0
	wantErr := &googleapi.Error{Code: http.StatusTooManyRequests}
	op := func() error {
		calls++
		return wantErr
	}
	err := retryWithBackoff(context.Background(), op)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 8 {
		t.Errorf("expected 8 calls (max attempts), got %d", calls)
	}
}
