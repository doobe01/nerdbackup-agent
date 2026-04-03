package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

type Client struct {
	baseURL    string
	agentID    string
	agentToken string
	httpClient *http.Client
}

func NewClient(baseURL, agentID, agentToken string) *Client {
	return &Client{
		baseURL:    baseURL,
		agentID:    agentID,
		agentToken: agentToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Register registers a new agent with the NerdBackup API.
// Uses an API key for initial auth (not agent token).
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

// SendHeartbeat sends a heartbeat to the NerdBackup API.
func (c *Client) SendHeartbeat(req HeartbeatRequest) error {
	return c.post(fmt.Sprintf("/api/v1/agents/%s/heartbeat", c.agentID), req, nil)
}

// ReportJob reports a completed/failed job to the API.
func (c *Client) ReportJob(req JobReportRequest) error {
	return c.post(fmt.Sprintf("/api/v1/agents/%s/report", c.agentID), req, nil)
}

// GetRepos fetches the repo configurations for this agent.
func (c *Client) GetRepos() ([]RepoConfig, error) {
	var repos []RepoConfig
	err := c.get(fmt.Sprintf("/api/v1/agents/%s/repos", c.agentID), &repos)
	return repos, err
}

func (c *Client) get(path string, out interface{}) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s failed (%d): %s", path, resp.StatusCode, string(b))
	}

	var envelope ApiResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, out)
}

func (c *Client) post(path string, body interface{}, out interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		logging.Log.Error().Int("status", resp.StatusCode).Str("path", path).Msg(string(b))
		return fmt.Errorf("POST %s failed (%d)", path, resp.StatusCode)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.agentToken)
	req.Header.Set("Accept", "application/json")
}
