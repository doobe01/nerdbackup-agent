//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const systemdTemplate = `[Unit]
Description=NerdBackup Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{.Binary}} run
Restart=always
RestartSec=10
Environment=HOME={{.Home}}

[Install]
WantedBy=default.target
`

type serviceData struct {
	Binary string
	Home   string
}

func Install(binaryPath string) error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create systemd dir: %w", err)
	}

	unitPath := filepath.Join(dir, "nerdbackup-agent.service")
	f, err := os.Create(unitPath)
	if err != nil {
		return fmt.Errorf("create unit file: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("systemd").Parse(systemdTemplate))
	if err := tmpl.Execute(f, serviceData{Binary: binaryPath, Home: home}); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	// Reload and enable
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", "nerdbackup-agent").Run(); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}

	fmt.Printf("Service installed at %s\n", unitPath)
	fmt.Println("Agent is now running as a systemd user service.")
	fmt.Println("  systemctl --user status nerdbackup-agent")
	fmt.Println("  journalctl --user -u nerdbackup-agent -f")
	return nil
}

func Uninstall() error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", "nerdbackup-agent").Run()
	home, _ := os.UserHomeDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "nerdbackup-agent.service")
	_ = os.Remove(unitPath)
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	fmt.Println("Service uninstalled.")
	return nil
}

func Start() error {
	return exec.Command("systemctl", "--user", "start", "nerdbackup-agent").Run()
}

func Stop() error {
	return exec.Command("systemctl", "--user", "stop", "nerdbackup-agent").Run()
}

func Restart() error {
	return exec.Command("systemctl", "--user", "restart", "nerdbackup-agent").Run()
}

func IsWindowsService() bool { return false }

func RunAsService(_ func() error, _ func()) error {
	return fmt.Errorf("RunAsService is only supported on Windows")
}
