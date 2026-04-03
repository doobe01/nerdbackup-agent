package api

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

// retryable checks if a status code warrants a retry.
func retryable(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// doWithRetry executes an HTTP request with exponential backoff.
// Base delay: 1s, max delay: 60s, jitter: ±25%.
func doWithRetry(ctx context.Context, client *http.Client, req *http.Request, maxRetries int) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt)
			logging.Log.Debug().
				Int("attempt", attempt).
				Dur("delay", delay).
				Str("url", req.URL.Path).
				Msg("Retrying request")

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue // network error — retry
		}

		if !retryable(resp.StatusCode) {
			return resp, nil // success or non-retryable error
		}

		// Retryable status — close body and retry
		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, req.URL.Path)
	}

	return nil, fmt.Errorf("exhausted %d retries: %w", maxRetries, lastErr)
}

func backoffDelay(attempt int) time.Duration {
	base := math.Pow(2, float64(attempt)) * 1000 // 1s, 2s, 4s, 8s, 16s...
	if base > 60000 {
		base = 60000 // cap at 60s
	}
	// Add ±25% jitter
	jitter := base * 0.25 * (2*rand.Float64() - 1)
	ms := base + jitter
	return time.Duration(ms) * time.Millisecond
}
