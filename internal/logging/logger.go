package logging

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var Log zerolog.Logger

func Init(debug bool) {
	zerolog.TimeFieldFormat = time.RFC3339

	if debug {
		Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"}).
			With().Timestamp().Str("service", "nerdbackup-agent").Logger()
	} else {
		Log = zerolog.New(os.Stderr).
			With().Timestamp().Str("service", "nerdbackup-agent").Logger()
	}
}
