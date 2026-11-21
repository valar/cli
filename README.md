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

Before storing a token, `config init` and `config endpoint set` (when the endpoint has a URL) ask the platform about it with `/users/info`: they refuse agent tokens, which are for MCP clients, and warn when the token expires within 14 days.

The CLI writes `~/.valar/config`, which holds tokens, readable only by you (mode 0600, in a 0700 directory). When it rewrites a configuration file that other users can read, it tightens the file to 0600 and says so on stderr.

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
valar service logs [--follow] [--tail] [--skip n] [--all | --deployment n] [--timestamps] [--service service]
```

`--all` shows every deployment's log, oldest first, and `--deployment` one deployment's. `--timestamps` prefixes each line with the time it was written. With `--json`, each line is a record such as `{"ts": "...", "deployment": "3", "stream": "output", "line": "..."}`; platform events have `stream: "platform"` with `event` and `attrs`, and further fields may appear. An older platform yields `{"line": "..."}` only. Following records reconnects at the end of the log, so lines written while reconnecting can be missed.

#### Print a service's URLs

Linked custom domains come first, then the service's own `<project>-<service>` domains. The command reads the service's inspect route, so a token bound to the service works; against an older platform it reads the project's service listing instead.

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

#### Show and change a service's settings

```bash
valar service settings [service]
valar service settings set cpu=500 memory=2048 [--service service]
valar service settings reset memory [--service service]
```

`cpu` is in millicores (100 to 2000, in steps of 100) and `memory` in MiB (128 to 4096). `settings` shows each value, whether it was set or is the platform's default, its bounds, and the project's budget; with `--json` it prints the API's response. `reset` returns a setting to its default. A change takes effect with the next deployment, which pins the settings it runs with. Raising `cpu`, `memory` or `minRunning` needs a token with full access (scope `all`), within the project's budget; otherwise the command exits with code 4. A value out of its bounds exits with code 2.

A push applies the settings declared in `.valar.yml`, next to `skip`:

```yaml
deployment:
  cpu: 500
  memory: 512
```

Before a push that deploys, the declared keys are sent as one change, so the push's deployment runs with them; undeclared settings and settings removed from the file keep their value. With `--no-deploy` or `skip: true` nothing is sent. `http` and `minRunning` are accepted too, but the platform refuses values other than `true` and `0` for now.

#### Listing all deployments of a service

```bash
valar deployment list
```

A failed deployment's status names the kind of failure, such as `failed (health)`, on platforms that report it. With `--json`, the API's `failure` object is passed through.

#### Roll out a specific build

```bash
valar deployment create [buildid]
```

Without a build ID, the build of the latest active deployment is redeployed with the service's current runtime variables.

#### Reverse service to the previous deployment

```bash
valar deployment rollback [--delta 1] [--env pinned|current] [--settings pinned|current]
```

The target is the newest deployment below the live one (the newest `done` deployment) that served: `done` or `expired`, without a failure. Failed deployments, and deployments above the live one, are skipped, and `--delta n` picks the n-th such deployment. Earlier versions of the CLI picked by version number alone, so they could roll back to a failed deployment.

With `--env pinned` (the default) the new deployment gets the environment the earlier deployment ran with, and the command names the variables whose values differ from the current ones. With `--env current` it gets the current variables. `--settings` works the same way for the service's settings; without it, the platform pins them.

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

`push` prints the build ID. Unless it skips the deployment, it first applies the settings declared in `.valar.yml` (see [the service's settings](#show-and-change-a-services-settings)). With `--wait` it also streams the build log to stderr, waits for the build and its deployment, and exits non-zero if either fails. A failed deployment is reported with its kind (`build`, `schedule`, `capacity`, `start` or `health`), the reason, the version that keeps serving and the command that reads its log; on a terminal outside CI (`CI` unset), the last 20 lines of that log are printed too. `--service` pushes to another service than the one in `.valar.yml`, with the same build settings.

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

Prints the user, their projects and, on platforms that report it, what the token in use grants: its kind, binding, scopes, creation, expiry, last use and the token that created it.

### Tokens

These commands need a newer Valar platform; against an older one they fail with "the server has no token API".

A token created with `valar token create` acts as you. It is a child of the token that creates it: it expires no later than that token and is revoked with it. Only a token created in the console or with authctl, with full access and no project binding, can create tokens.

#### Create a token

```bash
valar token create [--project project] [--service service] [--scopes read,deploy] [--ttl 90d] [--agent] name
```

`--project` binds the token to one project and `--service` to one service of it. `--scopes` limits it to some of `read`, `deploy`, `invoke`, `kv` and `all` (the default). `--ttl` takes days (`30d`) or a duration (`12h`); the default is 90 days and the maximum 365. A token for CI that pushes and deploys one project:

```bash
valar token create ci --project acme --scopes read,deploy
```

The token goes to stdout and the rest to stderr, so `TOKEN=$(valar token create ci)` captures only the token; with `--json` it is the `value` field. Valar shows it once.

#### Create a token for an AI agent

```bash
valar token create claude --agent --project acme [--service web] [--ttl 30d]
```

An agent token works only at the platform's MCP endpoint (`https://api.valar.dev/mcp`, or the API's origin followed by `/mcp` on another installation), not with this CLI or the API. It is bound to one project, can only read, and lasts 7 days by default and at most 30. The platform refuses it until the installation turns on its MCP endpoint. After creating it, the command prints how to configure MCP clients; with `--json`, `mcpUrl` holds the endpoint.

