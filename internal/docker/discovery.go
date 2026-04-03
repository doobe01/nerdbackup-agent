package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

// IsDockerAvailable checks if Docker CLI is installed and accessible.
func IsDockerAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	return cmd.Run() == nil
}

// DiscoverVolumes lists all Docker volumes on the host.
func DiscoverVolumes(ctx context.Context) ([]DockerVolume, error) {
	// List volumes
	out, err := exec.CommandContext(ctx, "docker", "volume", "ls", "--format", "{{json .}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}

	var volumes []DockerVolume
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var vol struct {
			Name       string `json:"Name"`
			Driver     string `json:"Driver"`
			Mountpoint string `json:"Mountpoint"`
			Scope      string `json:"Scope"`
			Labels     string `json:"Labels"`
		}
		if err := json.Unmarshal([]byte(line), &vol); err != nil {
			logging.Log.Debug().Str("line", line).Msg("Failed to parse volume JSON")
			continue
		}

		// Get detailed info including mountpoint
		inspectOut, err := exec.CommandContext(ctx, "docker", "volume", "inspect", vol.Name, "--format", "{{.Mountpoint}}").CombinedOutput()
		if err == nil {
			vol.Mountpoint = strings.TrimSpace(string(inspectOut))
		}

		// Find containers using this volume
		containers := findContainersUsingVolume(ctx, vol.Name)

		labels := make(map[string]string)
		if vol.Labels != "" {
			for _, pair := range strings.Split(vol.Labels, ",") {
				parts := strings.SplitN(pair, "=", 2)
				if len(parts) == 2 {
					labels[parts[0]] = parts[1]
				}
			}
		}

		volumes = append(volumes, DockerVolume{
			Name:       vol.Name,
			Driver:     vol.Driver,
			Mountpoint: vol.Mountpoint,
			Labels:     labels,
			Scope:      vol.Scope,
			Containers: containers,
		})
	}

	logging.Log.Info().Int("count", len(volumes)).Msg("Discovered Docker volumes")
	return volumes, nil
}

// DiscoverComposeProjects lists running Docker Compose projects.
func DiscoverComposeProjects(ctx context.Context) ([]ComposeProject, error) {
	out, err := exec.CommandContext(ctx, "docker", "compose", "ls", "--format", "json").CombinedOutput()
	if err != nil {
		// docker compose might not be available
		return nil, fmt.Errorf("docker compose ls: %w", err)
	}

	var rawProjects []struct {
		Name       string `json:"Name"`
		Status     string `json:"Status"`
		ConfigFile string `json:"ConfigFiles"`
	}
	if err := json.Unmarshal(out, &rawProjects); err != nil {
		return nil, fmt.Errorf("parse compose projects: %w", err)
	}

	var projects []ComposeProject
	for _, rp := range rawProjects {
		projects = append(projects, ComposeProject{
			Name:       rp.Name,
			Status:     rp.Status,
			ConfigFile: rp.ConfigFile,
		})
	}

	return projects, nil
}

func findContainersUsingVolume(ctx context.Context, volumeName string) []string {
	out, err := exec.CommandContext(ctx,
		"docker", "ps", "-a",
		"--filter", fmt.Sprintf("volume=%s", volumeName),
		"--format", "{{.Names}}",
	).CombinedOutput()
	if err != nil {
		return nil
	}

	var containers []string
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name != "" {
			containers = append(containers, name)
		}
	}
	return containers
}
