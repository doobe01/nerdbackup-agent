package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

const DefaultPort = 19284

type Server struct {
	provider StatusProvider
	server   *http.Server
}

func New(provider StatusProvider) *Server {
	return &Server{provider: provider}
}

func (s *Server) Start(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/repos", s.handleRepos)
	mux.HandleFunc("/progress", s.handleProgress)
	mux.HandleFunc("/backup/", s.handleTriggerBackup) // /backup/{repoID}
	mux.HandleFunc("/logs", s.handleLogs)

	s.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", DefaultPort),
		Handler: mux,
	}

	go func() {
		logging.Log.Info().Int("port", DefaultPort).Msg("Local API server started")
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Log.Warn().Err(err).Msg("Local API server error")
		}
	}()

	go func() {
		<-ctx.Done()
		s.server.Close()
	}()
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := s.provider.GetStatus()
	writeJSON(w, status)
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	repos := s.provider.GetRepos()
	writeJSON(w, repos)
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	progress := s.provider.GetProgress()
	if progress == nil {
		writeJSON(w, map[string]interface{}{"running": false})
		return
	}
	writeJSON(w, map[string]interface{}{"running": true, "progress": progress})
}

func (s *Server) handleTriggerBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Extract repo ID from path: /backup/{repoID}
	repoID := strings.TrimPrefix(r.URL.Path, "/backup/")
	if repoID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "repo ID required"})
		return
	}
	if err := s.provider.TriggerBackup(repoID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "triggered", "repo_id": repoID})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lines := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && n > 0 && n <= 500 {
		lines = n
	}

	// Read from agent log file
	logPath := logging.LogFilePath()
	data, err := os.ReadFile(logPath)
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"error": "cannot read log file",
			"path":  logPath,
		})
		return
	}

	allLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	start := len(allLines) - lines
	if start < 0 {
		start = 0
	}

	// Return the path along with lines — useful for tray to show the log location
	writeJSON(w, map[string]interface{}{
		"lines": allLines[start:],
		"total": len(allLines),
		"path":  logPath,
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
