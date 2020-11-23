# <img alt="Valar CLI" src="https://user-images.githubusercontent.com/3391295/80893874-701c1500-8cd6-11ea-8805-e9bcb5196b0a.png" height="30">

This repository contains the command line interface for Valar.

## Configuration

The API and Invoke authentication token can be supplied either via environment variable `VALAR_TOKEN` or via flag `--api-token`. The Valar endpoint URL has to be supplied via environment variable `VALAR_ENDPOINT` or via flag `--api-endpoint`.

To supply the token and endpoint without an environment variable, you can create a configuration file in `~/.valar/valarcfg` containing a `token` and `endpoint`.

```bash
$ mkdir -p ~/.valar/
$ cat > ~/.valar/valar.cloud.yml <<EOF
token: $VALAR_TOKEN
endpoint: $VALAR_ENDPOINT
EOF
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
#### Destroy a project <span style="color: grey">[not implemented]</span>
```bash
valar project  destroy [project-name]
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
#### Reverse service to the previous deployment
```bash
valar deployments rollback [--delta 1]
```
#### Deploy a specific build of a service 
```bash
valar builds deploy [buildid]
```
#### Delete a service <span style="color: grey">[not implemented]</span>
```bash
valar delete [service]
```
#### Set up service alias <span style="color: grey">[not implemented]</span>
```bash
valar alias [service] [alias]
```
> The alias has to match either `(projectname)-(servicealias).valar.dev` or if you have set up your own domain, `(servicealias).(user-owned-domain)`.

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
#### Abort a specific build [not implemented]
```bash
valar builds abort [buildid]
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
#### Reset permissions to project default <span style="color: grey">[not implemented]</span>
```bash
valar auth --reset
```

> In case of a public project, this means only the project owner has write, read and invoke access, while any person may invoke a service of the project.
