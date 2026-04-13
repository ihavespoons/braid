package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ihavespoons/braid/internal/config"
)

func TestIsRateLimitError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"rate limit exceeded", true},
		{"Rate-Limit hit", true},
		{"token limit", true},
		{"Token-limit breached", true},
		{"quota exhausted", true},
		{"too many requests", true},
		{"HTTP 429", true},
		{"API overloaded", true},
		{"at capacity", true},
		{"resource_exhausted", true},
		{"something else", false},
		{"404 not found", false},
		{"syntax error", false},
	}
	for _, tc := range cases {
		got := IsRateLimitError(errors.New(tc.msg))
		if got != tc.want {
			t.Errorf("IsRateLimitError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
	if IsRateLimitError(nil) {
		t.Error("nil error should not be rate-limit")
	}
}

func TestDo_SucceedsImmediately(t *testing.T) {
	cfg := config.DefaultRetryConfig()
	calls := 0
	val, err := Do(context.Background(), cfg, Options{}, func() (int, error) {
		calls++
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 || calls != 1 {
		t.Errorf("got val=%d calls=%d, want 42, 1", val, calls)
	}
}

func TestDo_NonRateLimitErrorPropagates(t *testing.T) {
	cfg := config.DefaultRetryConfig()
	calls := 0
	_, err := Do(context.Background(), cfg, Options{}, func() (int, error) {
		calls++
		return 0, errors.New("some other error")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("should not retry on non-rate-limit, calls=%d", calls)
	}
}

func TestDo_RetriesRateLimit(t *testing.T) {
	cfg := config.RetryConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MaxWait:      1 * time.Second,
	}
	calls := 0
	waitingCalls := 0
	retryCalls := 0
	opts := Options{
		OnWaiting: func(WaitingInfo) { waitingCalls++ },
		OnRetry:   func(RetryInfo) { retryCalls++ },
	}
	val, err := Do(context.Background(), cfg, opts, func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("rate limit exceeded")
		}
		return 99, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 99 || calls != 3 {
		t.Errorf("got val=%d calls=%d, want 99, 3", val, calls)
	}
	if waitingCalls != 2 {
		t.Errorf("waitingCalls = %d, want 2", waitingCalls)
	}
	if retryCalls != 2 {
		t.Errorf("retryCalls = %d, want 2", retryCalls)
	}
}

func TestDo_MaxWaitExceeded(t *testing.T) {
	cfg := config.RetryConfig{
		Enabled:      true,
		PollInterval: 10 * time.Millisecond,
		MaxWait:      15 * time.Millisecond,
	}
	calls := 0
	_, err := Do(context.Background(), cfg, Options{}, func() (int, error) {
		calls++
		return 0, errors.New("429 too many")
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	// First call, wait 10ms (total 10), second call, wait 5ms (total 15 = max), third call errors and we give up.
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestDo_ContextCancellation(t *testing.T) {
	cfg := config.RetryConfig{
		Enabled:      true,
		PollInterval: 500 * time.Millisecond,
		MaxWait:      10 * time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := Do(ctx, cfg, Options{}, func() (int, error) {
		return 0, errors.New("rate limit")
	})
	if err == nil {
		t.Fatal("expected error from cancellation")
	}
}

func TestDo_Disabled(t *testing.T) {
	cfg := config.RetryConfig{Enabled: false}
	calls := 0
	_, err := Do(context.Background(), cfg, Options{}, func() (int, error) {
		calls++
		return 0, errors.New("rate limit")
	})
	if err == nil {
		t.Fatal("expected error to propagate when disabled")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retries when disabled)", calls)
	}
}
