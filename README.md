# Agrisense Infrastructure

Docker Compose orchestration for the full Agrisense IoT platform. Runs all services from a single `.env` file.

## Architecture

```text
                    +------------------+
                    |    Frontend      |
                    |  (Next.js :3000) |
                    +--------+---------+
                             |
                    +--------v---------+
                    |    Backend API   |
                    | (NestJS :3143)   |
                    +--+----------+----+
                       |          |
              +--------v--+  +---v---------+
              | PostgreSQL |  |  Mosquitto  |
              | + PostGIS  |  | MQTT :1883  |
              | (Supabase) |  +-------------+
              +------------+
                    |
              +-----v------+
              |  Supabase   |
              |  Studio     |
              |  (:8000)    |
              +-------------+
```

## Services

| Service         | Port  | Description                              |
|-----------------|-------|------------------------------------------|
| Frontend        | 3000  | Next.js web dashboard                    |
| API             | 3143  | NestJS REST API + Swagger                |
| Supabase Studio | 8000  | Database management UI                   |
| MQTT            | 1883  | Mosquitto broker (devices + WebSocket)   |
| PostgreSQL      | -     | Internal only (via Supavisor pooler)     |

## Quick Start

### Prerequisites

- Docker & Docker Compose v2
- Clone all three repos as siblings:

```bash
mkdir agrisense && cd agrisense
git clone git@github.com:Agrisense-IoT/agrisense-infra.git
git clone git@github.com:Agrisense-IoT/agrisense-backend.git
git clone git@github.com:Agrisense-IoT/agrisense-frontend.git
```

### Setup

```bash
cd agrisense-infra

# Create and configure environment
cp .env.example .env
# Edit .env — follow the instructions to generate secrets

# Start the full stack
chmod +x start.sh
./start.sh --build
```

### Usage

```bash
./start.sh                # Start all services
./start.sh --build        # Rebuild images and start
./start.sh --no-frontend  # Start without frontend
./start.sh --down         # Stop all services
```

## Environment

All services share a single `.env` file. Key sections:

- **Secrets** - JWT, encryption keys, API tokens (generate before first run)
- **Database** - PostgreSQL connection settings
- **Pooler** - Supavisor connection pooler config
- **API** - NestJS port, seeding toggle
- **MQTT** - Broker host/port/auth
- **Auth** - GoTrue settings (email, phone, SMTP)
- **Storage** - S3/MinIO settings

See `.env.example` for all available options with documentation.

## Troubleshooting

**Services won't start**: Ensure Docker is running and all three repos are cloned as siblings.

**Database connection errors**: Wait for the `db` service to be healthy before the API starts (handled automatically by `depends_on`).

**MQTT not receiving messages**: Check that devices publish to `agrisense/devices/<MAC>/readings` with valid JSON payloads.

## License

UNLICENSED
