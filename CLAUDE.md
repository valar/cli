# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build          # Build binary to bin/valar (sets version=indev)
make test           # Run all tests: go test -v ./...
make clean          # Remove build artifacts
make install        # Copy bin/valar to /usr/local/bin/
go test -v -run TestName ./...  # Run a single test
```

## Architecture

This is the **Valar CLI** (`valar`), a Go CLI tool for managing services on the Valar serverless platform. It uses the Cobra command framework.

### Package Layout

- **`cmd/`** — Cobra command definitions. Each file covers a subject area (services, builds, deployments, env, domains, cron, auth/projects, config). Commands are registered in `cmd/root.go` via `init*Cmd()` functions.
- **`api/`** — HTTP client (`api.Client`) for the Valar v2 REST API. All backend communication goes through this package. Uses bearer token auth, JSON request/response, and streaming for logs.
- **`config/`** — Configuration management with two layers:
  - `CLIConfig` (global): endpoints, contexts, active context. Stored in `$HOME/.valar/config` (YAML). Supports multi-file merging via `VALARCONFIG` env var.
  - `ServiceConfig` (per-service): project, service name, build/deployment specs. Stored as `.valar.yml`, discovered by walking up from CWD.
- **`util/`** — Small helpers (table formatting).

### Key Patterns

- `runAndHandle()` in `cmd/root.go` wraps command run functions to convert errors into stderr output + exit code 1.
- `globalConfiguration` (`*config.CLIConfig`) is loaded in the root command's `PersistentPreRunE` and available to all subcommands.
- Service config (`.valar.yml`) is loaded on demand within individual commands that need it.
- The `builds push` command archives the working directory (tar.gz via `archiver`), uploads it, then optionally watches the build and creates a deployment.
