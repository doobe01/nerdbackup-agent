package api

import (
	"testing"
	"time"
)

func TestRetryable(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{200, false},
		{201, false},
		{204, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{409, false},
		{429, true},  // rate limit
		{500, true},  // server error
		{502, true},  // bad gateway
		{503, true},  // service unavailable
		{504, true},  // gateway timeout
	}

	for _, tt := range tests {
		got := retryable(tt.status)
		if got != tt.want {
			t.Errorf("retryable(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	// Backoff should increase with each attempt
	prev := time.Duration(0)
	for attempt := 1; attempt <= 5; attempt++ {
		delay := backoffDelay(attempt)

		// Should be positive
		if delay <= 0 {
			t.Errorf("attempt %d: delay should be positive, got %v", attempt, delay)
		}

		// Should generally increase (allowing for jitter)
		if attempt > 1 && delay < prev/3 {
			t.Errorf("attempt %d: delay %v is too small compared to previous %v", attempt, delay, prev)
		}

		// Should not exceed 60s + jitter
		if delay > 75*time.Second {
			t.Errorf("attempt %d: delay %v exceeds max (75s with jitter)", attempt, delay)
		}

		prev = delay
	}

	// High attempt should cap at ~60s
	delay := backoffDelay(20)
	if delay > 75*time.Second {
		t.Errorf("attempt 20: delay %v should be capped", delay)
	}
}
