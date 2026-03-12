package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"agrisense/internal/config"
	"agrisense/internal/runner"
	"agrisense/internal/sync"
	"agrisense/internal/tui"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			fmt.Fprint(os.Stderr, "Press Enter to exit...")
			bufio.NewReader(os.Stdin).ReadString('\n')
			os.Exit(2)
		}
	}()

	// 1. Ensure we are in the repository root
	scriptDir, err := sync.EnsureRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	envPath := filepath.Join(scriptDir, ".env")

	// 2. Parse --profile flag
	profile := flag.String("profile", "docker", "Runner profile: docker | docker-dev")
	flag.Parse()
	args := flag.Args()

	// 3. Dispatch
	if len(args) == 0 {
		// TUI mode
		m := tui.NewModel(scriptDir, *profile)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// CLI mode
		runCLI(scriptDir, envPath, *profile, args)
	}
}


func runCLI(scriptDir, envPath, profile string, args []string) {
	envValues, _ := config.ReadEnvValues(envPath)
	cfg := runner.RunnerConfig{
		ScriptDir: scriptDir,
		EnvPath:   envPath,
		EnvValues: envValues,
	}
	r, err := runner.NewRunner(profile, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	cmd := args[0]

	switch cmd {
	case "start":
		stream, err := r.StartAll(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		streamToStdout(stream)
	case "stop":
		stream, err := r.StopAll(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		streamToStdout(stream)
	case "restart":
		stream, err := r.RestartAll(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		streamToStdout(stream)
	case "build":
		stream, err := r.BuildAll(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		streamToStdout(stream)
	case "destroy":
		mode := parseDestroyFlags(args[1:])
		stream, err := r.Destroy(ctx, mode)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		streamToStdout(stream)
	case "status":
		services, err := r.ListServices(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printStatusTable(services)
	case "help", "--help", "-h":
		printCLIHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		printCLIHelp()
		os.Exit(1)
	}
}

func streamToStdout(stream runner.CommandStream) {
	for line := range stream.Lines {
		fmt.Println(line)
	}
	code := <-stream.ExitCode
	os.Exit(code)
}

func printStatusTable(services []runner.ServiceStatus) {
	if len(services) == 0 {
		fmt.Println("No services found.")
		return
	}
	maxName := len("NAME")
	maxState := len("STATE")
	for _, s := range services {
		if len(s.Name) > maxName {
			maxName = len(s.Name)
		}
		if len(s.State) > maxState {
			maxState = len(s.State)
		}
	}
	fmt.Printf(" %-*s  %-*s  %s\n", maxName, "NAME", maxState, "STATE", "STATUS")
	fmt.Println(" " + strings.Repeat("─", maxName) + "  " + strings.Repeat("─", maxState) + "  " + strings.Repeat("─", 20))
	for _, s := range services {
		fmt.Printf(" %-*s  %-*s  %s\n", maxName, s.Name, maxState, s.State, s.Status)
	}
}

func parseDestroyFlags(args []string) runner.DestroyMode {
	var volumes, images, prune bool
	for _, a := range args {
		switch a {
		case "--volumes":
			volumes = true
		case "--images":
			images = true
		case "--prune":
			prune = true
		}
	}
	switch {
	case prune:
		return runner.DestroyPrune
	case images:
		return runner.DestroyContainersVolumesImages
	case volumes:
		return runner.DestroyContainersAndVolumes
	default:
		return runner.DestroyContainersOnly
	}
}

func printCLIHelp() {
	fmt.Print(`Usage: agrisense [--profile <name>] <command> [flags]

Profiles:
  --profile docker      (default) Docker Compose
  --profile docker-dev  Docker Compose with dev overlay (hot-reload)

Commands:
  start                 Start all services
  stop                  Stop all services
  restart               Restart all services
  build                 Rebuild and start all services
  destroy               Tear down services
    --volumes             Also remove volumes + Supabase bind-mounts
    --images              Also remove built images
    --prune               Full prune (volumes + images + build cache)
  status                Print service status table and exit
  help                  Show this message
`)
}