Keep the token in a password manager or the client's secret store and give it to the client as an environment variable, here `VALAR_AGENT_TOKEN`, rather than writing it into a configuration file. For Claude Code:

```bash
claude mcp add --transport http valar https://api.valar.dev/mcp \
  --header 'Authorization: Bearer ${VALAR_AGENT_TOKEN}'
```

Other MCP clients that speak Streamable HTTP and send a static header take the same URL and header, as in an `mcpServers` entry:

```json
{
  "mcpServers": {
    "valar": {
      "type": "http",
      "url": "https://api.valar.dev/mcp",
      "headers": { "Authorization": "Bearer ${VALAR_AGENT_TOKEN}" }
    }
  }
}
```

Do not run the agent where it can read `~/.valar`, which holds your own token.

#### List tokens

```bash
valar token list [--project project]
```

Lists your tokens, including revoked and expired ones for 30 days. With `--project`, it lists every token bound to the project, whoever owns it, and needs permission to manage the project.

#### Revoke a token

```bash
valar token revoke [--project project] id
```

Revokes the token with the ID that `valar token list` shows, and the tokens it created. With `--project`, it revokes a token from the project's list.

### Deploys from CI

Where the installation trusts a CI provider's identity tokens, a CI job can deploy without a stored token. A trust policy lets the job exchange its identity token for a token that acts as the policy's creator, bound to one service with `read` and `deploy`, for an hour. These commands need permission to manage the project, a token created in the console or with authctl, and a newer Valar platform.

#### Add a trust policy

```bash
valar trust add [--project project] --service web --github-repo acme/web --branch main --workflow deploy.yml [--event push]
valar trust add [--project project] --service web --github-repo acme/web --environment production --workflow deploy.yml
valar trust add [--project project] --service 'webpr*' --github-repo acme/web --pull-requests
valar trust add [--project project] --service web --subject sub [--claim name=value]... [--issuer url]
```

`--github-repo` builds the policy for GitHub Actions. It reads the repository's ID, its owner's ID and its OIDC subject format from GitHub, writes the subject as GitHub will issue it (with immutable IDs where the repository uses them), and pins `repository_id` and `repository_owner_id`. A branch or environment policy also pins `event_name` (`--event`, `push` by default) and `workflow_ref` (`--workflow`), so other workflows and triggers do not match. `--workflow deploy.yml@refs/heads/release` names the ref an environment's workflow runs from; it is the default branch otherwise. Protect an environment that a policy names with deployment branch rules, because GitHub issues its subject to every job that names it.

A `--pull-requests` policy names a preview prefix followed by `*`: pull request 42 may deploy only `webpr42`. GitHub issues no identity tokens to pull requests from forks.

For a private repository, set `GH_TOKEN` to a GitHub token that can read it, such as `GH_TOKEN=$(gh auth token)`. `--subject` and `--claim` set the policy by hand, for other CI providers or subject formats `--github-repo` does not know. `--issuer` defaults to `https://token.actions.githubusercontent.com`. `create` is an alias of `add`.

#### List and remove trust policies

```bash
valar trust list [--project project]
valar trust remove [--project project] id
```

Removing a policy revokes the tokens it issued. `delete` is an alias of `remove`.

#### Exchange an identity token

```bash
valar auth exchange [--service service] [--project project] [--id-token-file path | --id-token jwt] [--audience audience] [--export]
```

Prints a token for the service, which comes from `--service` or `.valar.yml`. In a GitHub Actions job with `permissions: id-token: write`, the command requests the identity token itself, for `--audience` (the endpoint's origin, such as `https://api.valar.dev`, by default); elsewhere it reads it from `--id-token-file`, `--id-token` or stdin. In GitHub Actions it masks the token in the log, and `--export` sets `VALAR_TOKEN` for the job's later steps instead of printing it:

```bash
VALAR_TOKEN=$(valar auth exchange --service web)
export VALAR_TOKEN
valar build push --wait
```

When no policy admits the identity token, the error shows the claims a policy matches, such as `sub` and `workflow_ref`, to compare with `valar trust list`. The [Valar GitHub Action](https://github.com/valar/action) does all of this when it gets no `token` input.

### Audit log

```bash
valar audit [--project project] [--limit n] [--before id]
```

Shows the active project's audit log, newest first: an attempt row before each change made through the API and a result row after it, with the token, client and source address, and per-minute counts of refused requests. It needs permission to manage the project and a newer Valar platform. A page holds `--limit` rows (100 by default, at most 500); when a page is full, the command for the next one is printed to stderr.

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
