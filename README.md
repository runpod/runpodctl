# runpodctl
runpodctl is a CLI tool to automate / manage GPU pods for [runpod.io](https://runpod.io).

**Please note that there is a breaking change in 1.8.0 that is incompatible with previous versions. You can check your version by typing**
```
runpodctl version
```

All pods automatically come with runpodctl installed with a pod-scoped API key!

## Tutorial
A very detailed tutorial that shows how to utilize runpodctl in multiple situations: https://www.youtube.com/watch?v=QN1vdGhjcRc


## install linux/osx command line
get latest binary from [releases](https://github.com/Run-Pod/runpodctl/releases)

linux
```
wget --quiet --show-progress https://github.com/Run-Pod/runpodctl/releases/download/v1.9.0/runpodctl-linux-amd -O runpodctl && chmod +x runpodctl && sudo cp runpodctl /usr/bin/runpodctl
```

osx (ARM)
```
wget --quiet --show-progress https://github.com/runpod/runpodctl/releases/download/v1.9.0/runpodctl-darwin-arm -O runpodctl && chmod +x runpodctl && sudo mv runpodctl /usr/local/bin/runpodctl
```

![](https://github.com/runpod/runpodctl/blob/main/runpodctllinux.gif)

## how to transfer data
Using send or receive command does not require API keys due to built-in security of one-time codes.

Run the following on the computer that has the file you want to send
```
runpodctl send data.txt
```

The command should output something like
```
Sending 'data.txt' (5 B)
Code is: 8338-galileo-collect-fidel
On the other computer run

runpodctl receive 8338-galileo-collect-fidel
```

Run the following on the computer that you want to send the file to
```
runpodctl receive 8338-galileo-collect-fidel
```

It should start transferring with output that looks like
```
Receiving 'data.txt' (5 B)

Receiving (<-149.36.0.243:8692)
data.txt 100% |████████████████████| ( 5/ 5B, 0.040 kB/s)
```

<br />

## download with windows powershell

```
wget https://github.com/runpod/runpodctl/releases/download/v1.9.0/runpodctl-win-amd -O runpodctl.exe
```

![](https://github.com/runpod/runpodctl/blob/main/runpodctlwindows.gif)

<br />

## how to transfer data
Using send or receive command does not require API keys due to built-in security of one-time codes.

Run the following on the computer that has the file you want to send
```
./runpodctl.exe send data.txt
```

The command should output something like
```
Sending 'data.txt' (5 B)
Code is: 8338-galileo-collect-fidel
On the other computer run

runpodctl receive 8338-galileo-collect-fidel
```

Run the following on the computer that you want to send the file to
```
runpodctl receive 8338-galileo-collect-fidel
```

It should start transferring with output that looks like
```
Receiving 'data.txt' (5 B)

Receiving (<-149.36.0.243:8692)
data.txt 100% |████████████████████| ( 5/ 5B, 0.040 kB/s)
```

<br />

# Using google drive

You can use the following links for google colab

[Send](https://colab.research.google.com/drive/1UaODD9iGswnKF7SZfsvwHDGWWwLziOsr#scrollTo=2nlcIAY3gGLt)

[Receive](https://colab.research.google.com/drive/1ot8pODgystx1D6_zvsALDSvjACBF1cj6#scrollTo=RF1bMqhBOpSZ)

# how to manage pods
Visit [docs](doc/runpodctl.md) for details of all commands.

First configure the API key. You can get API key from [runpod](https://runpod.io/client/settings).
```
runpodctl config --apiKey={key}
```
Get all pods:
```
runpodctl get pod
```
Get a pod:
```
runpodctl get pod {podId}
```
Start an ondemand pod.
```
runpodctl start pod {podId}
```
Start a spot pod with bid. The bid price you set is the price you will pay if not outbid:
```
runpodctl start pod {podId} --bid=0.3
```
Stop a pod:
```
runpodctl stop pod {podId}
```

<br />
<br />

# thanks to
- [cobra](https://github.com/spf13/cobra)
- [croc](https://github.com/schollz/croc)
- [golang](https://go.dev/)
- [nebula](https://github.com/slackhq/nebula)
- [viper](https://github.com/spf13/viper)
