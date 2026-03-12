package runner

import (
	"context"
	"fmt"
)

// ServiceStatus is the runner-agnostic representation of one managed service.
type ServiceStatus struct {
	Name    string // display name (container name or process name)
	Service string // compose service name or binary name
	State   string // "running" | "stopped" | "restarting" | "error" | "unknown"
	Status  string // human-readable detail, e.g. "Up 2 hours" or "Exited (1) 3m ago"
	Health  string // "healthy" | "unhealthy" | "" if no healthcheck
}

// CommandStream is returned by any operation that produces streaming output.
type CommandStream struct {
	Lines    <-chan string
	ExitCode <-chan int
}

type DestroyMode int

const (
	DestroyContainersOnly DestroyMode = iota
	DestroyContainersAndVolumes
	DestroyContainersVolumesImages
	DestroyPrune
)

// Runner is the single interface the TUI uses to manage services.
type Runner interface {
	Profile() string

	ListServices(ctx context.Context) ([]ServiceStatus, error)
	StreamLogs(ctx context.Context, name string, tail int) (<-chan string, error)

	StartAll(ctx context.Context) (CommandStream, error)
	StopAll(ctx context.Context) (CommandStream, error)
	RestartAll(ctx context.Context) (CommandStream, error)
	BuildAll(ctx context.Context) (CommandStream, error)
	Destroy(ctx context.Context, mode DestroyMode) (CommandStream, error)

	StartService(ctx context.Context, name string) (CommandStream, error)
	StopService(ctx context.Context, name string) (CommandStream, error)
	RestartService(ctx context.Context, name string) (CommandStream, error)
	BuildService(ctx context.Context, name string) (CommandStream, error)

	// ServiceURL returns the local URL for a named service, or "" if not applicable.
	ServiceURL(name string) string
}

// RunnerConfig holds the paths and env values needed by any runner implementation.
type RunnerConfig struct {
	ScriptDir string
	EnvPath   string
	EnvValues map[string]string
}

// NewRunner is the factory that resolves a profile string to a Runner implementation.
func NewRunner(profile string, cfg RunnerConfig) (Runner, error) {
	switch profile {
	case "docker":
		return NewDockerComposeRunner(cfg, false), nil
	case "docker-dev":
		return NewDockerComposeRunner(cfg, true), nil
	// case "native":
	//     return NewNativeRunner(cfg), nil
	default:
		return nil, fmt.Errorf("unknown profile %q — valid profiles: docker, docker-dev", profile)
	}
}
