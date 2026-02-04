# Getting Started

This tutorial walks you through setting up Asiakirjat for the first time.

## Prerequisites

- **Binary / Build from Source:** Go 1.21 or later, a Linux, macOS, or Windows system
- **Docker:** Docker and optionally Docker Compose

## Installation

### Option 1: Download Binary

Download the latest release from the releases page and extract it.

### Option 2: Build from Source

```bash
git clone https://github.com/qwc/asiakirjat.git
cd asiakirjat
CGO_ENABLED=0 go build -mod=vendor -ldflags="-s -w" -o asiakirjat .
```

### Option 3: Docker

```bash
git clone https://github.com/qwc/asiakirjat.git
cd asiakirjat
docker build -t asiakirjat .
```

## Configuration

1. Copy the example configuration:

```bash
cp config.yaml.example config.yaml
```

2. Edit `config.yaml` with your settings. The most important settings are:

```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  driver: sqlite
  dsn: data/asiakirjat.db

storage:
  base_path: data/docs

auth:
  initial_admin:
    username: admin
    password: changeme
```

3. If running without Docker, create the data directory:

```bash
mkdir -p data/docs
```

## Running

### Binary / Build from Source

```bash
./asiakirjat -config config.yaml
```

### Docker

```bash
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v asiakirjat-data:/app/data \
  asiakirjat
```

### Docker Compose

The repository includes a `docker-compose.yml` you can use directly or adapt. Here is a full example:

```yaml
services:
  asiakirjat:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - data:/app/data
    restart: unless-stopped

volumes:
  data:
```

This configuration:

- **Builds** the image from the Dockerfile in the repository
- **Exposes** port 8080 on the host
- **Mounts** your `config.yaml` as read-only into the container
- **Persists** the database and uploaded documentation in a named `data` volume, so data survives container restarts and upgrades
- **Restarts** automatically unless explicitly stopped

If you already have a pre-built image (e.g. from a registry), replace `build: .` with `image: asiakirjat:latest`.

Start the service:

```bash
docker compose up -d
```

Check the logs to verify it started correctly:

```bash
docker compose logs -f
```

You should see:

```
level=INFO msg="starting server" address=0.0.0.0:8080
```

To stop the service:

```bash
docker compose down
```

Data in the named volume is preserved across `down`/`up` cycles. To remove the volume as well, use `docker compose down -v`.

## First Login

1. Open your browser to `http://localhost:8080`
2. Click "Login"
3. Enter the initial admin credentials from your config
4. You're now logged in as admin

## What's Next?

- [Create Your First Project](first-project.md) - Set up a documentation project
- [Upload Documentation](uploading-docs.md) - Upload your first docs
- [Configuration Reference](../reference/configuration.md) - Explore all options
