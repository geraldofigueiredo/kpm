# kportmaster (kpm)

Terminal UI for managing Kubernetes port-forwards across GCP/GKE clusters.

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)
![Platform](https://img.shields.io/badge/platform-macOS-lightgrey)

## Features

- **Wizard-based flow** — select GCP project → cluster → namespace → services
- **Multi-select** — forward multiple services in a single step
- **Auto port conflict resolution** — scans upward from the desired port if it's already in use
- **Inline port editing** — press `e` on any forward to change the local port on the fly
- **Reconnect with backoff** — retries up to N times on failure; surfaces error state visually
- **Pause / Resume** — temporarily stop a forward without removing it
- **Session registry** — forwards persist across terminal crashes; resume with `R`
- **Named profiles** — save and load sets of forwards with `s` / `o`
- **Live service logs** — stream pod logs directly in the TUI
- **Health checks** — periodic TCP probes with degraded-state indicator

## Requirements

- Go 1.22+
- `gcloud` CLI authenticated (`gcloud auth application-default login`)
- Access to one or more GKE clusters via GCP

## Installation

**From source**

```bash
git clone https://github.com/geraldofigueiredo/kportmaster.git
cd kportmaster
make install          # builds and copies to /usr/local/bin
```

**go install**

```bash
go install github.com/geraldofigueiredo/kportmaster/cmd/kpm@latest
```

## Usage

```bash
kpm              # launch TUI
kpm list         # list active forwards (non-interactive)
kpm stop         # stop all forwards
kpm version      # print version
```

## Keybindings

| Key | Action |
|---|---|
| `a` | Add new forwards (open wizard) |
| `e` | Edit local port of selected forward |
| `r` | Resume selected (paused / error / stopped) |
| `R` | Resume all forwards |
| `p` | Pause selected forward |
| `P` | Pause all forwards |
| `d` / `Delete` | Remove selected forward |
| `X` | Remove all forwards |
| `c` | Copy `localhost:<port>` to clipboard |
| `s` | Save current forwards as a named profile |
| `o` | Load a saved profile |
| `Tab` | Cycle panel focus (forwards → logs → app logs) |
| `l` | Toggle log panels |
| `m` | Open command menu |
| `?` | Toggle help overlay |
| `q` / `Ctrl+C` | Quit (stops all forwards) |

## Configuration

File: `~/.kpm/config.yaml`

```yaml
defaults:
  namespace: default
  reconnect_retries: 3
  reconnect_backoff_seconds: 2

projects:
  - id: my-gcp-project-prod
    label: Production
  - id: my-gcp-project-dev
    label: Development

# Optional per-service local port overrides
port_overrides:
  auth-service: 9090
  payment-svc: 9091
```

## Project Structure

```
kportmaster/
  cmd/kpm/          # entrypoint
  internal/
    gcp/            # GKE cluster discovery
    k8s/            # port-forward manager, reconnect logic
    tui/            # Bubble Tea models and panels
    config/         # config.yaml loader
    history/        # session history read/write
    portmgr/        # conflict resolver, port binding check
    profiles/       # named profile save/load
    registry/       # persistent forward registry
```

## Development

```bash
make build    # build ./kpm
make install  # build + copy to /usr/local/bin
make clean    # remove binary
```

## License

MIT
