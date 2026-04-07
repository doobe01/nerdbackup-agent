package logging

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rs/zerolog"
)

// grantSystemAccess runs icacls to grant LOCAL SYSTEM write access (Windows only).
func grantSystemAccess(dir string) error {
	return exec.Command("icacls", dir, "/grant", "SYSTEM:(OI)(CI)(F)", "/T", "/Q").Run()
}

var Log zerolog.Logger

// LogFilePath returns the path to the log file.
func LogFilePath() string {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "NerdBackup", "agent.log")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nerdbackup", "agent.log")
}

// syncWriter wraps a file and syncs after each write to ensure logs are flushed.
type syncWriter struct {
	f *os.File
}

func (w *syncWriter) Write(p []byte) (n int, err error) {
	n, err = w.f.Write(p)
	if err == nil {
		_ = w.f.Sync()
	}
	return
}

func Init(debug bool) {
	zerolog.TimeFieldFormat = time.RFC3339

	logPath := LogFilePath()
	logDir := filepath.Dir(logPath)
	_ = os.MkdirAll(logDir, 0777) // 0777 so Windows SYSTEM account can write

	// On Windows, explicitly grant SYSTEM write access to the log directory
	if runtime.GOOS == "windows" {
		_ = grantSystemAccess(logDir)
	}

	// Rotate: if log file > 10MB, rename to .old and start fresh
	if info, err := os.Stat(logPath); err == nil && info.Size() > 10*1024*1024 {
		_ = os.Rename(logPath, logPath+".old")
	}

	logFile, fileErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666) // 0666 for service access

	var writers []io.Writer

	if debug {
		writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
	} else {
		writers = append(writers, os.Stderr)
	}

	if fileErr == nil {
		writers = append(writers, &syncWriter{f: logFile})
	}

	multi := io.MultiWriter(writers...)
	Log = zerolog.New(multi).
		With().Timestamp().Str("service", "nerdbackup-agent").Logger()

	if fileErr == nil {
		Log.Debug().Str("path", logPath).Msg("Logging to file")
	}
}
