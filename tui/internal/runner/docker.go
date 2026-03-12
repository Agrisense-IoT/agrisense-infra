package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DockerComposeRunner implements Runner using docker compose.
type DockerComposeRunner struct {
	cfg     RunnerConfig
	devMode bool
}

// NewDockerComposeRunner constructs a DockerComposeRunner.
func NewDockerComposeRunner(cfg RunnerConfig, devMode bool) *DockerComposeRunner {
	return &DockerComposeRunner{cfg: cfg, devMode: devMode}
}

func (r *DockerComposeRunner) Profile() string {
	if r.devMode {
		return "docker-dev"
	}
	return "docker"
}

func (r *DockerComposeRunner) baseArgs() []string {
	args := []string{"--env-file", r.cfg.EnvPath, "--project-directory", r.cfg.ScriptDir}
	if r.devMode {
		args = append(args,
			"-f", filepath.Join(r.cfg.ScriptDir, "docker-compose.yml"),
			"-f", filepath.Join(r.cfg.ScriptDir, "docker-compose.dev.yml"),
		)
	}
	return args
}

// psEntry covers both modern (per-line JSON object) and legacy (JSON array) formats.
type psEntry struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	Health  string `json:"Health"`
}

func mapState(raw string) string {
	switch strings.ToLower(raw) {
	case "running":
		return "running"
	case "restarting":
		return "restarting"
	case "exited", "stopped":
		return "stopped"
	case "created":
		return "unknown"
	default:
		if raw == "" {
			return "unknown"
		}
		return "error"
	}
}

func (r *DockerComposeRunner) ListServices(ctx context.Context) ([]ServiceStatus, error) {
	args := append([]string{"compose"}, r.baseArgs()...)
	args = append(args, "ps", "-a", "--format", "json")

	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose ps: %w", err)
	}

	trimmed := strings.TrimSpace(string(out))
	var entries []psEntry

	// Try JSON array first (legacy), then newline-delimited objects (modern).
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, fmt.Errorf("parse ps output: %w", err)
		}
	} else {
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var e psEntry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				continue // skip malformed lines
			}
			entries = append(entries, e)
		}
	}

	statuses := make([]ServiceStatus, 0, len(entries))
	for _, e := range entries {
		statuses = append(statuses, ServiceStatus{
			Name:    e.Name,
			Service: e.Service,
			State:   mapState(e.State),
			Status:  e.Status,
			Health:  e.Health,
		})
	}
	return statuses, nil
}

func (r *DockerComposeRunner) StreamLogs(ctx context.Context, name string, tail int) (<-chan string, error) {
	// Determine if the service is running to decide whether to follow.
	svcs, _ := r.ListServices(ctx)
	follow := false
	for _, s := range svcs {
		if s.Name == name || s.Service == name {
			if s.State == "running" {
				follow = true
			}
			break
		}
	}

	tailStr := fmt.Sprintf("%d", tail)
	var cmd *exec.Cmd
	if follow {
		cmd = exec.CommandContext(ctx, "docker", "logs", "--tail", tailStr, "-f", name)
	} else {
		cmd = exec.CommandContext(ctx, "docker", "logs", "--tail", tailStr, name)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // merge stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	ch := make(chan string, 256)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case ch <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		_ = cmd.Wait()
	}()
	return ch, nil
}

// streamCommand runs a docker compose subcommand and returns a CommandStream.
func (r *DockerComposeRunner) streamCommand(ctx context.Context, extraArgs ...string) (CommandStream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	args := append([]string{"compose"}, r.baseArgs()...)
	args = append(args, extraArgs...)

	cmd := exec.CommandContext(ctx, "docker", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return CommandStream{}, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return CommandStream{}, err
	}

	lines := make(chan string, 256)
	exitCode := make(chan int, 1)

	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer close(exitCode)
		err := cmd.Wait()
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				exitCode <- e.ExitCode()
				return
			}
			exitCode <- 1
			return
		}
		exitCode <- 0
	}()

	return CommandStream{Lines: lines, ExitCode: exitCode}, nil
}

func (r *DockerComposeRunner) StartAll(ctx context.Context) (CommandStream, error) {
	// Ensure Supabase volume files are present before bringing services up.
	// Bootstrap messages are forwarded through the stream so the TUI can show them.
	lines := make(chan string, 256)
	exitCode := make(chan int, 1)

	go func() {
		defer close(lines)
		defer close(exitCode)

		if err := ensureSupabaseVolumes(r.cfg.ScriptDir, lines); err != nil {
			lines <- "[bootstrap] ERROR: " + err.Error()
			exitCode <- 1
			return
		}

		stream, err := r.streamCommand(ctx, "up", "-d")
		if err != nil {
			lines <- "[docker] ERROR: " + err.Error()
			exitCode <- 1
			return
		}
		for l := range stream.Lines {
			lines <- l
		}
		exitCode <- <-stream.ExitCode
	}()

	return CommandStream{Lines: lines, ExitCode: exitCode}, nil
}

