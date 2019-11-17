# <img alt="Valar CLI" src="https://user-images.githubusercontent.com/3391295/69001175-4874f400-08d2-11ea-99e8-63d256d7da03.png" height="30">

This repository contains the command line interface for Valar.

## Configuration

The API and Invoke authentication token can be supplied either via environment variable `VALAR_TOKEN` or via flag `--api-token`. The Valar endpoint URL has to be supplied via environment variable `VALAR_ENDPOINT` or via flag `--api-endpoint`.

## Usage

### Projects

```bash
# Set up a new project
valar create [--public] {my-project}
# Destroy a project
valar destroy [my-project]
```
### Services
```bash
# Create local configuration
valar init --env=[my-constructor] --project=[my-project] [my-function]
# Deploying a service
valar push
# Listing all services
valar list
# Listing all versions of a service
valar list [service]
# Reverse service to old version
valar rollback [service] [versionid]
# Delete a service
valar delete [service]
# Set up service alias
valar alias [service] [my-alias]
```
### Builds
```bash
# Listing all builds
valar build
# Viewing a specific build
valar build [taskid]
# Abort a specific build
valar build --abort [taskid]
```
### Permissions
```bash
# View permissions
valar auth [service]
# Allow somebody to read/write/invoke
valar auth --allow [user]:[read|write|invoke] [service]
# Disallow somebody to read/write/invoke
valar auth --forbid [user]:[read|write|invoke] [service]
# Reset permissions to project default
valar auth --reset
```
