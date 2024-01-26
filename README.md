<div align="center">

# RunPod CLI

The CLI tool to automate / manage GPU pods for [runpod.io](https://runpod.io).

*Note: All pods automatically come with runpod cli installed with a pod-scoped API key.*

</div>

## Table of Contents

- [RunPod CLI](#runpod-cli)
  - [Table of Contents](#table-of-contents)
  - [Get Started](#get-started)
    - [Install (linux/osx)](#install-linuxosx)
    - [Install (Windows PowerShell)](#install-windows-powershell)
  - [Tutorial](#tutorial)
  - [Transferring Data (file send/receive)](#transferring-data-file-sendreceive)
    - [To send a file](#to-send-a-file)
    - [To receive a file](#to-receive-a-file)
    - [Using Google Drive](#using-google-drive)
  - [Pod Commands](#pod-commands)
  - [Dockerless](#dockerless)
  - [Acknowledgements](#acknowledgements)

## Get Started

### Install (linux/osx)

```bash
# Download and install via wget
wget cli.runpod.io | sudo bash
```

```bash
# Using homebrew
brew tap runpod/runpod
brew install runpod
```

### Install (Windows PowerShell)

```powershell
wget https://github.com/runpod/runpodctl/releases/download/v1.9.0/runpodctl-win-amd -O runpodctl.exe
```

## Tutorial

Please checkout this [video tutorial](https://www.youtube.com/watch?v=QN1vdGhjcRc) for a detailed walkthrough of runpod cli.

**Video Chapters:**

- [Installing the latest version of RunPod CLI](https://www.youtube.com/watch?v=QN1vdGhjcRc&t=1384s)
- [Uploading large datasets](https://www.youtube.com/watch?v=QN1vdGhjcRc&t=2068s)
- [File transfers from PC to RunPod](https://www.youtube.com/watch?v=QN1vdGhjcRc&t=2106s)
- [Downloading folders from RunPod](https://www.youtube.com/watch?v=QN1vdGhjcRc&t=2549s)
- [Adding RunPod CLI to your environment path](https://www.youtube.com/watch?v=QN1vdGhjcRc&t=2589s)
- [Downloading model files](https://www.youtube.com/watch?v=QN1vdGhjcRc&t=4871s)

## Transferring Data (file send/receive)

**Note:** The `send` and `receive` commands do not require API keys due to the built-in security of one-time codes.

Run the following on the computer that has the file you want to send

### To send a file

```bash
runpodctl send data.txt
```

*Example output:*

 ```bash
Sending 'data.txt' (5 B)
Code is: 8338-galileo-collect-fidel
On the other computer run

runpodctl receive 8338-galileo-collect-fidel
```

### To receive a file

```bash
runpodctl receive 8338-galileo-collect-fidel
```

*Example output:*

```bash
Receiving 'data.txt' (5 B)

Receiving (<-149.36.0.243:8692)
data.txt 100% |████████████████████| ( 5/ 5B, 0.040 kB/s)
```

### Using Google Drive

You can use the following links for google colab

[Send](https://colab.research.google.com/drive/1UaODD9iGswnKF7SZfsvwHDGWWwLziOsr#scrollTo=2nlcIAY3gGLt)

[Receive](https://colab.research.google.com/drive/1ot8pODgystx1D6_zvsALDSvjACBF1cj6#scrollTo=RF1bMqhBOpSZ)

## Pod Commands

Before using pod commands, configure the API key obtained from your [RunPod account](https://runpod.io/console/user/settings).

```bash
# configure API key
runpodctl config --apiKey={key}

# Get all pods
runpodctl get pod

# Get a pod
runpodctl get pod {podId}

# Start an ondemand pod.
runpodctl start pod {podId}

# Start a spot pod with bid.
# The bid price you set is the price you will pay if not outbid:
runpodctl start pod {podId} --bid=0.3

# Stop a pod
runpodctl stop pod {podId}
```

For a comprehensive list of commands, visit [RunPod CLI documentation](doc/runpodctl.md).

## Dockerless

We have introduced the ability for users to develop locally and run remotely though our CLI development tool.

## Acknowledgements

- [cobra](https://github.com/spf13/cobra)
- [croc](https://github.com/schollz/croc)
- [golang](https://go.dev/)
- [nebula](https://github.com/slackhq/nebula)
- [viper](https://github.com/spf13/viper)
