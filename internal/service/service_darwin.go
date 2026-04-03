//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
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

	// Ensure log directory exists
	os.MkdirAll(filepath.Join(home, ".nerdbackup"), 0700)

	plistPath := filepath.Join(dir, "com.nerdbackup.agent.plist")
	f, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("plist").Parse(plistTemplate))
	if err := tmpl.Execute(f, serviceData{Binary: binaryPath, Home: home}); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	fmt.Printf("Service installed at %s\n", plistPath)
	fmt.Println("Agent is now running as a launchd service.")
	fmt.Println("  launchctl list | grep nerdbackup")
	fmt.Println("  tail -f ~/.nerdbackup/agent.log")
	return nil
}
