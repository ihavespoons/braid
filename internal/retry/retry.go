// Package retry implements rate-limit detection and a fixed-interval retry
// loop for agent invocations.
package retry

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/ihavespoons/braid/internal/config"
)

// rateLimitPatterns is the ordered list of case-insensitive regexes that
// identify rate-limit / capacity errors in agent output.
var rateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rate.?limit`),
	regexp.MustCompile(`(?i)token.?limit`),
	regexp.MustCompile(`(?i)quota`),
	regexp.MustCompile(`(?i)too many requests`),
	regexp.MustCompile(`\b429\b`),
	regexp.MustCompile(`(?i)capacity`),
	regexp.MustCompile(`(?i)overloaded`),
	regexp.MustCompile(`(?i)resource_exhausted`),
}

// IsRateLimitError reports whether err's message matches a known
// rate-limit / capacity pattern.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, p := range rateLimitPatterns {
		if p.MatchString(msg) {
			return true
		}
	}
	return false
}

// WaitingInfo is passed to the OnWaiting callback when a retry is scheduled.
type WaitingInfo struct {
	Err         error
	NextRetryAt time.Time
	Attempt     int
}

// RetryInfo is passed to the OnRetry callback when a retry attempt starts.
type RetryInfo struct {
	Attempt int
}

// Options control the behavior of Do.
type Options struct {
	OnWaiting func(WaitingInfo)
	OnRetry   func(RetryInfo)
}

// Do invokes fn; if it fails with a rate-limit error, waits
// cfg.PollInterval and retries, up to cfg.MaxWait total wait time.
// Non-rate-limit errors are returned immediately. Honors ctx cancellation.
func Do[T any](ctx context.Context, cfg config.RetryConfig, opts Options, fn func() (T, error)) (T, error) {
	var zero T

	if !cfg.Enabled {
		return fn()
	}

	var totalWaited time.Duration
	attempt := 0

	for {
		attempt++
		if attempt > 1 && opts.OnRetry != nil {
			opts.OnRetry(RetryInfo{Attempt: attempt})
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		if !IsRateLimitError(err) {
			return zero, err
		}

		if totalWaited >= cfg.MaxWait {
			return zero, err
		}

		if ctx.Err() != nil {
			return zero, err
		}

		remaining := cfg.MaxWait - totalWaited
		wait := min(cfg.PollInterval, remaining)

		if opts.OnWaiting != nil {
			opts.OnWaiting(WaitingInfo{
				Err:         err,
				NextRetryAt: time.Now().Add(wait),
				Attempt:     attempt,
			})
		}

		if err := sleep(ctx, wait); err != nil {
			return zero, err
		}
		totalWaited += wait
	}
}

// sleep waits for d or until ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ErrMaxWaitExceeded is a sentinel (unused currently but exported for future
// callers that want to distinguish "gave up due to timeout" from the original
// rate-limit error).
var ErrMaxWaitExceeded = errors.New("retry: max wait exceeded")
