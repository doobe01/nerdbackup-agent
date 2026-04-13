// Package ws provides a WebSocket client for real-time bidirectional
// communication between the NerdBackup agent and the platform.
//
// This replaces 5-minute polling with instant command delivery and
// progress streaming. Falls back to HTTP polling when the WebSocket
// connection is unavailable.
package ws

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"net/http"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

// Command represents a server-to-agent message received over WebSocket.
type Command struct {
	Type   string          `json:"type"`
	Action string          `json:"action"` // start_backup, pause, cancel, resume, config_update
	JobID  string          `json:"job_id,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Message represents an agent-to-server message sent over WebSocket.
type Message struct {
	Type string      `json:"type"` // heartbeat, progress, job_report
	Data interface{} `json:"data"`
}

// ProgressData holds real-time backup progress for streaming to the server.
type ProgressData struct {
	RepoID         string  `json:"repo_id"`
	JobID          string  `json:"job_id,omitempty"`
	PercentDone    float64 `json:"percent_done"`
	BytesProcessed int64   `json:"bytes_processed"`
	FilesProcessed int     `json:"files_processed"`
	CurrentFile    string  `json:"current_file,omitempty"`
	StartedAt      string  `json:"started_at"`
}

// HeartbeatData holds agent heartbeat information sent over WebSocket.
type HeartbeatData struct {
	AgentVersion  string `json:"agent_version"`
	ResticVersion string `json:"restic_version"`
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	Hostname      string `json:"hostname"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	DiskFreeBytes int64  `json:"disk_free_bytes"`
	CPUCount      int    `json:"cpu_count"`
	MemTotalBytes int64  `json:"memory_total_bytes"`
}

// Client manages the WebSocket connection to the NerdBackup server.
type Client struct {
	wsURL     string
	token     string
	agentID   string
	conn      *websocket.Conn
	mu        sync.RWMutex
	writeMu   sync.Mutex // serializes writes to prevent concurrent wsjson.Write
	connected bool
	onCommand func(Command)
}

// NewClient creates a new WebSocket client.
//
// apiURL is the base NerdBackup API URL (e.g., "https://nerdbackup.com").
// The client converts it to the WebSocket URL automatically.
// onCommand is called for each command received from the server.
func NewClient(apiURL, agentID, token string, onCommand func(Command)) *Client {
	wsURL := buildWSURL(apiURL, token)
	return &Client{
		wsURL:     wsURL,
		token:     token,
		agentID:   agentID,
		onCommand: onCommand,
	}
}

// buildWSURL converts an HTTP API base URL to a WebSocket URL.
// https://nerdbackup.com -> wss://nerdbackup.com/ws/agent
// http://localhost:3000  -> ws://localhost:3000/ws/agent
// Token is now sent via Authorization header, not query string.
func buildWSURL(apiURL, token string) string {
	u, err := url.Parse(apiURL)
	if err != nil {
		wsURL := strings.Replace(apiURL, "https://", "wss://", 1)
		wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
		return wsURL + "/ws/agent"
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}

	u.Path = "/ws/agent"
	_ = token // token passed via header in connect(), not in URL
	return u.String()
}

// IsConnected returns whether the WebSocket is currently connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Run starts the WebSocket connection with automatic reconnection.
// It blocks until ctx is cancelled. Use this in a goroutine.
//
// Reconnection uses exponential backoff from 1s to 60s with 25% jitter.
func (c *Client) Run(ctx context.Context) {
	delay := time.Second
	maxDelay := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			c.Close()
			return
		default:
		}

		err := c.connect(ctx)
		if err != nil {
			logging.Log.Warn().Err(err).Dur("retry_in", delay).Msg("WebSocket connection failed, retrying")

			// Jitter: +/- 25%
			jitter := time.Duration(float64(delay) * (0.75 + 0.5*rand.Float64()))
			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return
			}

			delay = min(delay*2, maxDelay)
			continue
		}

		delay = time.Second // reset backoff on successful connection
		c.readLoop(ctx)     // blocks until disconnect
	}
}

// connect dials the WebSocket server and sets up the connection.
func (c *Client) connect(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, c.wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + c.token},
		},
	})
	if err != nil {
		return err
	}

	// Set a generous read limit for large job reports
	conn.SetReadLimit(1 << 20) // 1MB

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	logging.Log.Info().Str("url", sanitizeURL(c.wsURL)).Msg("WebSocket connected")
	return nil
}

// readLoop reads messages from the server until the connection drops.
func (c *Client) readLoop(ctx context.Context) {
	for {
		var cmd Command
		err := wsjson.Read(ctx, c.conn, &cmd)
		if err != nil {
			// Check if this is a clean shutdown
			if ctx.Err() != nil {
				return
			}
			logging.Log.Warn().Err(err).Msg("WebSocket read error, will reconnect")
			c.mu.Lock()
			c.connected = false
			if c.conn != nil {
				c.conn.Close(websocket.StatusGoingAway, "read error")
				c.conn = nil
			}
			c.mu.Unlock()
			return
		}

		// Only dispatch actual commands — ignore acks and other message types
		if cmd.Type == "command" && cmd.Action != "" && c.onCommand != nil {
			go c.onCommand(cmd)
		}
	}
}

// Send sends a JSON message to the server.
// Thread-safe: RLock held through entire send to prevent conn becoming nil mid-write.
func (c *Client) Send(msg Message) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.conn == nil {
		return ErrNotConnected
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return wsjson.Write(ctx, c.conn, msg)
}

// SendHeartbeat sends a heartbeat message over WebSocket.
func (c *Client) SendHeartbeat(data HeartbeatData) error {
	return c.Send(Message{
		Type: "heartbeat",
		Data: data,
	})
}

// SendProgress sends real-time backup progress over WebSocket.
func (c *Client) SendProgress(progress ProgressData) error {
	return c.Send(Message{
		Type: "progress",
		Data: progress,
	})
}

// SendJobReport sends a completed/failed job report over WebSocket.
func (c *Client) SendJobReport(data interface{}) error {
	return c.Send(Message{
		Type: "job_report",
		Data: data,
	})
}

// Close cleanly shuts down the WebSocket connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	if c.conn != nil {
		c.conn.Close(websocket.StatusNormalClosure, "agent shutdown")
		c.conn = nil
	}
}

// sanitizeURL removes the token from the URL for safe logging.
func sanitizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid url]"
	}
	q := u.Query()
	if q.Has("token") {
		q.Set("token", "***")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// min returns the smaller of two durations.
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
