package sync

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const RepoURL = "https://github.com/Agrisense-IoT/agrisense-infra"

// EnsureRepoRoot checks if the application is running within the agrisense-infra repository.
// If not, it attempts to clone the repository from GitHub.
func EnsureRepoRoot() (string, error) {
	// Determine the base directory (where the executable is)
	exe, err := os.Executable()
	if err != nil {
		return os.Getwd()
	}
	exeDir := filepath.Dir(exe)

	// 1. Check if we are already in the repo root (looking for docker-compose.yml)
	current := exeDir
	for {
		if _, err := os.Stat(filepath.Join(current, "docker-compose.yml")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// 2. Not in repo root. Ask to clone.
	fmt.Printf("Agrisense infrastructure files not detected.\n")
	fmt.Printf("The executable should be run from the root of the agrisense-infra repository.\n")
	fmt.Printf("Would you like to clone the repository from GitHub into the current directory? [Y/n]: ")

	var response string
	fmt.Scanln(&response)
	if response != "" && response != "y" && response != "Y" {
		return "", fmt.Errorf("repository sync cancelled by user")
	}

	fmt.Printf("\nInitializing and fetching repository from %s...\n", RepoURL)
	fmt.Println("Note: If prompted, please log in via your browser to access the private repository.")

	// Check if git is available
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git command not found. Please install Git: https://git-scm.com/")
	}

	// Check for .exclude-on-pull
	excludeFile := filepath.Join(exeDir, ".exclude-on-pull")
	var excludePatterns []string
	if data, err := os.ReadFile(excludeFile); err == nil {
		lines := readFileLines(data)
		for _, line := range lines {
			if line != "" {
				excludePatterns = append(excludePatterns, "!"+line)
			}
		}
	}

	// Commands to initialize, fetch and checkout in the current directory
	commands := [][]string{
		{"git", "init"},
		{"git", "remote", "add", "origin", RepoURL},
	}

	// If we have exclusions, use sparse-checkout
	if len(excludePatterns) > 0 {
		commands = append(commands, []string{"git", "sparse-checkout", "init", "--no-cone"})
		// Include everything except the excluded patterns
		sparsePatterns := append([]string{"/*"}, excludePatterns...)
		setCmd := append([]string{"sparse-checkout", "set", "--no-cone"}, sparsePatterns...)
		commands = append(commands, append([]string{"git"}, setCmd...))
	}

	commands = append(commands,
		[]string{"git", "fetch", "origin"},
		[]string{"git", "checkout", "-f", "main"},
	)

	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = exeDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to execute %v: %w", args, err)
		}
	}

	fmt.Printf("\nRepository files synchronized successfully to: %s\n", exeDir)
	return exeDir, nil
}

func readFileLines(data []byte) []string {
	var lines []string
	var current []byte
	for _, b := range data {
		if b == '\n' {
			lines = append(lines, string(trimSpaceFromBytes(current)))
			current = nil
		} else {
			current = append(current, b)
		}
	}
	if len(current) > 0 {
		lines = append(lines, string(trimSpaceFromBytes(current)))
	}
	return lines
}

func trimSpaceFromBytes(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}
