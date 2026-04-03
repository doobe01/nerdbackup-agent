package logging

import (
	"context"
	"io"
	"sync"
	"time"
)

const maxBufferSize = 500

// LogSender is the interface for shipping logs. Avoids import cycle with api package.
type LogSender interface {
	ShipLogs(ctx context.Context, lines []string) error
}

// Shipper captures log output and ships it to the API in batches.
type Shipper struct {
	sender   LogSender
	buffer   []string
	mu       sync.Mutex
	interval time.Duration
}

// NewShipper creates a log shipper that batches and sends logs.
func NewShipper(sender LogSender, interval time.Duration) *Shipper {
	return &Shipper{
		sender:   sender,
		buffer:   make([]string, 0, maxBufferSize),
		interval: interval,
	}
}

// Write implements io.Writer so it can be used as a zerolog output.
func (s *Shipper) Write(p []byte) (n int, err error) {
	line := string(p)
	s.mu.Lock()
	if len(s.buffer) >= maxBufferSize {
		s.buffer = s.buffer[1:]
	}
	s.buffer = append(s.buffer, line)
	s.mu.Unlock()
	return len(p), nil
}

// Start begins the shipping loop. Call in a goroutine.
func (s *Shipper) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.flush(ctx)
			return
		case <-ticker.C:
			s.flush(ctx)
		}
	}
}

func (s *Shipper) flush(ctx context.Context) {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return
	}
	batch := make([]string, len(s.buffer))
	copy(batch, s.buffer)
	s.buffer = s.buffer[:0]
	s.mu.Unlock()

	_ = s.sender.ShipLogs(ctx, batch)
}

var _ io.Writer = (*Shipper)(nil)
