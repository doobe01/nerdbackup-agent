package docker

// DockerVolume represents a discovered Docker volume.
type DockerVolume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels"`
	Scope      string            `json:"scope"`
	Containers []string          `json:"containers"` // container names using this volume
}

// ComposeProject represents a discovered Docker Compose project.
type ComposeProject struct {
	Name       string             `json:"name"`
	Status     string             `json:"status"`
	ConfigFile string             `json:"config_file"`
	Services   []ComposeService   `json:"services"`
}

// ComposeService is a service within a compose project.
type ComposeService struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Status  string   `json:"status"`
	Volumes []string `json:"volumes"`
}
