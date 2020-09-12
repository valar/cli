# <img alt="Valar CLI" src="https://user-images.githubusercontent.com/3391295/80893874-701c1500-8cd6-11ea-8805-e9bcb5196b0a.png" height="30">

This repository contains the command line interface for Valar.

## Configuration

The API and Invoke authentication token can be supplied either via environment variable `VALAR_TOKEN` or via flag `--api-token`. The Valar endpoint URL has to be supplied via environment variable `VALAR_ENDPOINT` or via flag `--api-endpoint`.

To supply the token and endpoint without an environment variable, you can create a configuration file in `~/.valar/valar.cloud.yml` containing a `token` and `endpoint`.

```bash
$ mkdir -p ~/.valar/
$ cat > ~/.valar/valar.cloud.yml <<EOF
token: $VALAR_TOKEN
endpoint: $VALAR_ENDPOINT
EOF
```

## Usage

### Projects

#### Set up a new project <span style="color: grey">(not implemented)</span>
```
valar create [--public] [project-name]
```
> Public projects can be invoked by any anonymous person.
#### Set the default project, endpoint and token <span style="color:grey">(not implemented)
```
valar use --project=[project-name] --api-token=[api-token] --api-endpoint=[api-endpoint]
```
#### Destroy a project <span style="color: grey">(not implemented)</span>
```bash
valar destroy [project-name]
```
> Destroying a project deletes all services and configuration associated with it. Use with care.
### Services
#### Create local configuration <span style="color:green">(implemented)</span>
```bash
valar init --type=[constructor] --project=[project-name] [service]
```
> Valar supports a variety of constructors. If you are looking for an up-to-date list, please refer to [the official documentation](https://docs.valar.dev).
#### Deploying a service <span style="color:green">(implemented)</span>
```bash
valar push
```
#### Listing all services in the project <span style="color: grey">(not implemented)</span>
```bash
valar list
```
#### Show the logs of the latest service version <span style="color: green">(implemented)</span>
```
valar logs [service]
```
#### Reverse service to old version <span style="color: grey">(not implemented)</span>
```bash
valar rollback [service] [versionid]
```
#### Delete a service <span style="color: grey">(not implemented)</span>
```bash
valar delete [service]
```
#### Set up service alias <span style="color: grey">(not implemented)</span>
```bash
valar alias [service] [alias]
```
> The alias has to match either `(projectname)-(servicealias).valar.dev` or if you have set up your own domain, `(servicealias).(user-owned-domain)`.

### Builds

#### Listing all builds <span style="color:green">(implemented)</span>
```bash
valar builds
```
#### Listing all builds with the given prefix <span style="color:green">(implemented)</span>
```bash
valar builds [prefix]
```
#### Inspecting a build <span style="color:green">(implemented)</span>
```bash
valar builds inspect [prefix]
```
#### Abort a specific build <span style="color: grey">(not implemented)</span>
```bash
valar builds --abort [taskid]
```
#### Show logs from build <span style="color:green">(implemented)</span>
```bash
valar builds logs [--follow] [taskid]
```
### Permissions
#### View permissions <span style="color: green">(implemented)</span>
```bash
valar auth
```
#### Allow someone to read/write/invoke <span style="color: green">(implemented)</span>
```bash
valar auth allow --user [user] --action [read|write|invoke|manage]
```
#### Forbid someone to read/write/invoke <span style="color: green">(implemented)</span>
```bash
valar auth forbid --user [user] --action [read|write|invoke|manage]
```
#### Reset permissions to project default <span style="color: grey">(not implemented)</span>
```bash
valar auth --reset
```
