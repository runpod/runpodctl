# runpodctl
runpodctl is a CLI tool to automate / manage GPU pods for [runpod.io](https://runpod.io).

<br />

# install linux/osx command line
get latest binary from [releases](https://github.com/Run-Pod/runpodctl/releases)

```
wget --quiet --show-progress https://github.com/Run-Pod/runpodctl/releases/download/v1.6.1/runpodctl-linux-amd -O runpodctl
chmod +x runpodctl
cp runpodctl /usr/bin/runpodctl
```

# download with windows powershell

```
wget https://github.com/runpod/runpodctl/releases/download/v1.6.1/runpodctl-win-amd -O runpodctl.exe
```

*Note that you must either put runpoodctl.exe in your PATH or navigate to it and use ./runpodctl.exe (instead of just runpodctl as in linux)*

<br />

# how to transfer data
Using send or receive command does not require API keys due to built-in security of one-time codes.

Send a file or folder
```
runpodctl send data.txt

Sending 'data.txt' (5 B)
Code is: 8338-galileo-collect-fidel
On the other computer run

runpodctl receive 8338-galileo-collect-fidel
```

Receive on any other computer or pod
```
runpodctl receive 8338-galileo-collect-fidel
Receiving 'data.txt' (5 B)

Receiving (<-149.36.0.243:8692)
data.txt 100% |████████████████████| ( 5/ 5B, 0.040 kB/s)
```

<br />

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
