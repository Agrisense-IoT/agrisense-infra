# 🚜 AgriSense Infra - Changelog

All notable changes to the Agrisense single-repo orchestrator and TUI will be documented in this file.

## [1.3.1] - 2026-03-12

### ✨ Features

- **Exclusion Support:** Added `.exclude-on-pull` support using Git sparse-checkout to exclude specific files and folders during repository synchronization.

## [1.3.0] - 2026-03-12

### ✨ Features

- **Repository Synchronization:** Added automatic repository detection and cloning logic with GitHub authentication (Device Code Flow).

## [1.2.0] - 2026-03-12

### 🏗️ TUI Infrastructure

- **Go Migration:** Fully migrated the infrastructure TUI from PowerShell (`agrisense.ps1`) to a high-performance Go-based binary (`agrisense-tui.exe`) using the Bubble Tea framework.
- **Improved Monitoring:** Enhanced log tailing and container health management.
- **Secrets Management:** Integrated robust JWT generation for Supabase synchronization.

## [1.1.2] - 2026-03-08

### ⚙️ Sync & Build

- Version synchronization for OTA and Telemetry maintenance update.

## [1.1.1] - 2026-03-08

### ⚙️ Sync & Build

- Synchronized versions across frontend, backend, and infra to 1.1.1.
- Updated documentation and orchestrated deployment scripts.

## [1.1.0] - 2026-03-08

### 🚀 Performance & Optimizations

- **TUI Rendering Strategy:** Migrated `agrisense.ps1` from synchronous full-screen `Write-Host` calls to a double-buffered ANSI escape code memory buffer.
- **Dirty-Flag System:** Applied logical `ScreenDirty` flags to prevent redundant rendering when idle or when state has not changed.
- **Adaptive Frame Rate:** Adjusted idle sleep to 50ms and active input sleep to 16ms to provide a 60fps responsive feel.
- **Environment Editor Engine:** Fixed an $O(n^2)$ lookup latency by pre-compiling an index hashtable.

### ✨ Features

- **Categorized Dashboard Editor:** Organized `.env` variables via headers (`## <Category>`) inside `.env.example` for a clean segmented layout.
- **Custom Environmental Labels:** Added support for mapping user-friendly names to internal `.env` variable keys.

### 🐛 Bug Fixes

- **Character Encoding Crashes:** Resolved PowerShell 5.1 crashing issues during character multiplication routines for TUI dialog borders and separators.
- **Unicode Support:** Fixed question marks appearing instead of emoji characters by forcing `[Console]::OutputEncoding` to UTF-8 on Launch.
- **Variable Parsing Issues:** Cleaned up `.env` extraction and fixed credential propagation errors inside `config.ps1`.
- **Navigation Bounds:** Implemented cyclic wrap-around navigation in both the root dashboard and the `.env` editor.
- **Layout Padding:** Fixed string padding logic offset when using double-width emoji icons (e.g. ⚡).
- Corrected license information (GPL-3.0)

## [1.0.0] - 2026-03-07

### 🌱 Initial Release

- **Docker Compose Orchestration:** Merged all backend, frontend, and Supabase service groups under a single root `docker-compose.yml`.
- **PowerShell TUI (`agrisense.ps1`):** Launched a native windows terminal interface to manage and inspect containers.
- **Configuration Wizard (`config.ps1`):** Created an interactive guided setup for credential generation and `.env` initialization.
