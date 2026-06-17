// Package retry provides exponential backoff retry logic for Google API calls.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"google.golang.org/api/googleapi"
)

// Config controls the backoff behavior.
type Config struct {
	Initial     time.Duration // wait before the first retry
	Max         time.Duration // maximum wait between retries
	Jitter      time.Duration // maximum additional random wait
	MaxAttempts int           // maximum attempts before giving up
}

// DefaultConfig returns the standard retry settings used by the spam service.
func DefaultConfig() Config {
	return Config{
		Initial:     300 * time.Millisecond,
		Max:         10 * time.Second,
		Jitter:      200 * time.Millisecond,
		MaxAttempts: 8,
	}
}

// Do retries op with exponential backoff until it succeeds, the context is
// cancelled, or a non-retryable error occurs.
func Do(ctx context.Context, cfg Config, op func() error) error {
	wait := cfg.Initial
	for i := range cfg.MaxAttempts {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := op()
		if err == nil {
			return nil
		}

		if IsNonRetryable(err) {
			return err
		}
		if i == cfg.MaxAttempts-1 {
			return err
		}

		var jitter time.Duration
		if cfg.Jitter > 0 {
			jitter = time.Duration(rand.Intn(int(cfg.Jitter/time.Millisecond))) * time.Millisecond
		}
		time.Sleep(wait + jitter)
		wait *= 2
		if wait > cfg.Max {
			wait = cfg.Max
		}
	}
	return fmt.Errorf("retry attempts exhausted")
}

// IsNonRetryable checks whether an error is a Google API error with a
// non-retryable HTTP status code (4xx except 429).
func IsNonRetryable(err error) bool {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		return code != 429 && code >= 400 && code < 500
	}
	return false
}
