# Getting Started

This tutorial walks you through setting up Asiakirjat for the first time.

## Prerequisites

- Go 1.21 or later (for building from source)
- A Linux, macOS, or Windows system

## Installation

### Option 1: Download Binary

Download the latest release from the releases page and extract it.

### Option 2: Build from Source

```bash
git clone https://github.com/qwc/asiakirjat.git
cd asiakirjat
CGO_ENABLED=0 go build -mod=vendor -ldflags="-s -w" -o asiakirjat .
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

3. Create the data directory:

```bash
mkdir -p data/docs
```

## Running

Start Asiakirjat:

```bash
./asiakirjat -config config.yaml
```

You should see:

```
level=INFO msg="starting server" address=0.0.0.0:8080
```

## First Login

1. Open your browser to `http://localhost:8080`
2. Click "Login"
3. Enter the initial admin credentials from your config
4. You're now logged in as admin

## What's Next?

- [Create Your First Project](first-project.md) - Set up a documentation project
- [Upload Documentation](uploading-docs.md) - Upload your first docs
- [Configuration Reference](../reference/configuration.md) - Explore all options
