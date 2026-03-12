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
	fmt.Printf("Would you like to clone the repository from GitHub? [Y/n]: ")

	var response string
	fmt.Scanln(&response)
	if response != "" && response != "y" && response != "Y" {
		return "", fmt.Errorf("repository sync cancelled by user")
	}

	fmt.Printf("\nCloning repository from %s...\n", RepoURL)
	fmt.Println("Note: If prompted, please log in via your browser to access the private repository.")

	// Check if git is available
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git command not found. Please install Git: https://git-scm.com/")
	}

	// Clone into a subdirectory
	repoDir := filepath.Join(exeDir, "agrisense-infra")
	
	// If the directory already exists (maybe an incomplete clone?), we might need to handle it.
	// For now, let git handle it (it will error if not empty).
	
	cmd := exec.Command("git", "clone", RepoURL, repoDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	fmt.Printf("\nRepository cloned successfully to: %s\n", repoDir)
	return repoDir, nil
}
