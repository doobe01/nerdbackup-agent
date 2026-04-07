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

// Init initializes the logger.
// isService: when true, skips stderr (Windows Services have no console).
func Init(debug bool, isService bool) {
	zerolog.TimeFieldFormat = time.RFC3339

	logPath := LogFilePath()
	logDir := filepath.Dir(logPath)
	_ = os.MkdirAll(logDir, 0777)

	// On Windows, grant LOCAL SYSTEM write access
	if runtime.GOOS == "windows" {
		_ = exec.Command("icacls", logDir, "/grant", "SYSTEM:(OI)(CI)(F)", "/T", "/Q").Run()
	}

	// Rotate at 10MB
	if info, err := os.Stat(logPath); err == nil && info.Size() > 10*1024*1024 {
		_ = os.Rename(logPath, logPath+".old")
	}

	logFile, fileErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

	var writers []io.Writer

	// File writer FIRST — critical for service mode.
	if fileErr == nil {
		writers = append(writers, &syncWriter{f: logFile})
	}

	// Console only in interactive mode (services have no console — stderr writes fail).
	if !isService {
		if debug {
			writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
		} else {
			writers = append(writers, os.Stderr)
		}
	}

	if len(writers) == 0 {
		// Last resort: write to stderr anyway (better than nothing)
		writers = append(writers, os.Stderr)
	}

	multi := io.MultiWriter(writers...)
	Log = zerolog.New(multi).
		With().Timestamp().Str("service", "nerdbackup-agent").Logger()

	if fileErr == nil && !isService {
		Log.Debug().Str("path", logPath).Msg("Logging to file")
	}
}
