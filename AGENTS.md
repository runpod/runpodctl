# AGENTS.md

runpod cli: command-line tool for managing gpu pods, serverless endpoints, and developing serverless applications on runpod.

## codebase structure

```
runpod/
├── main.go                      # entry point, version injection
├── cmd/                         # cli commands (cobra)
│   ├── root.go                     # root command, config init
│   ├── config.go                   # api key & ssh config
│   ├── ssh.go                      # ssh key management & connections
│   ├── pod/                        # pod commands
│   │   ├── pod.go                     # parent command
│   │   ├── list.go                    # list pods
│   │   ├── get.go                     # get pod by id
│   │   ├── create.go                  # create pod
│   │   ├── update.go                  # update pod
│   │   ├── start.go                   # start pod
│   │   ├── stop.go                    # stop pod
│   │   └── delete.go                  # delete pod
│   ├── serverless/                 # serverless endpoint commands (alias: sls)
│   │   ├── serverless.go              # parent command
│   │   ├── list.go                    # list endpoints
│   │   ├── get.go                     # get endpoint
│   │   ├── create.go                  # create endpoint
│   │   ├── update.go                  # update endpoint
│   │   └── delete.go                  # delete endpoint
│   ├── template/                   # template commands (alias: tpl)
│   │   └── ...
│   ├── volume/                     # network volume commands (alias: vol)
│   │   └── ...
│   ├── registry/                   # container registry auth (alias: reg)
│   │   └── ...
│   ├── transfer/                   # file transfer (croc)
│   │   ├── transfer.go                # send/receive commands
│   │   ├── croc.go                    # croc implementation
│   │   └── rtt.go                     # relay rtt testing
│   ├── project/                    # serverless project workflow
│   │   ├── project.go                 # create, dev, deploy, build
│   │   ├── functions.go               # project lifecycle logic
│   │   └── starter_examples/          # template projects
│   └── legacy/                     # deprecated command aliases
│       └── legacy.go                  # backwards compatibility
├── internal/
│   ├── api/                        # api clients
│   │   ├── client.go                  # rest client
│   │   ├── pods.go                    # pod api methods
│   │   ├── endpoints.go               # endpoint api methods
│   │   ├── templates.go               # template api methods
│   │   ├── volumes.go                 # volume api methods
│   │   ├── registry.go                # registry auth methods
│   │   └── graphql.go                 # graphql client (fallback)
│   └── output/                     # output formatting
│       └── output.go                  # json/yaml/table output
├── docs/                           # generated documentation
└── .goreleaser.yml                 # release configuration
```

## key technologies

- **go 1.24** with modules
- **cobra** — cli framework
- **viper** — configuration management
- **croc** — peer-to-peer file transfer (no api key required)
- **rest api** — primary api (https://rest.runpod.io/v1)
- **graphql** — fallback for features rest lacks

## configuration

- config file: `~/.runpod/config.toml`
- api key via: `runpod config --apiKey=xxx`
- environment override: `RUNPOD_API_KEY`, `RUNPOD_API_URL`

## build commands

```bash
# local development build
make local
# output: bin/runpod

# cross-platform release builds
make release
# outputs: bin/runpod-{os}-{arch}

# run tests
go test ./...
```

## command structure

commands follow noun-verb pattern: `runpod <resource> <action>`

| command | description |
|---------|-------------|
| `runpod pod list` | list all pods |
| `runpod pod get <id>` | get pod by id |
| `runpod pod create --image=<img>` | create a pod |
| `runpod pod start <id>` | start a stopped pod |
| `runpod pod stop <id>` | stop a running pod |
| `runpod pod delete <id>` | delete a pod |
| `runpod serverless list` | list endpoints (alias: sls) |
| `runpod serverless get <id>` | get endpoint details |
| `runpod template list` | list templates (alias: tpl) |
| `runpod volume list` | list network volumes (alias: vol) |
| `runpod registry list` | list registry auths (alias: reg) |
| `runpod send <file>` | send file via croc |
| `runpod receive <code>` | receive file via croc |
| `runpod ssh list-keys` | list account ssh keys |
| `runpod ssh connect <pod>` | show ssh connect command |
| `runpod project create` | create serverless project |
| `runpod project dev` | start dev session |
| `runpod project deploy` | deploy as endpoint |
| `runpod config --apiKey=xxx` | configure api key |

## output format

default output is json (for agents). use `--output=table` for human-readable format.

```bash
runpod pod list                    # json output
runpod pod list --output=table     # table output
runpod pod list --output=yaml      # yaml output
```

## where to make changes

| task | location |
|------|----------|
| add new rest api operation | `internal/api/` |
| add new cli command | `cmd/<resource>/` |
| modify pod commands | `cmd/pod/` |
| modify serverless commands | `cmd/serverless/` |
| add project template | `cmd/project/starter_examples/` |
| change file transfer | `cmd/transfer/` |
| update ssh logic | `cmd/ssh.go` |
| modify build/release | `makefile`, `.goreleaser.yml` |

## api layer pattern

rest api operations in `internal/api/`:
1. define request/response structs
2. call appropriate http method (Get, Post, Patch, Delete)
3. parse json response
4. return typed result or error

graphql fallback in `internal/api/graphql.go` for features rest doesn't support (ssh keys, detailed pod info).

## important notes

- **never start/stop servers** — user handles that
- file transfer (`send`/`receive`) works without api key
- version is injected at build time via `-ldflags`
- config auto-migrates from `~/.runpod.yaml` to `~/.runpod/config.toml`
- ssh keys are auto-generated and synced to account on `config` command
- all text output is lowercase and concise
- default output format is json for agent consumption
- always add unit + e2e tests for new behavior
- e2e tests must clean up resources they create (use `t.Cleanup`)
