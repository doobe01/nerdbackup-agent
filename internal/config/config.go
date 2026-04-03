package config

// AgentConfig is the persistent agent configuration stored locally.
type AgentConfig struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
	APIBaseURL string `json:"api_base_url"`
	APIKey     string `json:"api_key,omitempty"` // used during init only
	Name       string `json:"name"`
	Debug      bool   `json:"debug"`
}
