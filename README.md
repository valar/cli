# <img alt="Valar CLI" src="https://user-images.githubusercontent.com/3391295/80893874-701c1500-8cd6-11ea-8805-e9bcb5196b0a.png" height="30">

This repository contains the command line interface for Valar.

## Getting started

### Install via Homebrew
```
brew install valar/tap/valar
```

### Install via cURL
```
curl -sSL https://cli.valar.dev | bash -
```

## Configuration

The API and authentication token can be supplied either via environment variable `VALAR_TOKEN` or via flag `--api-token`. The API endpoint URL has to be supplied via environment variable `VALAR_ENDPOINT` or via flag `--api-endpoint`.

To supply the token and endpoint without an environment variable, use
```
valar config --api-endpoint https://api.valar.dev/v1 --api-token [YOUR TOKEN]
```

## Usage

### Basics

#### Set the default endpoint and token
```
valar config --api-token=[api-token] --api-endpoint=[api-endpoint]
```

### Projects

#### Set up a new project <span style="color: grey">[not implemented]</span>
```
valar project create [--public] [project-name]
```
> Public projects can be invoked by any anonymous person.
#### Delete a project <span style="color: grey">[not implemented]</span>
```bash
valar project delete [project-name]
```
> Destroying a project deletes all services and configuration associated with it. Use with care.
### Services
#### Create local configuration
```bash
valar init --type=[constructor] --project=[project-name] [service]
```
> Valar supports a variety of constructors. If you are looking for an up-to-date list, please refer to [the official documentation](https://docs.valar.dev).
#### Deploying a service
```bash
valar push
```
#### Listing all services in the project
```bash
valar list
```
#### Show the logs of the latest deployment
```
valar logs [service]
```
#### Listing all deployments of a service 
```bash
valar deployments
```

#### Roll out a specific build
```bash
valar deployments create [buildid]
```

#### Reverse service to the previous deployment
```bash
valar deployments rollback [--delta 1]
```
#### Delete a service [not implemented]
```bash
valar delete [service]
```

### Domains

#### Add a domain [not implemented]
```bash
valar domains add [domain]
```

#### Link a domain (or subdomain) to a service [not implemented]
```
valar domains link [domain] [service]
```

#### Remove a domain and all domain links [not implemented]
```
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
valar auth forbid --user [user] --action
```
#### Reset permissions to project default [not implemented]
```bash
valar auth --reset
```

> In case of a public project, this means only the project owner has write, read and invoke access, while any person may invoke a service of the project.
