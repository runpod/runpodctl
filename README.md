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
      - [conda, mamba, pixi (conda-forge)](#conda-mamba-pixi-conda-forge)
  - [quick start](#quick-start)
  - [commands](#commands)
    - [pod management](#pod-management)
    - [serverless endpoints](#serverless-endpoints)
    - [waiting until a resource is usable](#waiting-until-a-resource-is-usable)
      - [invoking an endpoint](#invoking-an-endpoint)
    - [file transfer](#file-transfer)
  - [output format](#output-format)
    - [pod runtime status](#pod-runtime-status)
    - [error format](#error-format)
  - [environment variables](#environment-variables)
  - [legacy commands](#legacy-commands)
  - [release process](#release-process)
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

#### conda, mamba, pixi (conda-forge)

runpodctl is available on [conda-forge](https://anaconda.org/conda-forge/runpodctl).

```bash
# conda
conda install conda-forge::runpodctl

# mamba
mamba install conda-forge::runpodctl

# pixi (user-global install)
pixi global install runpodctl
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
runpodctl pod create --image=runpod/pytorch:2.8.0-py3.11-cuda12.8.1-cudnn-devel-ubuntu22.04 --gpu-id=NVIDIA_A100

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
runpodctl pod logs <id>               # read a pod's logs (--follow to stream)
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
runpodctl serverless health <id>      # worker and job counts for an endpoint
runpodctl serverless logs <id>        # read worker logs (--follow to stream)
runpodctl serverless run <id>         # invoke an endpoint and wait for the result
runpodctl serverless status <id> <job-id>  # check a job submitted earlier
```

#### reading logs

`pod logs` and `serverless logs` stream from rest v2 (`api.runpod.io/v2`), which
is a different host from the rest v1 control plane the crud commands use. output
is json lines — one `{source,line,ts}` object per line, plus `workerId` on
serverless — so it pipes into `jq` or an agent without further parsing.

```bash
runpodctl pod logs <id> --tail 50            # replay recent lines and exit
runpodctl pod logs <id> --follow             # stream until interrupted
runpodctl pod logs <id> --since 30m          # a time window instead of a line count
runpodctl pod logs <id> --source system      # platform lines only
runpodctl serverless logs <id>               # every worker, tagged with workerId
runpodctl serverless logs <id> --worker <wid>  # one worker
```

behavior worth knowing, all verified against prod:

- `source` is `container` (your workload's own output) or `system` (the platform
  narrating image pull, container create/start/stop). a deploy that never comes up
  usually explains itself in the system lines.
- `--tail` is applied **per source**, so `--tail 5` on a pod emitting both kinds
  returns up to 5 container lines *and* up to 5 system lines.
- `--since` accepts a duration (`30m`, `2h`, `7d`) or an rfc3339 timestamp, and
  overrides `--tail` server-side.
- logs outlive the workload: a stopped pod still returns its history, including
  the `stop container` lines. this is the post-mortem case, and it works.
- the stream never ends on its own, so without `--follow` the command returns once
  the replayed lines stop arriving (or after `--max-wait`, default 5s).
- the api sends nothing at all — not even response headers — until it has a line
  to send, so a filter that matches nothing (`--since` past the last log, a
  `--source` with no output) ends in a `timeout` rather than an empty result. that
  is reported as an error on purpose: exiting 0 with no output would be
  indistinguishable from a workload that genuinely logged nothing.
- `--follow` reconnects on its own if the connection drops, resuming from the last
  line it printed rather than replaying `--tail` again. reconnect notes go to
  stderr; stdout stays pure json lines.
- `serverless logs` without `--worker` resolves the endpoint's workers and reads
  them all concurrently. the worker set is resolved once at start, so a worker
  that appears later needs the command re-run.
- `--tail` is **per worker as well as per source**, so it multiplies: the default
  `--tail 100` across 10 workers writing both sources can replay a few thousand
  interleaved lines before the live output starts. narrow it with `--worker`, a
  smaller `--tail`, or `--tail 0` for live output only. at most 32 workers are read
  at once, and the cap is reported on stderr.

#### invoking an endpoint

`run` and `health` call the invoke api (`api.runpod.ai/v2`) with the api key you
already configured, so there is no need to hand-build a curl request:

```bash
# invoke and wait for the result
runpodctl serverless run <id> --input '{"prompt":"hello"}'

# big payloads: skip shell quoting
runpodctl serverless run <id> --input-file payload.json
cat payload.json | runpodctl serverless run <id> --input -

# give a cold or slow endpoint longer
runpodctl serverless run <id> --input '{}' --wait 15m

# submit and get the job id back immediately
runpodctl serverless run <id> --input '{}' --no-wait
runpodctl serverless status <id> <job-id> --wait 5m
```

the payload must be a json object and is sent as `{"input": <your json>}`; pass
only the handler payload. it is parsed and size-checked locally first, so a
quoting mistake, or a request body over the invoke api's 10 MiB `/run` limit,
fails with `usage_error` before anything is uploaded. the size checked is the body
the cli actually sends — the payload compacted and json-escaped inside
`{"input": ...}` — so whitespace in an `--input-file` does not count against the
limit and an `&`-heavy payload (six bytes escaped) does.

| behavior | detail |
| --- | --- |
| waiting | `run` submits on `/run` and polls `/status` until the job is terminal, bounded by `--wait` (default 5m) |
| `--no-wait` | submits and prints the queued job without polling (same as `--wait 0`, exit 0). follow it with `serverless status`. passing an explicit `--wait` alongside it is a `usage_error`, not a silently ignored flag |
| stdout | always the job payload as json — including a `FAILED` job's `error`, and the last known payload when the wait ran out |
| stderr | progress notes and the error object, never job data |
| exit codes | 0 when the job is `COMPLETED`, and when `--wait 0` / `--no-wait` submitted it successfully. 1 when the request fails, when `--wait` runs out (`timeout`), or when the job ends `FAILED` / `CANCELLED` / `TIMED_OUT` (`job_failed`) |

`timeout` means the cli stopped waiting, not that the endpoint is broken. when the
message names a `serverless status` command the job is still running server-side —
poll it with that command rather than re-invoking. when it does not (a single api
call ran out of time), nothing was left running and a retry is fine.

two budgets, two levers: `--wait` bounds the whole job, and the shared `timeout`
config key (30s by default) bounds one api call. a call made inside a wait is
clamped to what is left of `--wait`, except that no single call is ever given less
than 1s — a request with milliseconds left is doomed before it is sent, and
clamping it would throw away a terminal status one round trip away. so a `--wait`
below one second may overshoot by up to that much, and no more.

`/runsync` is deliberately not used, even though `serverless get` still reports
its url. it is not synchronous: the invoke api holds the connection for 90s and
then answers with a still-running job, so the cli has to poll `/status` either
way. Submitting there also has two failure modes `/run` does not — until the
response arrives there is no job id, so a slow response strands a billed job that
cannot be polled at all, and a job submitted on `/runsync` has its result
discarded 1 minute after it completes instead of 30 minutes. `/run` costs one
extra round trip and avoids both.

other resources: `template` (alias: `tpl`), `volume` (alias: `vol`), `registry` (alias: `reg`)

### waiting until a resource is usable

create returns as soon as the resource is *scheduled*: a pod reports
`desiredStatus: RUNNING` while its image is still being pulled. `--wait` blocks
until it is actually usable instead, so there is no poll loop to write.

```bash
# returns when ssh answers, not when the pod is scheduled
runpodctl pod create --image <img> --gpu-id "NVIDIA GeForce RTX 4090" --wait

# returns when the endpoint's health reports a ready or running worker
runpodctl serverless create --template-id <id> --workers-min 1 --wait

# give up sooner than the 10m default
runpodctl pod create --image <img> --gpu-id <id> --wait --wait-timeout 3m
```

what each one waits for, precisely:

| command | ready means |
| --- | --- |
| `pod create --wait` | the pod's public port 22 accepts a tcp connection **and** answers with an ssh protocol banner. no key and no handshake, so it works before `runpodctl doctor` has ever run — it proves sshd is up, not that your key is installed. port 22 merely *appearing* in `runtime.ports` is not enough: prod allocates that port for images that run no sshd at all |
| `serverless create --wait` | the endpoint's `/health` reports at least one worker `ready` or `running`. neither counter is quite "a hot handler", and `/health` exposes no stronger one: a `ready` worker is flashboot-cached (its record reads `desiredStatus: EXITED`), so the first request resumes it, and `running` is written when a worker is *scheduled*, before its container exists. `running` still has to count, because a `--workers-min` worker stays `RUNNING` for its whole life and never appears in `ready` |

- progress goes to **stderr** on a 15s cadence; stdout stays exactly one json
  object, so `... 2>/dev/null | jq` sees a single payload.
- `pod create --wait` prints the same shape as `pod get` (which includes the live
  `ssh` block) rather than the create response, which has no ssh info.
  `serverless create --wait` prints the same create response as without `--wait`.
- `--wait-timeout` defaults to `10m` and accepts `90s`, `10m`, `1h`, `2d`.
- on timeout or ctrl-c the resource is **not** deleted — you paid for it, and you
  need the id to debug or clean up. the exit code is non-zero and the error carries
  the id, the last known state and the delete command, with code `wait_timeout` /
  `wait_interrupted`. (the one error without a last-known-state is a ctrl-c during
  the `pod create --wait` re-read, which happens after ssh already answered.)
- a second ctrl-c always exits, even while the first one is still producing that
  error object: the read that follows a successful wait is not cancellable, so the
  signal handler is released as soon as the first signal arrives.
- `serverless create --wait` warns at `--workers-min 0` but still waits. runpod
  does fill a standby pool at 0 min workers whenever `--workers-max` is above 1,
  and `/health` counts those cached workers as `ready` — it is just slower and less
  certain than `--workers-min 1`, which starts (and bills for) a worker right away.
- `pod create --wait` needs ssh, so it cannot be combined with `--ssh=false`. two
  combinations warn on stderr instead of failing, because they are satisfiable but
  often are not:
  - `--compute-type CPU` — cpu pods are created over rest, which cannot request
    runpod-managed ssh, so only an image that starts its own sshd becomes
    reachable.
  - `--cloud-type COMMUNITY` without `--public-ip` — community cloud only maps a
    public ssh port on a machine that has a public ip, and `--public-ip` is what
    asks the scheduler for one.
- a transient api failure during a wait does **not** end it: the poll error is
  reported as the current state and polling continues to the deadline, because the
  resource already exists and bills. only a failure that cannot resolve stops the
  wait early:
  - bad credentials or a rejected request — http `400`/`401`/`403`, or the codes
    `unauthorized` / `forbidden` / `no_credentials` / `bad_request`. the pod wait
    reads graphql, whose failures all carry the code `graphql_error`, so there the
    decision is made on the http status and the emitted code stays `graphql_error`.
  - a pod in a terminal state (`conflict`), or a resource that two consecutive
    reads no longer list (`not_found` — one missing read is treated as an unknown
    state, not a deletion).
  - a resource that has never been readable *at all* after twelve consecutive
    reads — ~1 min at the default 5s interval, so only reachable when
    `--wait-timeout` is longer than that: `not_found`. an endpoint that never
    propagated to `/health`, or a pod terminated before it was ever listed, is no
    longer propagation lag, and waiting out the full budget would report it as a
    `wait_timeout` worth retrying.
  - `404`, `429` and `5xx` stay transient on purpose within those bounds:
    `/health` 404s an endpoint id the invoke service has not propagated yet.

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

### pod runtime status

`desiredStatus` says what you asked for, never what is happening: a pod whose
20 gb image is still downloading and a pod that has been serving for an hour are
both `RUNNING`. `pod get` and `pod list` therefore also report a derived
`runtimeStatus`, plus a `runtimeStatusReason` token when there is more to say.
branch on these, not on the reason text.

| `runtimeStatus` | meaning |
| --- | --- |
| `running` | `desiredStatus` is RUNNING and the platform reports runtime telemetry: the container is up. does **not** imply any port is reachable |
| `initializing` | `desiredStatus` is RUNNING and no telemetry is being reported. usually placed on a machine with the container not up yet — image pull, container create or boot, which the platform does not distinguish — but the same absence is what an upstream telemetry lookup failure looks like, so read it as "no container reported", not "the container is provably down". either way: keep polling |
| `stopped` | `desiredStatus` is EXITED and `lastStatusChange` does not name a termination. container gone, disk kept, `pod start` will bring it back |
| `terminated` | the pod is being destroyed: `desiredStatus` is TERMINATED, **or** it is EXITED and `lastStatusChange` says "terminated by ...". the second is the normal case — a terminate writes EXITED, not TERMINATED — and a terminated pod drops out of `pod list` shortly after, so this is a narrow window |
| `unknown` | not derivable: either a `desiredStatus` the platform defines but does not surface in practice (CREATED, RESTARTING, PAUSED, DEAD), or the runtime lookup failed. read `desiredStatus`, which is in the same output |

| `runtimeStatusReason` | meaning |
| --- | --- |
| `awaiting_container` | with `initializing`: no container is being reported for a pod that should be running |
| `stopped_by_user` / `terminated_by_user` | you did it |
| `stopped_by_runpod` / `terminated_by_runpod` | runpod did it. the platform records no machine-readable cause; in practice this is insufficient credit, a fatal image-pull failure, or host action |
| `stopped_outbid` / `terminated_outbid` | a spot/community pod lost its machine to a higher bid. the only involuntary stop with a real recorded cause; retry elsewhere or at on-demand pricing |
| `runtime_unavailable` | with `unknown`: the runtime lookup could not be made, so running and initializing cannot be told apart |

the token is a lossy read of the backend's free-text `lastStatusChange`, which
`pod get` and `pod list` both also publish — so a phrasing this cli does not
recognise leaves `runtimeStatusReason` absent rather than wrong, and the raw text
is still there.

`runtimeStatus` is derived from the graphql snapshot the runtime telemetry came
from, while `desiredStatus` is rest's. the two surfaces can briefly disagree, so
a single `pod get` can show `desiredStatus: RUNNING` next to
`runtimeStatus: stopped`. that is deliberate: gating telemetry on the *other*
surface's status is how a stopped pod's stale ports get handed back as a working
ssh command. when they disagree, trust `runtimeStatus`.

there is deliberately no `pulling` value. the api exposes no pull state (see
`internal/podstate` for the full trace of what it does expose), so `pulling`
could only ever be a guess, and `initializing` covers the whole pre-container
window honestly instead.

`uptimeSeconds` is the container's uptime and is absent whenever no container is
reporting, rather than being published as `0`.

to poll a pod to readiness, wait for `runtimeStatus: running`; for ssh, wait for
`ssh.ssh_command` to appear in `pod get`. a running container still needs a
publicly routable port 22, and when it has none `ssh.error` says which case it
is: the pod never asked for `22/tcp`, the host has not published the mapping yet,
or the mapping exists but is not publicly routable on that machine (no public ip
— nothing you can change on the pod).

in the first case the message hands you a `pod update ... --ports` command with
your existing ports already in it, because **`--ports` replaces the pod's whole
port list** rather than adding to it (unlike `--env`, which merges). changing the
port list also bumps the pod's version, which can restart the container, so
processes and container-local state outside the volume may not survive it.

`pod list` gets its telemetry from one bulk graphql call regardless of pod count,
never one per pod. it is skipped entirely when no listed pod is `RUNNING` (a
stopped pod's telemetry is stale and never consulted anyway). that call is
best-effort and capped at 5s: if it fails, every pod comes back `unknown` /
`runtime_unavailable` and the list still succeeds.

### error format

data goes to stdout; errors go to stderr as a single flat json object, and the
exit code is non-zero. branch on `code`, never on the message text:

```jsonc
{"error":"failed to get endpoint: endpoint not found","code":"not_found","status":404}
```

| field | notes |
| --- | --- |
| `error` | human-readable message, unwrapped (never a nested json blob) |
| `code` | stable, lowercase. present on every error from the resource commands (see the caveat below) |
| `status` | http status, **only** when the failure came back from a rest call |
| `id` | id of a resource the failure left behind, **only** when one exists — a `pod create --wait` that timed out has already bought a pod, and this is how you find it without parsing the message |

`status` is deliberately absent when the api answered 200 with an empty result
(graphql reports a missing resource that way), so `code` is the field to branch
on — `if status == 404` misses every graphql not-found.

codes the cli generates:

| code | meaning |
| --- | --- |
| `usage_error` | your invocation was wrong (unknown command/flag, bad or missing args, missing required flags). usage text is printed after the json |
| `not_found` | the api has no such resource. during a `--wait` it can also mean a resource that *was* created has gone (or never became visible), so check for an `id` field and clean up rather than assuming nothing exists |
| `bad_request` `unauthorized` `forbidden` `conflict` `rate_limited` `server_error` `api_error` | derived from the rest status |
| `graphql_error` | graphql returned an errors array (http 200) |
| `timeout` | the cli stopped waiting. two cases, told apart by the message: a `--wait` / `--wait-for-hash` budget ran out with the work still running server-side (the message names the command to poll it — do that, do not re-invoke), or a single api call exceeded the `timeout` config key (nothing is running; retry) |
| `job_failed` | a serverless job reached a terminal status other than `COMPLETED`. the job payload is still on stdout |
| `no_credentials` | no api key configured — run `runpodctl doctor` or set `RUNPOD_API_KEY` |
| `network_error` | the api could not be reached at all — dns, refused, tls, timeout. transient: retry |
| `wait_timeout` | `--wait` gave up before the resource was usable. the resource **was created and still bills** — the last known state is in the message and the id is in `id` |
| `wait_interrupted` | `--wait` was cancelled (ctrl-c / SIGTERM). same as above: the resource exists, and `id` names it |
| `cli_error` | anything else local: validation, config, bad input (including a malformed `RUNPOD_API_URL`) |

the api may also return its own code, which is passed through lowercased, so
treat the list as the set the cli generates rather than an exhaustive one.

**known gaps.** the json error shape covers `pod`, `serverless`, `template`,
`volume`, `registry`, `gpu`, `datacenter`, `billing`, `user`, `model`, `ssh`,
`send`, `receive`, `hub` and `update`. these still print plaintext on stderr and
carry no `code`:

| surface | shape |
| --- | --- |
| legacy `get/create/remove/start/stop pod`, `create/remove pods`, `get cloud` | `Error: <msg>` via cobra, exit 1 |
| `exec` | plaintext, exit 1 |
| `project` | prints to **stdout** and exits 0 (bug, tracked as CON-816) |

so a parser should tolerate a non-json line on stderr from those, and must not
rely on the exit code for `project` until CON-816 lands.

## environment variables

| variable | default | what it sets |
| --- | --- | --- |
| `RUNPOD_API_KEY` | — | api key. also settable via `runpodctl doctor` or `~/.runpod/config.toml` |
| `RUNPOD_API_URL` | `https://rest.runpod.io/v1` | rest control plane (config key `restApiUrl`) |
| `RUNPOD_GRAPHQL_URL` | `https://api.runpod.io/graphql` | graphql control plane (config key `apiUrl`) |
| `RUNPOD_INVOKE_URL` | `https://api.runpod.ai/v2` | base for the serverless invoke urls reported by `serverless create/get/list/update`, and the host `serverless run/status/health` call (config key `invokeUrl`) |
| `RUNPOD_REST_V2_URL` | `https://api.runpod.io/v2` | rest v2, which serves `pod logs`, `serverless logs` and the worker listing behind them (config key `restV2ApiUrl`) |

invoke is a separate service from the control plane: pointing `RUNPOD_API_URL`
or `RUNPOD_GRAPHQL_URL` at a non-prod host does **not** move the invoke urls.
override `RUNPOD_INVOKE_URL` explicitly when you need that. rest v2 is separate
again — the crud commands are still on rest v1, so moving one does not move the
other.

## legacy commands

legacy commands are still supported but deprecated. please update your scripts:

`get pod`, `create pod`, `remove pod`, `start pod`, `stop pod`

## release process

releases are fully automated by [goreleaser](https://goreleaser.com/) via the `release` github action ([.github/workflows/release.yml](.github/workflows/release.yml)). to cut a release:

1. make sure `main` is green and holds everything you want to ship.
2. create and push a `v*` semver tag on the commit to release:

   ```bash
   git tag v2.7.0
   git push origin v2.7.0
   ```

3. the tag push triggers the `release` workflow, which runs `goreleaser release --clean` and:
   - builds binaries for darwin/linux/windows (amd64/arm64), incl. raw `runpodctl-{os}-{arch}` binaries for the legacy self-update command and a upx-compressed linux/amd64 build.
   - signs and notarizes the darwin universal binary with [quill](https://github.com/anchore/quill), after the per-arch builds are merged into the fat binary and before archiving, so the release checksums and the homebrew formula/cask hashes all cover the signed artifact. the raw per-arch `runpodctl-darwin-{arch}` binaries are **not** signed; only pre-signing runpodctl versions fetch those.
   - creates the github release with archives + checksums (prereleases are auto-detected from the tag).
   - opens pull requests on [runpod/homebrew-runpodctl](https://github.com/runpod/homebrew-runpodctl) for the homebrew formula and cask.

4. **merge the homebrew pull requests.** the tap lives in a separate repo, so the release is not complete for `brew install` users until those prs are merged. (they cannot be combined into this repo.)

notes:

- the workflow can also be run manually via `workflow_dispatch` (re-runs goreleaser against the current ref).
- running goreleaser locally needs `quill` on your `PATH` (see [quill's install instructions](https://github.com/anchore/quill#installation); the workflow's install step is pinned to linux/amd64, so copy the version but not the tarball name), because the darwin signing hook also runs for snapshots. snapshots sign ad-hoc and skip the notary submission, so no apple credentials are needed:

  ```bash
  goreleaser release --snapshot --clean
  ```

- signing/notarization uses the `QUILL_SIGN_P12`, `QUILL_SIGN_PASSWORD`, `QUILL_NOTARY_KEY`, `QUILL_NOTARY_KEY_ID` and `QUILL_NOTARY_ISSUER` secrets. a signing or notary failure fails the whole release before anything is published.
- quill waits up to 15 minutes (900s) for apple's verdict and then fails. that timeout cannot be raised much: quill mints a single app store connect token with `exp = timeout + 2m` and never refreshes it, and apple rejects notary tokens whose lifetime exceeds 20 minutes — so anything above 1080s (18m) is a guaranteed 401. if apple is slow enough to trip the timeout, re-run the workflow; quill has no resume, so it re-signs and resubmits from scratch (each signing run embeds a fresh apple timestamp, so the hash differs every run).
- the signed binary is not stapled (quill cannot staple a bare mach-o). that only matters for downloads that carry the quarantine bit (a browser download, or `brew install --cask`): there gatekeeper does an online notarization check on first run. a `curl` download or the homebrew formula sets no quarantine bit, so gatekeeper never checks.
- the homebrew prs are authored by a github app; tap auth uses the `RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY` secrets, and goreleaser pushes via the generated `HOMEBREW_TAP_TOKEN`.
- conda-forge is updated separately by the conda-forge feedstock bot, not by this workflow.

## acknowledgements

- [cobra](https://github.com/spf13/cobra)
- [croc](https://github.com/schollz/croc)
- [golang](https://go.dev/)
- [viper](https://github.com/spf13/viper)
