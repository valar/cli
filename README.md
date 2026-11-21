# <img alt="Valar CLI" src="https://user-images.githubusercontent.com/3391295/80893874-701c1500-8cd6-11ea-8805-e9bcb5196b0a.png" height="30">

This repository contains the command line interface for Valar.

## Getting started

### Install via Homebrew

```bash
brew install valar/tap/valar
```

### Initial setup

```bash
valar config init
```

The interactive wizard will prompt for your API endpoint, token, and project. For non-interactive usage (e.g. in CI), pass all values as flags:

```bash
valar config init --token=[your-api-token] --project=[your-project]
```

## Usage

By default, Valar uses the default valarconfig file in `$HOME/.valar/config`. If the `VALARCONFIG` environment variable does exist, `valar` uses an effective configuration that is the result of merging the files listed in the `VALARCONFIG` variable.

`VALAR_TOKEN`, `VALAR_ENDPOINT` and `VALAR_PROJECT` take precedence over the active context and the project in `.valar.yml`, so CI needs no configuration file. Without a configured endpoint, `VALAR_TOKEN` talks to `https://api.valar.dev/v2`.

```bash
VALAR_TOKEN=... valar build push --wait
```

Tokens expire. Within 14 days of its token's expiry, a run prints one warning to stderr with the date and where to create a new token. A token that expired or was revoked fails with the server's reason.

### Machine-readable output

With `--json`, or `VALAR_OUTPUT=json`, every command prints its result as one JSON document on stdout. Lists are arrays of objects whose fields match the API's JSON. Logs (`service logs`, `build logs`, `build watch`, `batch logs`) are printed as one JSON object per line, progress goes to stderr, and an error is printed on stderr as:

```json
{"error": {"kind": "notFound", "message": "no builds available"}}
```

```bash
valar service list --json | jq -r '.[].name'
```

### Exit codes

| Code | Kind | Meaning |
|---|---|---|
| 0 | | Success |
| 1 | `failed` | The operation ran and failed, such as a build or deployment |
| 2 | `invalid` | Bad arguments, flags or configuration |
| 3 | `notFound` | Not found |
| 4 | `notAllowed` | Missing or rejected credentials or permissions |
| 5 | `conflict` | Conflict with the current state |
| 6 | `unavailable` | The server could not be reached or answered with an error |

`valar build status` exits with 1 for any build that is not done.

### Configuration

#### Initialize configuration

```bash
valar config init [--token token] [--project project] [--url url] [--name name] [--force]
```

#### Dump the current configuration as YAML

```bash
valar config view
```

#### Print the JSON Schema of `.valar.yml`

```bash
valar config schema
```

The schema is also checked in at [`schema/valar.yml.json`](schema/valar.yml.json), for editors. The CLI refuses a `.valar.yml` with keys the schema does not know, naming the key and its line.

#### Add an API endpoint

```bash
valar config endpoint set [endpoint] --token=[api-token] --url=[endpoint-url]
```

#### List configured API endpoints

```bash
valar config endpoint
```

#### Remove an API endpoint

```bash
valar config endpoint remove [endpoint]
```

#### Add a configuration context

```bash
valar config context set [context] --project=[project] --endpoint=[endpoint]
```

#### List configured CLI contexts

```bash
valar config context
```

#### Set a context as the default one

```bash
valar config context use [context]
```

#### Remove a configuration context

```bash
valar config context remove [context]
```

### Projects

These commands need Valar platform 2.11.0 or newer.

#### Set up a new project

```bash
valar projects create [--public] [project-name]
```

A project name is 3 to 20 lowercase letters or digits and starts with a letter. Some names are reserved. Each user can create at most 3 projects.

> [!WARNING]
> With `--public`, anyone can invoke the project's services without a token.

#### Delete a project

```bash
valar projects delete [--yes] [--cascade] [project-name]
```

Without `--cascade`, only empty projects can be deleted: a project with services, domains or key-value storage is refused. With `--cascade`, the project's services are deleted first, then its domain claims. The command asks you to type the project name to confirm; `--yes` skips that. With `--cascade` and no terminal to ask on, it refuses unless `--yes` is given.

> [!CAUTION]
> Deleting a project retires its name for good. No one can create a project with that name again, not even you.

### Services

#### Create local configuration

```bash
valar service init [--type=[constructor]] [--project=[project-name]] [service]
```

Without `--type`, the constructor is detected from the files in the current directory, and the first match wins:

| File | Constructor |
|---|---|
| `go.mod` | `go` |
| `package.json` | `node` |
| `requirements.txt` or `pyproject.toml` | `python` |
| `index.html` | `static` |
| `Dockerfile` | `docker` |

The choice is printed to stderr and written to `.valar.yml`. `build push` detects the constructor the same way when `.valar.yml` names none.

