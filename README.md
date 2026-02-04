<div align="center">

# runpod cli

runpod is the cli tool to manage gpu pods, serverless endpoints, and more on [runpod.io](https://runpod.io).

_note: all pods automatically come with runpod cli installed with a pod-scoped api key._

</div>

## table of contents

- [runpod cli](#runpod-cli)
  - [table of contents](#table-of-contents)
  - [get started](#get-started)
    - [install](#install)
      - [linux/macos (wsl)](#linuxmacos-wsl)
      - [macos](#macos)
      - [windows powershell](#windows-powershell)
  - [quick start](#quick-start)
  - [commands](#commands)
    - [pod management](#pod-management)
    - [serverless endpoints](#serverless-endpoints)
    - [file transfer](#file-transfer)
  - [output format](#output-format)
  - [legacy commands](#legacy-commands)
  - [acknowledgements](#acknowledgements)

## get started

### install

#### linux/macos (wsl)

```bash
wget -qO- cli.runpod.net | sudo bash
```

#### macos

```bash
brew install runpod/runpodctl/runpodctl
```

#### windows powershell

```powershell
wget https://github.com/runpod/runpodctl/releases/latest/download/runpod-windows-amd64.exe -O runpod.exe
```

## quick start

```bash
# configure api key
runpod config --apiKey=your_api_key

# list all pods
runpod pod list

# get a specific pod
runpod pod get pod_id

# create a pod
runpod pod create --image=runpod/pytorch:latest --gpu-type-id=NVIDIA_A100

# start/stop/delete a pod
runpod pod start pod_id
runpod pod stop pod_id
runpod pod delete pod_id
```

## commands

commands follow noun-verb pattern: `runpod <resource> <action>`

### pod management

```bash
runpod pod list                    # list all pods
runpod pod get <id>                # get pod details
runpod pod create --image=<img>    # create a pod
runpod pod update <id>             # update a pod
runpod pod start <id>              # start a stopped pod
runpod pod stop <id>               # stop a running pod
runpod pod delete <id>             # delete a pod
```

### serverless endpoints

```bash
runpod serverless list             # list endpoints (alias: sls)
runpod serverless get <id>         # get endpoint details
runpod serverless create           # create endpoint
runpod serverless update <id>      # update endpoint
runpod serverless delete <id>      # delete endpoint
```

other resources: `template` (alias: `tpl`), `volume` (alias: `vol`), `registry` (alias: `reg`)

### file transfer

send and receive files without api key using croc:

```bash
# send a file
runpod send data.txt
# output: code is: 8338-galileo-collect-fidel

# receive on another computer
runpod receive 8338-galileo-collect-fidel
```

## output format

default output is json (optimized for agents). use `--output` flag for alternatives:

```bash
runpod pod list                    # json (default)
runpod pod list --output=table     # human-readable table
runpod pod list --output=yaml      # yaml format
```

## legacy commands

legacy commands are still supported but deprecated. please update your scripts:

`get pod`, `create pod`, `remove pod`, `start pod`, `stop pod`

## acknowledgements

- [cobra](https://github.com/spf13/cobra)
- [croc](https://github.com/schollz/croc)
- [golang](https://go.dev/)
- [viper](https://github.com/spf13/viper)
