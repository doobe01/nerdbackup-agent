//go:build windows

package service

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/doobe01/nerdbackup-agent/internal/config"
	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

const serviceName = "NerdBackupAgent"

// Install registers the NerdBackup Agent as a Windows Service.
func Install(binaryPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	// Check if service already exists
	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		// Remove existing service first
		logging.Log.Info().Msg("Removing existing service before reinstall")
		_ = Uninstall()
	}

	s, err = m.CreateService(serviceName, binaryPath, mgr.Config{
		DisplayName: "NerdBackup Agent",
		Description: "Restic-powered backup agent managed by NerdBackup. Handles scheduled backups, heartbeats, and configuration sync.",
		StartType:   mgr.StartAutomatic,
	}, "run")
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Set recovery actions: restart on failure
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, uint32(86400)) // reset failure count after 24 hours
	if err != nil {
		logging.Log.Warn().Err(err).Msg("Failed to set recovery actions")
	}

	// Copy config to system-wide path so LOCAL SYSTEM can read it
	if err := config.CopyToSystemPath(); err != nil {
		logging.Log.Warn().Err(err).Msg("Failed to copy config to system path — service may not start")
	} else {
		logging.Log.Info().Str("path", config.SystemConfigPath()).Msg("Config copied to system path")
	}

	logging.Log.Info().Str("service", serviceName).Msg("Windows service installed")
	fmt.Printf("Windows service '%s' installed.\n", serviceName)
	fmt.Println("  Visible in: services.msc")
	fmt.Println("  Start type: Automatic")
	return nil
}

// Uninstall removes the Windows Service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	err = s.Delete()
	if err != nil {
		return fmt.Errorf("delete service: %w", err)
	}

	logging.Log.Info().Str("service", serviceName).Msg("Windows service uninstalled")
	fmt.Printf("Windows service '%s' removed.\n", serviceName)
	return nil
}

// Start starts the Windows Service.
func Start() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	err = s.Start()
	if err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	logging.Log.Info().Str("service", serviceName).Msg("Windows service started")
	fmt.Printf("Windows service '%s' started.\n", serviceName)
	return nil
}

// Stop stops the Windows Service.
func Stop() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service: %w", err)
	}

	// Wait for service to actually stop
	for i := 0; i < 30; i++ {
		if status.State == svc.Stopped {
			break
		}
		time.Sleep(time.Second)
		status, _ = s.Query()
	}

	logging.Log.Info().Str("service", serviceName).Msg("Windows service stopped")
	fmt.Printf("Windows service '%s' stopped.\n", serviceName)
	return nil
}

// Restart stops then starts the Windows Service.
func Restart() error {
	if err := Stop(); err != nil {
		return err
	}
	return Start()
}

// IsWindowsService returns true if the process is running as a Windows Service.
func IsWindowsService() bool {
	is, _ := svc.IsWindowsService()
	return is
}

// RunAsService runs the agent as a Windows Service using the provided run function.
func RunAsService(runFunc func() error, stopFunc func()) error {
	return svc.Run(serviceName, &agentService{
		runFunc:  runFunc,
		stopFunc: stopFunc,
	})
}

type agentService struct {
	runFunc  func() error
	stopFunc func()
}

func (s *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	// Run agent in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.runFunc()
	}()

	for {
		select {
		case err := <-errCh:
			if err != nil {
				logging.Log.Error().Err(err).Msg("Service run function exited with error")
				changes <- svc.Status{State: svc.StopPending}
				return true, 1
			}
			changes <- svc.Status{State: svc.StopPending}
			return false, 0

		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				logging.Log.Info().Msg("Service stop/shutdown requested")
				changes <- svc.Status{State: svc.StopPending}
				s.stopFunc()
				return false, 0
			}
		}
	}
}
