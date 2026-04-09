//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"text/template"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.nerdbackup.agent</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Binary}}</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>{{.Home}}/.nerdbackup/agent.log</string>
	<key>StandardErrorPath</key>
	<string>{{.Home}}/.nerdbackup/agent.err</string>
</dict>
</plist>
`

const label = "com.nerdbackup.agent"

type serviceData struct {
	Binary string
	Home   string
}

func Install(binaryPath string) error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	_ = os.MkdirAll(filepath.Join(home, ".nerdbackup"), 0700)

	plistPath := filepath.Join(dir, label+".plist")
	f, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("plist").Parse(plistTemplate))
	if err := tmpl.Execute(f, serviceData{Binary: binaryPath, Home: home}); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Try modern launchctl bootstrap first (macOS 10.13+), fall back to load
	uid := getUID()
	domain := fmt.Sprintf("gui/%s", uid)

	// Unload existing if present (ignore error)
	_ = exec.Command("launchctl", "bootout", domain+"/"+label).Run()

	// Try bootstrap (modern)
	if err := exec.Command("launchctl", "bootstrap", domain, plistPath).Run(); err != nil {
		// Fall back to legacy load for older macOS
		if loadErr := exec.Command("launchctl", "load", plistPath).Run(); loadErr != nil {
			return fmt.Errorf("launchctl failed (tried bootstrap and load): %w", loadErr)
		}
	}

	fmt.Printf("Service installed at %s\n", plistPath)
	fmt.Println("Agent is now running as a launchd service.")
	fmt.Println("  launchctl list | grep nerdbackup")
	fmt.Println("  tail -f ~/.nerdbackup/agent.log")
	return nil
}

func getUID() string {
	u, err := user.Current()
	if err != nil {
		return "501" // Default macOS user UID
	}
	return u.Uid
}

func Uninstall() error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	uid := getUID()
	domain := fmt.Sprintf("gui/%s", uid)
	_ = exec.Command("launchctl", "bootout", domain+"/"+label).Run()
	_ = os.Remove(plistPath)
	fmt.Println("Service uninstalled.")
	return nil
}

func Start() error {
	uid := getUID()
	domain := fmt.Sprintf("gui/%s", uid)
	return exec.Command("launchctl", "kickstart", "-k", domain+"/"+label).Run()
}

func Stop() error {
	uid := getUID()
	domain := fmt.Sprintf("gui/%s", uid)
	return exec.Command("launchctl", "kill", "SIGTERM", domain+"/"+label).Run()
}

func Restart() error {
	if err := Stop(); err != nil {
		return err
	}
	return Start()
}

func IsWindowsService() bool { return false }

func RunAsService(_ func() error, _ func()) error {
	return fmt.Errorf("RunAsService is only supported on Windows")
}
