---
name: valar
description: Deploy and operate services on a Valar platform with the `valar` CLI. Use when a repository has a `.valar.yml`, or when asked to deploy, roll back, inspect logs, or manage environment variables of a Valar service.
---

# Valar

Valar builds a folder into a container image and runs it as a service. Instances start on the first request and pause when idle. Drive it through the `valar` CLI. The API host also describes itself:

| Document | Contents |
|---|---|
| `<host>/llms.txt` | The API guide, one line per operation |
| `<host>/openapi.yaml` | Every operation in full |
| `<host>/schema/valar.yml.json` | The `.valar.yml` schema; `valar config schema` prints the same |

Here `<host>` is `VALAR_ENDPOINT` without its `/v2` suffix.

## Setup

Set the credentials in the environment rather than running the interactive `valar config` wizard:

| Variable | Value |
|---|---|
| `VALAR_TOKEN` | The API token |
| `VALAR_ENDPOINT` | The API URL, ending in `/v2` |
| `VALAR_PROJECT` | The project to act on |

These take precedence over `~/.valar/config` and `.valar.yml`. Check them with `valar whoami`.

## Always use `--json`

Add `--json`, or set `VALAR_OUTPUT=json`, on every command:

- results arrive as one JSON document on stdout, and progress goes to stderr;
- logs arrive as one JSON object per line;
- errors arrive on stderr as `{"error": {"kind", "message"}}`.

Branch on the exit code, not on text:

| Code | Kind | Meaning | What to do |
|---|---|---|---|
| 0 | | Success | |
| 1 | `failed` | A build or deployment ran and failed | Read the logs |
| 2 | `invalid` | Bad arguments, flags or `.valar.yml` | Fix the call; retrying won't help |
| 3 | `notFound` | Not found | |
| 4 | `notAllowed` | The token is missing, wrong, or lacks permission | Ask the user; don't retry |
| 5 | `conflict` | Conflict with the current state | Re-read the state, then decide |
| 6 | `unavailable` | Server unreachable or erroring | Retry later |

## Deploy

1. `valar service init <name> --json` writes `.valar.yml`.
   - The constructor is detected from the files: `go.mod` → go, `package.json` → node, `requirements.txt` or `pyproject.toml` → python, `index.html` → static, `Dockerfile` → docker.
   - Pass `--type` to override the detection.
2. `valar build push --wait --json` uploads the folder (ignoring `.git`, `node_modules` and `.valar.yml`), builds it, deploys the build, and exits non-zero if either fails. The build log streams to stderr.
3. `valar service url --json` gives the service's URLs.

If the build fails, read its log with `valar build logs <build-id>`. If the build passes but requests fail, read `valar service logs --json`. `valar deployment rollback` deploys the previous version again.

## The app's contract

- **Port.** Serve HTTP on port 8080. With a Dockerfile, the first TCP port the image `EXPOSE`s is used, falling back to 8080.
- **Presets:**

  | Preset | What runs |
  |---|---|
  | go | The built binary |
  | node | The `start` script of `package.json` |
  | python | `app.py` in a venv with `requirements.txt` installed |
  | static | Serves the folder |

- **Requests.** Each request must finish within 10 seconds. Instances pause when idle, so don't rely on background work between requests or on local files surviving.
- **Logs.** Stdout and stderr are the service's logs.
- **Configuration and secrets.** Keep them out of the repository. Use `valar env set NAME=VALUE`, or `valar env set --secret NAME=VALUE` for write-only values. Changes apply to the next deployment. `valar deployment create` with no build ID redeploys the current build with the current variables.
- **Token.** The platform injects `VALAR_TOKEN` into the service for calls back to the API.

## Destructive commands

These need explicit approval from the user:

- `valar service delete`, which deletes the service, its builds, deployments, environment and domain links;
- `valar project delete --cascade`, which deletes every service in the project.

Both ask the user to type the name. With `--yes` they don't ask, so pass `--yes` only when the user has named the exact service or project to delete.
