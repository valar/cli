# <img alt="Valar CLI" src="https://user-images.githubusercontent.com/3391295/80893874-701c1500-8cd6-11ea-8805-e9bcb5196b0a.png" height="30">

This repository contains the command line interface for Valar.

## Getting started

### Install via Homebrew
```
brew install valar/tap/valar
```

## Configuration

```bash
# Add the Valar API endpoint
valar config endpoint add default --token=[your-api-token] --url=https://api.valar.dev/v2
# Add a new project context
valar config context add default --project=[your-project] --endpoint=default
# Set the configured context as the default one
valar config context use default
```

## Usage

### Configuration

> By default, Valar uses the default valarconfig file in `$HOME/.valar/config`. If the `VALARCONFIG` environment variable does exist, `valar` uses an effective configuration that is the result of merging the files listed in the `VALARCONFIG` variable.

#### Dump the current configuration as YAML
```
valar config view
```

#### Add an API endpoint
```
valar config endpoint set [endpoint] --token=[api-token] --url=[endpoint-url]
```

#### List configured API endpoints
```
valar config endpoint
```

#### Remove an API endpoint
```
valar config endpoint remove [endpoint]
```

#### Add a configuration context
```
valar config context set [context] --project=[project] --endpoint=[endpoint]
```

#### List configured CLI contexts
```
valar config context
```

#### Set a context as the default one
```
valar config context use [context]
```

#### Remove a configuration context
```
valar config context remove [context]
```

### Projects

#### Set up a new project <span style="color: grey">[not implemented]</span>
```
valar projects create [--public] [project-name]
```
> Public projects can be invoked by any anonymous person.
#### Delete a project <span style="color: grey">[not implemented]</span>
```bash
valar projects delete [project-name]
```
> Destroying a project deletes all services and configuration associated with it. Use with care.
### Services
#### Create local configuration
```bash
valar init --type=[constructor] [--project=[project-name]] [service]
```
> Valar supports a variety of constructors. If you are looking for an up-to-date list, please refer to [the official documentation](https://docs.valar.dev).

> Using the `--project` flag is optional, if it is not defined a value will be inferred from the default project set via the `config` command or the projects supplied by the API service.

#### Deploying a service
```bash
valar push [--skip-deploy]
```

#### Listing all services in the project
```bash
valar list
```

#### Show the logs of the latest deployment
```bash
valar logs [--follow] [--tail] [--skip n] [service]
```

#### Listing all deployments of a service 
```bash
valar deploys
```

#### Roll out a specific build
```bash
valar deploys create [buildid]
```

#### Reverse service to the previous deployment
```bash
valar deploys rollback [--delta 1]
```

#### Enable a service [not implemented]
```bash
valar enable [service]
```

#### Disable a service [not implemented]
```bash
valar disable [service]
```

### Environment variables

#### Set a variable
```bash
valar env set [--build] [--secret] [key]=[value]
```

#### Delete a variable
```bash
valar env delete [--build] [key]
```

#### List variables
```bash
valar env [--build] [--format=(table|raw)]
```

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

#### Listing all builds
```bash
valar builds
```
#### Listing all builds with the given prefix
```bash
valar builds [prefix]
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
valar builds logs [--follow] [optional buildid]
```
#### Show build status
```bash
valar builds status [--exit] [buildid]
```
### Permissions
#### View permissions 
```bash
valar auth
```
#### Allow someone to read/write/invoke/manage
```bash
valar auth allow --user [user] --action [read|write|invoke|manage]
```
#### Forbid someone to read/write/invoke/manage
```bash
valar auth forbid --user [user] --action [read|write|invoke|manage]
```
#### Reset permissions to project default [not implemented]
```bash
valar auth --reset
```

> In case of a public project, this means only the project owner has write, read and invoke access, while any person may invoke a service of the project.