Valar supports a variety of constructors. If you are looking for an up-to-date list, please refer to [the official documentation](https://docs.valar.dev).

> [!TIP]
> Using the `--project` flag is optional, if it is not defined a value will be inferred from the default project set via the `config` command or the projects supplied by the API service.

#### Listing all services in the project

```bash
valar service list
```

#### Show the logs of the latest deployment

```bash
valar service logs [--follow] [--tail] [--skip n] [service]
```

#### Print a service's URLs

Linked custom domains come first, then the service's own `<project>-<service>` domains.

```bash
valar service url [--service service]
```

#### Enable a service

Right after a disable, this waits until the old instances have stopped.

```bash
valar service enable [--service service]
```

#### Disable a service

```bash
valar service disable [--service service]
```

#### Delete a service

```bash
valar service delete [--yes] [service]
```

Deletes the service with its builds, deployments, batch executions, environment, secrets, schedules and permission rules. Its linked domains are unlinked; the project keeps their claims. The service defaults to the one in `.valar.yml`. The command asks you to type the service name to confirm; `--yes` skips that, and without a terminal it refuses unless `--yes` is given. If a deletion fails, the service stays disabled and running the command again finishes it.

#### Listing all deployments of a service

```bash
valar deployment list
```

#### Roll out a specific build

```bash
valar deployment create [buildid]
```

Without a build ID, the build of the latest active deployment is redeployed with the service's current runtime variables.

#### Reverse service to the previous deployment

```bash
valar deployment rollback [--delta 1] [--env pinned|current]
```

With `--env pinned` (the default) the new deployment gets the environment the earlier deployment ran with, and the command names the variables whose values differ from the current ones. With `--env current` it gets the current variables.

### Environment variables

Variables are stored by Valar per service and target: `runtime` variables reach deployments and batch executions, `build` variables reach builds. The commands below act on runtime variables; pass `--build` for build variables or `--all` for both. Secret values are write-only and never shown again. These commands need Valar platform 2.9.0 or newer.

#### Set variables

```bash
valar env set [--build|--all] [--secret] [key]=[value]...
valar env set [--build|--all] [--secret] [key] < value-file
```

Given only a key, the value is read from standard input, or prompted for without echo. Prefer that for secrets, since arguments end up in shell history.

#### Remove variables

```bash
valar env unset [--build|--all] [key]...
```

#### List variables

```bash
valar env list [--build|--all] [--format=(table|raw)]
```

#### Show the change log

```bash
valar env history [--build|--all]
```

#### Move variables from `.valar.yml` to Valar

```bash
valar env import
```

Older versions kept variables in the `environment` lists of `.valar.yml` and sent them with every push. `valar env import` stores the plain entries as variables, unless they were changed on the service since, names the secrets that have to be set again with `valar env set --secret`, and empties the lists. Pushes no longer send the lists.

### Domains

#### List all domains attached to a project

```bash
valar domains
```

#### Add a domain to a project

```bash
valar domains add [domain]
```

#### Verify a domain

```bash
valar domains verify [domain]
```

#### Link a domain to a service

```bash
valar domains link [--insecure] [domain] ([service])
```

> [!CAUTION]
> If `--insecure` is enabled, the default HTTP-to-HTTPS redirection handler will be disabled and any plaintext HTTP requests will be forwarded to your service.

#### Unlink a domain from a service

```bash
valar domains unlink [domain] ([service])
```

#### Remove a domain

```bash
valar domains delete [domain]
```

### Builds

#### Pushing a new build

```bash
valar builds push [--no-deploy] [--service service] [--wait [--timeout 30m]]
```

`push` prints the build ID. With `--wait` it also streams the build log to stderr, waits for the build and its deployment, and exits non-zero if either fails. `--service` pushes to another service than the one in `.valar.yml`, with the same build settings.

#### Listing all builds

```bash
valar builds list [prefix]
```

#### Listing all builds with the given prefix

```bash
valar builds list [prefix]
```

#### Inspecting a build

```bash
valar builds inspect [prefix]
```

#### Abort a specific build

```bash
valar builds abort [prefix]
```

#### Show logs from build

```bash
valar builds logs [--follow] [--raw] [optional buildid]
```

#### Watch the build and status until its completed

Exits non-zero if the build fails.

```bash
valar builds watch [optional buildid]
```

#### Show build status

```bash
valar builds status [--exit] [buildid]
```

### Identity

#### Show the currently authenticated user

```bash
valar whoami
```

### Permissions

#### View permissions

```bash
valar auth list
```

#### Allow someone to read/write/invoke/manage

```bash
valar auth allow [path] [user] [read | write | invoke | manage]
```

#### Forbid someone to read/write/invoke/manage

```bash
valar auth forbid [path] [user] [read | write | invoke | manage ]
```

#### Remove a previously added permission

```bash
valar auth clear [path] [user] [read | write | invoke | manage ]
```

#### Check someones permission to perform an action

```bash
valar auth check [path] [user] [read | write | invoke | manage]
```

> [!WARNING]
> In case of a public project, this means only the project owner has write, read and invoke access, while any person may invoke a service of the project.

### Cron

#### View the scheduled invokes

```bash
valar cron list
```

#### Schedule a service invocation

```bash
valar cron set [name] [schedule] [--path path] [--data payload] [--service service] [--enabled|--disabled]
```

#### Remove a scheduled service invocation

```bash
valar cron delete [name] [--service service]
```

#### Trigger a cron invoke manually

```bash
valar cron trigger [name] [--service service]
```

#### Inspect the invocation history

```bash
valar cron inspect [name] [--service service]
```

## Agent skill

`skills/valar/SKILL.md` teaches coding agents to deploy and operate Valar services with this CLI: setup through environment variables, `--json` output, exit codes, the deploy flow and the app's contract. Install it the way your agent loads skills, for example by copying the `skills/valar` folder into the agent's skills directory.
