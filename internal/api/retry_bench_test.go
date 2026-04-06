package api

import "testing"

func BenchmarkBackoffDelay(b *testing.B) {
	for i := 0; i < b.N; i++ {
		backoffDelay(3)
	}
}

func BenchmarkRetryable(b *testing.B) {
	codes := []int{200, 400, 429, 500, 503}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range codes {
			retryable(c)
		}
	}
}
