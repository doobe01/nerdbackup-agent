package config

import "time"

// AgentConfig is the persistent agent configuration stored locally.
type AgentConfig struct {
	AgentID          string    `json:"agent_id"`
	AgentToken       string    `json:"agent_token"`
	APIBaseURL       string    `json:"api_base_url"`
	Name             string    `json:"name"`
	Debug            bool      `json:"debug"`
	InitializedRepos []string  `json:"initialized_repos,omitempty"`
	LastETag         string    `json:"last_etag,omitempty"`
	LastBackupAt     string    `json:"last_backup_at,omitempty"`
	StartedAt        time.Time `json:"started_at,omitempty"`
}

// IsRepoInitialized checks if a repo has been initialized before.
func (c *AgentConfig) IsRepoInitialized(repoID string) bool {
	for _, id := range c.InitializedRepos {
		if id == repoID {
			return true
		}
	}
	return false
}

// MarkRepoInitialized adds a repo to the initialized list.
func (c *AgentConfig) MarkRepoInitialized(repoID string) {
	if !c.IsRepoInitialized(repoID) {
		c.InitializedRepos = append(c.InitializedRepos, repoID)
	}
}
