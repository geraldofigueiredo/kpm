# kportmaster (kpm)

Terminal UI tool for managing Kubernetes port-forwards across GCP/GKE clusters.

## Overview

`kpm` is a Go TUI application that provides a wizard-based flow for discovering GKE clusters, searching services with fuzzy matching, and managing multiple simultaneous port-forwards with automatic conflict resolution.

## Architecture

Three layers:

### 1. Infrastructure/GCP Layer
- Authenticates using local `gcloud` credentials
- Lists clusters in projects configured in `~/.kpm/config.yaml`
- Uses the full GKE cluster name as the display name
- Library: `google.golang.org/api/container/v1`
- Supports multiple kubeconfigs (one per cluster/context) to isolate dev and prod environments

### 2. Kubernetes Layer
- Uses `k8s.io/client-go` for native port-forward streams (same as kubectl internals)
- Forwards target `Service` resources (not pods directly) — leverages Service load balancing
- Default namespace: `default`; user can select a different namespace in the wizard
- Goroutine per active tunnel with `context.CancelFunc` for lifecycle control
- No subprocess spawning — direct API connections
- **Reconnect policy**: on tunnel failure, retry up to N times (configurable, default: 3)
  - If all retries fail, mark tunnel as `error` in the active list
  - Each retry uses exponential backoff

### 3. TUI Layer (Bubble Tea / The Elm Architecture)
- Library: `charmbracelet/bubbletea` + `charmbracelet/bubbles`
- All state mutations via `Msg` dispatch — no shared mutable state

#### Panels
- **Wizard Panel** (main flow):
  1. Choose GCP Project (from list configured in `config.yaml`)
  2. Choose Cluster (list from GKE API)
  3. Choose Namespace (default pre-selected, others listed)
  4. Fuzzy search Services — **multi-select** to forward multiple at once
  5. Confirm selection and start all forwards
- **Active Forwards Panel**: list of tunnels with status indicators
  - `auth-service  8080 -> 8080  [running]`
  - `payment-svc   8081 -> 8080  [error: max retries reached]`
  - `order-svc     8082 -> 8080  [reconnecting 2/3]`
- **Log/Alerts Panel**: port conflict resolutions, connection events, errors

#### Port Conflict Resolver
- Before starting, attempt `bind` on `localhost:<service_port>`
- If taken, scan upward (port+1, port+2, ...) for first available
- If user pre-defined a local port in config, honor it; otherwise auto-resolve
- Emit conflict notice to Alerts Panel: `auth-service: port 8080 in use, using 8081`

#### Recent Forwards (Session History)
- On exit, save the current forward list to `~/.kpm/history.json`
- On next launch, offer "Resume last session" shortcut
- Allows quick re-establishment if terminal dies unexpectedly

### Port Mapping Rules
1. Use the port declared in the `Service` spec as the remote port
2. Attempt to use the same port locally
3. On conflict, auto-assign next available local port
4. Allow manual override per service (inline edit in TUI or via `config.yaml`)

## Tech Stack

| Concern | Library |
|---|---|
| TUI framework | `charmbracelet/bubbletea` |
| TUI components | `charmbracelet/bubbles` |
| GCP/GKE API | `google.golang.org/api/container/v1` |
| Kubernetes client | `k8s.io/client-go` |
| Config parsing | `gopkg.in/yaml.v3` |
| Language | Go >= 1.25 |
| Binary | Single self-contained binary, named `kpm` |

## Configuration

File: `~/.kpm/config.yaml`

```yaml
defaults:
  namespace: default
  reconnect_retries: 3
  reconnect_backoff_seconds: 2

# Optional: regex with one capture group to extract a short display name from the
# full GKE cluster name. If omitted, the full cluster name is shown.
# Example: "my-cluster-(.+)-01"
cluster_env_regex: ""

projects:
  - id: my-gcp-project-prod
    label: Production
  - id: my-gcp-project-dev
    label: Development

# Optional per-service port overrides
port_overrides:
  auth-service: 9090
  payment-svc: 9091
```

## Logging

- TUI: Log/Alerts Panel shows recent events inline
- File: `~/.kpm/logs/kpm-<date>.log` — append-only, rotated daily
- Log levels: `INFO`, `WARN`, `ERROR`

## Session History

- File: `~/.kpm/history.json`
- Stores last N sessions (default: 5) with cluster, namespace, and service list
- Shown as "Recent" shortcut on wizard start screen

## Keybindings

| Key | Action |
|---|---|
| `Enter` | Confirm / Select |
| `Space` | Toggle selection (multi-select) |
| `Tab` | Switch panel focus |
| `/` | Focus fuzzy search |
| `d` / `Delete` | Stop selected forward |
| `r` | Retry errored forward |
| `R` | Resume last session |
| `q` / `Ctrl+C` | Quit (stops all forwards) |
| `?` | Toggle help overlay |

## GCP Projects

Configured by the user in `~/.kpm/config.yaml`. No defaults are hardcoded.

## Key Design Decisions

- `client-go` port-forward over `kubectl` subprocess for full lifecycle control
- Target `Service` (not `Pod`) for built-in load balancing
- Multi-kubeconfig support: each cluster context is isolated
- TEA architecture: predictable state, easy to test
- Ephemeral sessions + history file for resilience against terminal crashes
- Reconnect with backoff; surface `error` state visually, never silently drop

## Commands

```bash
kpm           # Launch TUI
kpm list      # List active forwards (non-interactive, reads state file)
kpm stop      # Stop all forwards
kpm version   # Print version
```

## Installation

```bash
# Homebrew (macOS)
brew install geraldofigueiredo/tap/kpm

# go install
go install github.com/geraldofigueiredo/kportmaster/cmd/kpm@latest

# Manual binary (GitHub Releases)
curl -sSL https://github.com/geraldofigueiredo/kportmaster/releases/latest/download/kpm_darwin_arm64 -o /usr/local/bin/kpm
chmod +x /usr/local/bin/kpm
```

## Platform Support

- Primary: macOS (arm64 + amd64)
- Planned: Linux
- Not planned: Windows

## Development

```bash
go build -o kpm ./cmd/kpm
./kpm
```

Go >= 1.25 required.

## Project Structure

```
kportmaster/
  cmd/kpm/          # entrypoint
  internal/
    gcp/            # GKE cluster discovery
    k8s/            # port-forward manager, reconnect logic
    tui/            # Bubble Tea models, panels, keybindings
    config/         # config.yaml loader
    history/        # session history read/write
    portmgr/        # conflict resolver, port binding check
  ~/.kpm/
    config.yaml
    history.json
    logs/
```
