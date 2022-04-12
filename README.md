# runpodctl
runpodctl is a CLI tool to automate / manage GPU pods for [rundpod.io](https://runpod.io).

# install
get latest binary from [releases](https://github.com/Run-Pod/runpodctl/releases)

# how to use
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
