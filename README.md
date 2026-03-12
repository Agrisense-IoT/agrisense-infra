# 🚜 AgriSense Infra

> ⚠️ **CONFIDENTIAL**: This repository contains proprietary and confidential information. Unauthorized access or distribution is strictly prohibited.

[![AgriSense](https://img.shields.io/badge/AgriSense-IoT-green.svg)](https://github.com/Agrisense-IoT)
[![License: Proprietary](https://img.shields.io/badge/License-Proprietary-red.svg)](LICENSE.md)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8.svg?logo=go&logoColor=white)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Supported-2496ED.svg?logo=docker&logoColor=white)](https://www.docker.com/)

AgriSense Infra is the central orchestration and management hub for the AgriSense IoT ecosystem. It provides a high-performance terminal interface (TUI) and CLI tool to manage backend services, frontend applications, and infrastructure components like Supabase and MQTT brokers.

## ✨ Features

- **🚀 Integrated Orchestration:** Manage Backend, Frontend, Supabase, and Mosquitto through a single interface.
- **🖥️ High-Performance TUI:** A responsive Go-based terminal interface built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).
- **🛠️ Powerful CLI:** Scriptable command-line interface for service management.
- **🔄 Repository Sync:** Automatic synchronization with `agrisense-backend` and `agrisense-frontend` repositories.
- **🔐 Secrets Management:** Automated JWT and secret generation via the configuration wizard.
- **📉 Real-time Monitoring:** Live log streaming and container health tracking.

## 🏗️ Architecture

The AgriSense stack consists of:

- **Frontend:** Next.js application.
- **Backend:** NestJS API with Prisma ORM.
- **Database:** Supabase (PostgreSQL, Auth, Storage).
- **Messaging:** Mosquitto MQTT broker for IoT telemetry.

## 🚦 Getting Started

### Prerequisites

- [Docker](https://www.docker.com/get-started) & [Docker Compose](https://docs.docker.com/compose/install/)
- [Go 1.21+](https://go.dev/doc/install) (only if building from source)
- Git

### Installation

1. **Clone the Infrastructure Repository:**

   ```bash
   git clone https://github.com/Agrisense-IoT/agrisense-infra.git
   cd agrisense-infra
   ```

2. **Initialize Environment Configuration:**
   Copy the example environment file and customize it.

   ```bash
   cp .env.example .env
   ```

3. **Configure Secrets:**
   Open `.env` and fill in the required secrets (JWT secrets, database passwords, etc.). You can use the TUI to help manage these.

### 🎮 Usage

AgriSense Infra provides two modes of operation: **Interactive TUI** and **CLI**.

#### Interactive TUI

Launch the full management dashboard:

```bash
./agrisense
```

_Note: If running for the first time on Windows, the binary might be named `agrisense.exe`._

#### CLI Mode

Run specific commands directly:

```bash
./agrisense [command] [flags]
```

**Common Commands:**

- `start`: Start all services.
- `stop`: Stop all services.
- `restart`: Restart all services.
- `build`: Rebuild and start all services.
- `status`: Show service health table.
- `destroy`: Tear down infrastructure (optional: `--volumes`, `--images`, `--prune`).

**Profiles:**

- `--profile docker`: Standard production-like environment (default).
- `--profile docker-dev`: Development environment with hot-reload enabled.

## 📂 Repository Structure

- `/tui`: Source code for the AgriSense Go management tool.
- `/supabase`: Docker and configuration files for the self-hosted Supabase stack.
- `/mosquitto`: MQTT broker configuration.
- `docker-compose.yml`: Root orchestration file.
- `CHANGELOG.md`: Detailed history of project changes.

## 📄 License

This project is **proprietary and confidential**. All rights are reserved by **Agrisense IoT**. See the [LICENSE.md](LICENSE.md) file for more information.

---

_Built with ❤️ by Stefan Cucoranu_
