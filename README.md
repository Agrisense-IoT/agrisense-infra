# 🚜 Agrisense Infra

![Docker](https://img.shields.io/badge/docker-%230db7ed.svg?style=for-the-badge&logo=docker&logoColor=white)
![PowerShell](https://img.shields.io/badge/PowerShell-%235391FE.svg?style=for-the-badge&logo=powershell&logoColor=white)

Single-repo orchestrator for the **full Agrisense Docker stack**.
Its only job is to run three service groups seamlessly together:

| Service Group            | Description                              |
| ------------------------ | ---------------------------------------- |
| 🟢 **supabase**          | PostgreSQL, Auth, REST, Realtime, Studio |
| 🔵 **agrisense-backend** | NestJS API *(port 3143)*                 |
| 🟠 **agrisense-frontend**| Next.js dashboard *(port 3000)*          |

All configuration lives in **one `.env` file** at the root of this directory. No service defines its own environmental values independently.

---

## 📂 Directory Layout

```text
<parent>/
├── agrisense-infra/          ← this repo (clone here first)
│   ├── docker-compose.yml    ← root compose — orchestrates everything
│   ├── agrisense.ps1         ← TUI dashboard (use this)
│   ├── config.ps1            ← interactive .env setup wizard
│   ├── .env                  ← your config (created by config.ps1)
│   ├── .env.example          ← template with all variables documented
│   └── supabase/
│       └── docker-compose.yml  ← Supabase service definitions (do not edit)
│
├── agrisense-backend/        ← auto-cloned if missing, or mount your local copy
└── agrisense-frontend/       ← auto-cloned if missing, or mount your local copy
```

Both `agrisense-backend` and `agrisense-frontend` must be **siblings** of
`agrisense-infra` (i.e. children of the same parent directory).
`agrisense.ps1` clones them automatically if they are not present.

---

## 🏁 Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (v4.20+, Compose v2.20+)
- [Git](https://git-scm.com/) (required for auto-clone)
- PowerShell 5.1+ (included in Windows 10/11)

---

## 🚀 First-Time Setup

### 1. Clone this repo

```powershell
git clone https://github.com/Agrisense-IoT/agrisense-infra.git
cd agrisense-infra
```

### 2. Create and configure `.env`

Run the interactive wizard (recommended):

```powershell
./config.ps1
```

The wizard will:

- Prompt for dashboard credentials, public URLs, and database password
- Auto-generate all cryptographic secrets (JWT keys, Supabase tokens, etc.)
- Check for port conflicts and offer alternatives
- Write a complete `.env` file

Or manually copy the template and edit it:

```powershell
Copy-Item .env.example .env
# Open .env in your editor and fill in every <placeholder> value
```

### 3. Launch the TUI

```powershell
./agrisense.ps1
```

On first launch, if containers are not already running, the script will
automatically invoke `config.ps1` for setup and then start the stack.

---

## 🕹️ Usage

`agrisense.ps1` opens a live terminal dashboard. All actions are performed via
keyboard shortcuts shown in the bottom action bar:

| Key     | Action                                          |
| ------- | ----------------------------------------------- |
| `↑` `↓` | Navigate container list                         |
| `Enter` | View scrollable logs for the selected container |
| `Esc`   | Return to the dashboard from the log viewer     |
| `S`     | Start all containers                            |
| `X`     | Stop all containers                             |
| `R`     | Restart all containers                          |
| `B`     | Re-build and start all containers               |
| `D`     | Stop & destroy (with confirmation prompt)       |
| `E`     | Open the environment variable editor            |
| `Q`     | Quit the TUI                                    |

---

## 🔌 Services & Ports

| Service             | Default port | Description                        |
|---------------------|--------------|------------------------------------|
| Frontend            | 3000         | Next.js web dashboard              |
| Backend API         | 3143         | NestJS REST API + Swagger          |
| Supabase Studio     | 8000         | Database management UI             |
| PostgreSQL (pooled) | 5432         | Via Supavisor (external access)    |
| Supavisor (txn)     | 6543         | Transaction-mode connection pooler |

All ports are configurable in `.env`.

---

## ⚙️ Environment Variables

All variables are documented in [`.env.example`](.env.example).
Key sections:

| Section            | Key variables                                        |
|--------------------|------------------------------------------------------|
| **Secrets**        | `JWT_SECRET`, `ANON_KEY`, `SERVICE_ROLE_KEY`, ...    |
| **Database**       | `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_PORT`  |
| **Repositories**   | `BACKEND_REPO`, `FRONTEND_REPO`                      |
| **Ports**          | `PORT`, `FRONTEND_PORT`, `KONG_HTTP_PORT`            |
| **Frontend build** | `NEXT_PUBLIC_API_URL`                                |
| **Auth**           | `SITE_URL`, `JWT_EXPIRY`, SMTP settings              |
| **Storage**        | `GLOBAL_S3_BUCKET`, `REGION`, MinIO credentials      |

`NEXT_PUBLIC_API_URL` is baked into the frontend Docker image at build time.
If you change the backend port, you must rebuild the frontend image (`-Build`).

---

## 🛠️ Development Mode (Hot Reload)

Development mode mounts your local `agrisense-backend` and `agrisense-frontend`
source directories into the running containers so that code changes take effect
immediately — no image rebuild required.

| Service  | Reload mechanism                                           |
| -------- | ---------------------------------------------------------- |
| Backend  | NestJS watch mode (`npm run start:dev`) via `@nestjs/cli`  |
| Frontend | Next.js dev server (`npm run dev`) with Fast Refresh       |

Supabase services are unaffected.

### Starting in development mode

**Via `agrisense.ps1` (recommended):**

```powershell
# Interactive TUI with hot reload
./agrisense.ps1 -Development

# Auto-start the stack in dev mode, then open the TUI
./agrisense.ps1 -Development -StartProject

# Non-interactive CLI actions in dev mode
./agrisense.ps1 -Development -Start
./agrisense.ps1 -Development -Restart
./agrisense.ps1 -Development -Build
```

**Via Docker Compose directly:**

```powershell
# Start in development mode
docker compose -f docker-compose.yml -f docker-compose.dev.yml up

# Start in production mode (default)
docker compose up -d
```

### Production mode (default)

```powershell
# Interactive TUI — production images, no volume mounts
./agrisense.ps1

# Or directly
docker compose up -d
```

> **Note:** `NEXT_PUBLIC_API_URL` is baked into the frontend image at build time
> in production mode. In development mode the Next.js dev server reads it from
> the environment at runtime, so no rebuild is needed when this value changes.

---

## 💡 Troubleshooting

**`.env` not found**
Run `./agrisense.ps1` (it invokes `config.ps1` automatically), or `Copy-Item .env.example .env` and fill in placeholders.

**Port already in use**
The dashboard will show containers in an error state. Check ports in `.env` or use the `E` shortcut to edit them.
Either stop that process or change the port in `.env` (then re-run).

**Partial clone / missing Dockerfile**
`agrisense.ps1` detects this and automatically re-clones the affected repository.

**Backend fails to start (database connection)**
The backend depends on `supavisor` being healthy. Wait for the Supabase stack
to finish initializing (can take 30–60 s on first boot). The health check retries
automatically.

**Frontend build fails (`NEXT_PUBLIC_API_URL`)**
This variable is required at image build time. Ensure it is set in `.env` before
pressing `B` (Re-Build) in the dashboard.

**Supabase Studio not loading**
Studio depends on the `analytics` service. Check `docker compose logs analytics`
if Studio shows a blank page or 502.

**Execution policy error (PowerShell)**
Run once as Administrator:

```powershell
Set-ExecutionPolicy RemoteSigned -Scope CurrentUser
```

---

## 📄 License

Distributed under the **GNU General Public License v3.0 (GPL-3.0)**. See `LICENSE` for more information.
