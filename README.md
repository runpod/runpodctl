<div align="center">

# runpodctl cli

runpodctl is the cli tool to manage gpu pods, serverless endpoints, and more on [runpod.io](https://runpod.io).

_note: all pods automatically come with runpodctl installed with a pod-scoped api key._

</div>

## table of contents

- [runpodctl cli](#runpodctl-cli)
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
wget https://github.com/runpod/runpodctl/releases/latest/download/runpodctl-windows-amd64.exe -O runpodctl.exe
```

## quick start

```bash
# configure api key
runpodctl config --apiKey=your_api_key

# list all pods
runpodctl pod list

# get a specific pod
runpodctl pod get pod_id

# create a pod
runpodctl pod create --image=runpod/pytorch:latest --gpu-id=NVIDIA_A100

# start/stop/delete a pod
runpodctl pod start pod_id
runpodctl pod stop pod_id
runpodctl pod delete pod_id
```

## commands

commands follow noun-verb pattern: `runpodctl <resource> <action>`

### pod management

```bash
runpodctl pod list                    # list all pods
runpodctl pod get <id>                # get pod details
runpodctl pod create --image=<img>    # create a pod
runpodctl pod update <id>             # update a pod
runpodctl pod start <id>              # start a stopped pod
runpodctl pod stop <id>               # stop a running pod
runpodctl pod delete <id>             # delete a pod
```

### serverless endpoints

```bash
runpodctl serverless list             # list endpoints (alias: sls)
runpodctl serverless get <id>         # get endpoint details
runpodctl serverless create           # create endpoint
runpodctl serverless update <id>      # update endpoint
runpodctl serverless delete <id>      # delete endpoint
```

other resources: `template` (alias: `tpl`), `volume` (alias: `vol`), `registry` (alias: `reg`)

### file transfer

send and receive files without api key using croc:

```bash
# send a file
runpodctl send data.txt
# output: code is: 8338-galileo-collect-fidel

# receive on another computer
runpodctl receive 8338-galileo-collect-fidel
```

## output format

default output is json (optimized for agents). use `--output` flag for alternatives:

```bash
runpodctl pod list                    # json (default)
runpodctl pod list --output=table     # human-readable table
runpodctl pod list --output=yaml      # yaml format
```

## legacy commands

legacy commands are still supported but deprecated. please update your scripts:

`get pod`, `create pod`, `remove pod`, `start pod`, `stop pod`

## acknowledgements

- [cobra](https://github.com/spf13/cobra)
- [croc](https://github.com/schollz/croc)
- [golang](https://go.dev/)
- [viper](https://github.com/spf13/viper)
