package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

type Client struct {
	baseURL    string
	agentID    string
	agentToken string
	httpClient *http.Client
	lastETag   string
}

func NewClient(baseURL, agentID, agentToken string) *Client {
	return &Client{
		baseURL:    baseURL,
		agentID:    agentID,
		agentToken: agentToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Register registers a new agent. Uses API key auth (not agent token).
func Register(baseURL, apiKey string, req RegisterAgentRequest) (*RegisterAgentResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", baseURL+"/api/v1/agents", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to register agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed (%d): %s", resp.StatusCode, string(b))
	}

	var result ApiResponse[RegisterAgentResponse]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// RegisterWithToken registers an agent using a pre-authenticated install token.
func RegisterWithToken(baseURL, installToken string, req RegisterAgentRequest) (*RegisterWithTokenResponse, error) {
	payload := map[string]string{
		"install_token": installToken,
		"platform":      req.Platform,
		"arch":          req.Arch,
		"hostname":      req.Hostname,
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequest("POST", baseURL+"/api/v1/agents/register-with-token", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to register agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed (%d): %s", resp.StatusCode, string(b))
	}

	var result ApiResponse[RegisterWithTokenResponse]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// Deregister deletes this agent from the NerdBackup API.
func (c *Client) Deregister(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.baseURL+"/api/v1/agents/"+c.agentID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deregister failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deregister failed (%d): %s", resp.StatusCode, string(b))
	}
	return nil
}

// GetPendingBackups fetches any backup triggers queued from the dashboard.
func (c *Client) GetPendingBackups(ctx context.Context) ([]PendingBackup, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+fmt.Sprintf("/api/v1/agents/%s/pending-backups", c.agentID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("pending-backups: HTTP %d", resp.StatusCode)
	}

	var result ApiResponse[[]PendingBackup]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetPendingFileDumps fetches any file download requests from the dashboard.
func (c *Client) GetPendingFileDumps(ctx context.Context) ([]PendingFileDump, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+fmt.Sprintf("/api/v1/agents/%s/pending-file-dumps", c.agentID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("pending-file-dumps: HTTP %d", resp.StatusCode)
	}

	var result ApiResponse[[]PendingFileDump]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// UploadFileDump uploads a dumped file back to the server (base64 encoded).
func (c *Client) UploadFileDump(ctx context.Context, requestID string, data []byte, fileName string) error {
	encoded := base64Encode(data)
	payload := map[string]string{
		"requestId": requestID,
		"fileName":  fileName,
		"data":      encoded,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+fmt.Sprintf("/api/v1/agents/%s/file-dump-result", c.agentID),
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload file dump: HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// GetPendingRestores fetches any restore requests queued from the dashboard.
func (c *Client) GetPendingRestores(ctx context.Context) ([]PendingRestore, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+fmt.Sprintf("/api/v1/agents/%s/pending-restores", c.agentID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("pending-restores: HTTP %d", resp.StatusCode)
	}

	var result ApiResponse[[]PendingRestore]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// SendHeartbeat sends a heartbeat. Returns config_changed flag.
func (c *Client) SendHeartbeat(ctx context.Context, req HeartbeatRequest) (*HeartbeatResponse, error) {
	var resp HeartbeatResponse
	err := c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/heartbeat", c.agentID), req, &resp, 3)
	return &resp, err
}

// ReportJob reports a completed/failed job. Retries 5x, then saves to pending.
func (c *Client) ReportJob(ctx context.Context, req JobReportRequest) error {
	err := c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/report", c.agentID), req, nil, 5)
	if err != nil {
		logging.Log.Warn().Err(err).Msg("Job report failed — saving to pending reports")
		SavePendingReport(req)
	}
	return err
}

// FlushPendingReports retries any saved pending reports.
func (c *Client) FlushPendingReports(ctx context.Context) {
	reports := LoadPendingReports()
	if len(reports) == 0 {
		return
	}

	logging.Log.Info().Int("count", len(reports)).Msg("Retrying pending reports")
	var remaining []JobReportRequest

	for _, r := range reports {
		err := c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/report", c.agentID), r, nil, 2)
		if err != nil {
			remaining = append(remaining, r)
		}
	}

	if len(remaining) == 0 {
		ClearPendingReports()
		logging.Log.Info().Msg("All pending reports flushed")
	} else {
		logging.Log.Warn().Int("remaining", len(remaining)).Msg("Some pending reports still failed")
	}
}

// GetRepos fetches repo configs with ETag support.
// Returns (repos, changed, error). If changed=false, repos is nil (304).
func (c *Client) GetRepos(ctx context.Context) ([]RepoConfig, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+fmt.Sprintf("/api/v1/agents/%s/repos", c.agentID), nil)
	if err != nil {
		return nil, false, err
	}
	c.setHeaders(req)
	if c.lastETag != "" {
		req.Header.Set("If-None-Match", c.lastETag)
	}

	resp, err := doWithRetry(ctx, c.httpClient, req, 3)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 304 {
		return nil, false, nil
	}

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("GET repos failed (%d): %s", resp.StatusCode, string(b))
	}

	if etag := resp.Header.Get("ETag"); etag != "" {
		c.lastETag = etag
	}

	var envelope ApiResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, false, err
	}

	var repos []RepoConfig
	if err := json.Unmarshal(envelope.Data, &repos); err != nil {
		return nil, false, err
	}

	return repos, true, nil
}

// ReportProgress sends real-time backup progress. Best-effort, 1 retry.
func (c *Client) ReportProgress(ctx context.Context, progress ProgressReport) {
	_ = c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/progress", c.agentID), progress, nil, 1)
}

// ShipLogs sends a batch of log lines.
func (c *Client) ShipLogs(ctx context.Context, lines []string) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/logs", c.agentID), LogBatch{Lines: lines}, nil, 2)
}

// PostDockerVolumes uploads discovered Docker volumes to the API.
func (c *Client) PostDockerVolumes(ctx context.Context, data interface{}) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/docker/volumes", c.agentID), data, nil, 2)
}

// GetLatestVersion checks for agent updates.
func (c *Client) GetLatestVersion(ctx context.Context) (*VersionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/downloads/agent/latest", nil)
	if err != nil {
		return nil, err
	}

	resp, err := doWithRetry(ctx, c.httpClient, req, 1)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var envelope ApiResponse[VersionInfo]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (c *Client) post(ctx context.Context, path string, body interface{}, out interface{}, maxRetries int) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := doWithRetry(ctx, c.httpClient, req, maxRetries)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s failed (%d): %s", path, resp.StatusCode, string(b))
	}

	if out != nil {
		var envelope ApiResponse[json.RawMessage]
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			return err
		}
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.agentToken)
	req.Header.Set("Accept", "application/json")
}
