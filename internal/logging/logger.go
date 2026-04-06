package logging

import (
	"io"
	"os"
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

func Init(debug bool) {
	zerolog.TimeFieldFormat = time.RFC3339

	// Set up file logging
	logPath := LogFilePath()
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)

	logFile, fileErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	var writers []io.Writer

	// Console writer
	if debug {
		writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
	} else {
		writers = append(writers, os.Stderr)
	}

	// File writer (JSON format for machine parsing)
	if fileErr == nil {
		writers = append(writers, logFile)

		// Truncate if over 10MB
		if info, err := logFile.Stat(); err == nil && info.Size() > 10*1024*1024 {
			_ = logFile.Truncate(0)
			_, _ = logFile.Seek(0, 0)
		}
	}

	multi := io.MultiWriter(writers...)
	Log = zerolog.New(multi).
		With().Timestamp().Str("service", "nerdbackup-agent").Logger()

	if fileErr == nil {
		Log.Debug().Str("path", logPath).Msg("Logging to file")
	}
}