func (r *DockerComposeRunner) StopAll(ctx context.Context) (CommandStream, error) {
	return r.streamCommand(ctx, "stop")
}

func (r *DockerComposeRunner) RestartAll(ctx context.Context) (CommandStream, error) {
	return r.streamCommand(ctx, "restart")
}

func (r *DockerComposeRunner) BuildAll(ctx context.Context) (CommandStream, error) {
	return r.streamCommand(ctx, "build")
}

func (r *DockerComposeRunner) Destroy(ctx context.Context, mode DestroyMode) (CommandStream, error) {
	var args []string
	switch mode {
	case DestroyContainersOnly:
		args = []string{"down", "--remove-orphans"}
	case DestroyContainersAndVolumes:
		args = []string{"down", "--remove-orphans", "-v"}
	case DestroyContainersVolumesImages:
		args = []string{"down", "--remove-orphans", "-v", "--rmi", "all"}
	case DestroyPrune:
		args = []string{"down", "--remove-orphans", "-v", "--rmi", "all"}
	}

	stream, err := r.streamCommand(ctx, args...)
	if err != nil {
		return stream, err
	}

	// For modes that remove volumes, clean bind-mount contents after the command finishes.
	if mode == DestroyContainersAndVolumes || mode == DestroyContainersVolumesImages || mode == DestroyPrune {
		go func() {
			// Wait for the compose command to finish before cleaning bind-mounts.
			code := <-stream.ExitCode
			_ = code
			r.cleanBindMounts()
			if mode == DestroyPrune {
				pruneCmd := exec.Command("docker", "builder", "prune", "--all", "--force")
				_ = pruneCmd.Run()
			}
		}()
		// Reconstruct stream with a new exitCode channel that fires after cleanup.
		// Since we consumed the original ExitCode, wrap it.
		lines2 := make(chan string, 256)
		exit2 := make(chan int, 1)
		go func() {
			for l := range stream.Lines {
				lines2 <- l
			}
			close(lines2)
		}()
		go func() {
			// The goroutine above already consumed exit; signal done.
			exit2 <- 0
			close(exit2)
		}()
		return CommandStream{Lines: lines2, ExitCode: exit2}, nil
	}

	return stream, nil
}

func (r *DockerComposeRunner) cleanBindMounts() {
	dirs := []string{
		filepath.Join(r.cfg.ScriptDir, "supabase", "volumes", "db", "data"),
		filepath.Join(r.cfg.ScriptDir, "supabase", "volumes", "storage"),
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

func (r *DockerComposeRunner) StartService(ctx context.Context, name string) (CommandStream, error) {
	return r.streamCommand(ctx, "up", "-d", name)
}

func (r *DockerComposeRunner) StopService(ctx context.Context, name string) (CommandStream, error) {
	return r.streamCommand(ctx, "stop", name)
}

func (r *DockerComposeRunner) RestartService(ctx context.Context, name string) (CommandStream, error) {
	return r.streamCommand(ctx, "restart", name)
}

func (r *DockerComposeRunner) BuildService(ctx context.Context, name string) (CommandStream, error) {
	return r.streamCommand(ctx, "build", name)
}

func (r *DockerComposeRunner) ServiceURL(name string) string {
	env := r.cfg.EnvValues

	getEnv := func(key, def string) string {
		if v, ok := env[key]; ok && v != "" {
			return v
		}
		return def
	}

	switch name {
	case "agrisense-backend":
		port := getEnv("PORT", "3143")
		return "http://localhost:" + port
	case "agrisense-frontend":
		port := getEnv("FRONTEND_PORT", "3000")
		return "http://localhost:" + port
	case "studio":
		port := getEnv("STUDIO_PORT", "3000")
		return "http://localhost:" + port
	default:
		return ""
	}
}

// BuildTestingURL constructs the testing page URL from env values.
// Format: http://localhost:{PORT}/testing?host=127.0.0.1&port=9001[&user=...][&pass=...]
func BuildTestingURL(envValues map[string]string) string {
	get := func(key, def string) string {
		if v, ok := envValues[key]; ok && v != "" {
			return v
		}
		return def
	}
	port := get("PORT", "3143")
	url := "http://localhost:" + port + "/testing?host=127.0.0.1&port=9001"
	if u := envValues["MQTT_USERNAME"]; u != "" {
		url += "&user=" + u
	}
	if p := envValues["MQTT_PASSWORD"]; p != "" {
		url += "&pass=" + p
	}
	return url
}
