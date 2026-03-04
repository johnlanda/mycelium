package embedder

import (
	"context"
	"errors"
	"math"
	"time"
)

// retryableError wraps errors that should be retried (e.g. HTTP 429).
type retryableError struct {
	err        error
	retryAfter time.Duration // from Retry-After header, zero if not present
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// doWithRetry calls fn up to maxRetries+1 times, only retrying on *retryableError.
// Uses exponential backoff: 2^attempt seconds, overridden by retryAfter if set.
// Respects context cancellation between retries.
func doWithRetry(ctx context.Context, maxRetries int, fn func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		var re *retryableError
		if !errors.As(err, &re) {
			return err
		}

		if attempt == maxRetries {
			return err
		}

		backoff := re.retryAfter
		if backoff == 0 {
			backoff = time.Duration(math.Pow(2, float64(attempt))) * time.Second
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return err
}
