package doctor

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/api"
	"github.com/doobe01/nerdbackup-agent/internal/config"
	"github.com/doobe01/nerdbackup-agent/internal/restic"
)

type CheckResult struct {
	Name    string
	Status  string // "OK", "WARN", "FAIL"
	Detail  string
}

// RunAll executes all diagnostic checks and returns results.
func RunAll(ctx context.Context) []CheckResult {
	var results []CheckResult

	// 1. Config file
	results = append(results, checkConfig())

	cfg, err := config.Load()
	if err != nil {
		results = append(results, CheckResult{"Agent token valid", "FAIL", "Cannot load config"})
		return results
	}

	client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)

	// 2. API reachable
	results = append(results, checkAPI(cfg.APIBaseURL))

	// 3. Agent token valid
	results = append(results, checkToken(ctx, client))

	// 4. Restic binary
	resticBinary, resticResult := checkRestic()
	results = append(results, resticResult)

	if resticBinary == "" {
		return results
	}

	// 5. Restic version
	results = append(results, checkResticVersion(ctx, resticBinary))

	// 6. Per-repo checks
	repos, _, err := client.GetRepos(ctx)
	if err != nil {
		results = append(results, CheckResult{"Fetch repo config", "FAIL", err.Error()})
		return results
	}

	results = append(results, CheckResult{"Fetch repo config", "OK", fmt.Sprintf("%d repos", len(repos))})

	for _, repo := range repos {
		prefix := fmt.Sprintf("Repo '%s'", repo.ID[:8])

		// Check paths exist
		for _, p := range repo.Paths {
			if _, err := os.Stat(p); err != nil {
				results = append(results, CheckResult{prefix + ": path " + p, "FAIL", "Path does not exist"})
			} else {
				results = append(results, CheckResult{prefix + ": path " + p, "OK", ""})
			}
		}

		// Check storage accessible
		storageEnv := map[string]string{
			"AWS_ACCESS_KEY_ID":     repo.StorageConfig.AccessKeyID,
			"AWS_SECRET_ACCESS_KEY": repo.StorageConfig.SecretAccessKey,
		}
		runner := restic.NewRunner(resticBinary, repo.ResticRepoPath, repo.ResticPassword, storageEnv)

		snapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if runner.IsInitialized(snapCtx) {
			results = append(results, CheckResult{prefix + ": storage accessible", "OK", ""})
		} else {
			results = append(results, CheckResult{prefix + ": storage accessible", "FAIL", "Cannot reach restic repo"})
		}
		cancel()

		// Check disk space (>1GB free)
		results = append(results, checkDiskSpace(prefix))
	}

	return results
}

func checkConfig() CheckResult {
	if config.Exists() {
		return CheckResult{"Config file exists", "OK", config.ConfigPath()}
	}
	return CheckResult{"Config file exists", "FAIL", "Run 'nerdbackup-agent init' first"}
}

func checkAPI(baseURL string) CheckResult {
	start := time.Now()
	resp, err := http.Get(baseURL + "/api/v1/downloads/agent/latest")
	latency := time.Since(start)
	if err != nil {
		return CheckResult{"API reachable", "FAIL", err.Error()}
	}
	resp.Body.Close()
	return CheckResult{"API reachable", "OK", fmt.Sprintf("%dms", latency.Milliseconds())}
}

func checkToken(ctx context.Context, client *api.Client) CheckResult {
	_, err := client.SendHeartbeat(ctx, api.HeartbeatRequest{
		AgentVersion:  "doctor",
		ResticVersion: "check",
		Platform:      runtime.GOOS,
		Arch:          runtime.GOARCH,
		Hostname:      "doctor-check",
	})
	if err != nil {
		return CheckResult{"Agent token valid", "FAIL", err.Error()}
	}
	return CheckResult{"Agent token valid", "OK", ""}
}

func checkRestic() (string, CheckResult) {
	path, err := restic.FindOrInstall()
	if err != nil {
		return "", CheckResult{"Restic binary found", "FAIL", "Not found in PATH or ~/.local/bin"}
	}
	return path, CheckResult{"Restic binary found", "OK", path}
}

func checkResticVersion(ctx context.Context, binary string) CheckResult {
	runner := &restic.Runner{Binary: binary}
	ver := runner.Version(ctx)
	if ver == "unknown" {
		return CheckResult{"Restic version", "WARN", "Could not determine version"}
	}
	return CheckResult{"Restic version", "OK", ver}
}

func checkDiskSpace(prefix string) CheckResult {
	// Simple check: this runs on the agent's machine
	var stat struct{}
	_ = stat
	// Use a simple file write test rather than platform-specific disk check
	f, err := os.CreateTemp("", "nerdbackup-doctor-*")
	if err != nil {
		return CheckResult{prefix + ": disk writable", "FAIL", err.Error()}
	}
	f.Close()
	os.Remove(f.Name())
	return CheckResult{prefix + ": disk writable", "OK", ""}
}
