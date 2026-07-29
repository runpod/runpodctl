// Package podstate derives a pod's observable runtime state from the signals
// the platform actually exposes, and nothing more.
//
// # Why this exists
//
// `desiredStatus` answers "what did you ask for", never "what is happening".
// A pod that has been RUNNING for 30 seconds and a pod whose 20 GB image is
// still downloading are both `desiredStatus: RUNNING`, so `pod get` could only
// ever say "pod not ready" with no reason (CON-690).
//
// # What the platform actually exposes
//
// Verified against prod graphql (introspection is disabled there; fields were
// probed by name and confirmed against runpod-backend's schema) and against a
// live pod's full create -> running -> stopped timeline:
//
//   - `Pod.desiredStatus` is the PodStatus enum: CREATED, RUNNING, RESTARTING,
//     EXITED, PAUSED, DEAD, TERMINATED.
//   - `Pod.runtime` (type PodRuntime) carries only container/gpu utilisation,
//     ports and uptimeInSeconds. There is no pulling/starting/ready enum, and
//     no `status`, `phase`, `state` or `pullStatus` field anywhere on Pod or
//     PodRuntime. It resolves by GET hapi.runpod.net/v1/internal/pod/{id} with
//     a 2s timeout and returns null on any error, 404 included. So
//     "runtime == null" means "the host daemon is not reporting a container for
//     this pod yet" — that absence is the only start-up signal there is.
//   - The host daemon does track image pulls internally, but the response it
//     serves for a pod is mapped down to stats/gpus/ports/uptime before it
//     reaches graphql, so the pull state never leaves the machine.
//   - `PodStopReason` (IMAGE_AUTH_ERROR, IMAGE_NOT_FOUND, IMAGE_PULL_ERROR,
//     CUDA_VERSION_MISMATCH) exists, but only as an *input* to the podStop
//     mutation. It is not persisted and not queryable: it is used to send the
//     owner an email and then dropped. `lastStatusChange` is overwritten with a
//     bare "Exited by Runpod: <date>" that names no reason.
//   - REST /pods and /pods/{id} never return `runtime` at all. They do return
//     `portMappings` and `publicIp`, but a pod that declares no ports never
//     gets `portMappings`, so port presence cannot stand in for liveness.
//
// # Consequence for the vocabulary
//
// There is therefore no honest way to distinguish "pulling the image" from
// "creating the container" from "booting". `initializing` deliberately covers
// all three rather than inventing a `pulling` value that would be a guess.
// Likewise there is no terminal `image_pull_error` value: the reason a
// Runpod-initiated stop happened is not exposed, only that Runpod (rather than
// the user) did it, which is what `stopped_by_runpod` says and no more.
package podstate

import "strings"

// Status is the small, stable vocabulary for a pod's observable runtime state.
// Values are lowercase and safe to branch on.
type Status string

const (
	// StatusRunning means desiredStatus is RUNNING and the platform is
	// reporting runtime telemetry for the pod: the container is up. It does
	// NOT imply any port is reachable — see SSHUnavailableReason.
	StatusRunning Status = "running"

	// StatusInitializing means desiredStatus is RUNNING but the platform
	// reports no runtime telemetry yet. The pod is placed and the container is
	// not up. This covers image pull, container create and container boot: the
	// platform does not distinguish them.
	StatusInitializing Status = "initializing"

	// StatusStopped means desiredStatus is EXITED. The container is gone; the
	// pod's disk survives and it can be started again.
	StatusStopped Status = "stopped"

	// StatusTerminated means desiredStatus is TERMINATED. The pod is being or
	// has been destroyed.
	StatusTerminated Status = "terminated"

	// StatusUnknown means the state cannot be derived: either desiredStatus is
	// one of the values the platform defines but never surfaces in practice
	// (CREATED, RESTARTING, PAUSED, DEAD), or runtime telemetry could not be
	// looked up. Read desiredStatus, which is always in the same output.
	StatusUnknown Status = "unknown"
)

// Reason is a stable lowercase token explaining a Status. It is empty when
// there is nothing to add (a plainly running pod), and callers should omit the
// field in that case.
type Reason string

const (
	// ReasonAwaitingContainer accompanies StatusInitializing: the pod is
	// placed on a machine but the host daemon reports no container. Could be
	// image pull, container create or boot — indistinguishable from outside.
	ReasonAwaitingContainer Reason = "awaiting_container"

	// ReasonStoppedByUser means lastStatusChange attributes the stop to the
	// account owner (an explicit `pod stop`).
	ReasonStoppedByUser Reason = "stopped_by_user"

	// ReasonStoppedByRunpod means lastStatusChange attributes the stop to
	// Runpod. The platform records no machine-readable cause; in practice this
	// is insufficient credit, a fatal image-pull failure, or host action.
	ReasonStoppedByRunpod Reason = "stopped_by_runpod"

	// ReasonTerminatedByUser means lastStatusChange attributes the
	// termination to the account owner.
	ReasonTerminatedByUser Reason = "terminated_by_user"

	// ReasonTerminatedByRunpod means lastStatusChange attributes the
	// termination to Runpod.
	ReasonTerminatedByRunpod Reason = "terminated_by_runpod"

	// ReasonRuntimeUnavailable accompanies StatusUnknown for a RUNNING pod:
	// the runtime lookup was not made or failed, so running cannot be told
	// apart from initializing. This is a gap in our knowledge, not in the pod.
	ReasonRuntimeUnavailable Reason = "runtime_unavailable"
)

// Signals is every input Derive looks at. Everything here is a field the api
// really returns; nothing is inferred upstream of this struct.
type Signals struct {
	// DesiredStatus is Pod.desiredStatus, matched case-insensitively.
	DesiredStatus string

	// LastStatusChange is Pod.lastStatusChange. It is free text written by the
	// backend and typed as interface{} on the api structs, so it is accepted
	// as any and coerced here.
	LastStatusChange any

	// RuntimeProbed reports whether runtime telemetry was actually looked up.
	// False means "we did not or could not ask", which is NOT the same as "the
	// container is down".
	RuntimeProbed bool

	// RuntimeReported is true when the platform returned a runtime object for
	// this pod. Only meaningful when RuntimeProbed is true.
	//
	// Note: a stopped pod keeps reporting stale runtime telemetry for a while
	// (observed: uptimeInSeconds still climbing on an EXITED pod), so this must
	// only ever be consulted after desiredStatus says RUNNING.
	RuntimeReported bool
}

// State is the derived result.
type State struct {
	Status Status
	Reason Reason
}

// Derive maps the available signals onto the vocabulary. It never guesses: any
// combination it cannot account for becomes StatusUnknown, leaving the caller
// to fall back on the raw desiredStatus that ships alongside it.
func Derive(s Signals) State {
	switch strings.ToUpper(strings.TrimSpace(s.DesiredStatus)) {
	case "RUNNING":
		switch {
		case !s.RuntimeProbed:
			return State{Status: StatusUnknown, Reason: ReasonRuntimeUnavailable}
		case s.RuntimeReported:
			return State{Status: StatusRunning}
		default:
			return State{Status: StatusInitializing, Reason: ReasonAwaitingContainer}
		}
	case "EXITED":
		return State{
			Status: StatusStopped,
			Reason: actor(s.LastStatusChange, "exited by ", ReasonStoppedByUser, ReasonStoppedByRunpod),
		}
	case "TERMINATED":
		return State{
			Status: StatusTerminated,
			Reason: actor(s.LastStatusChange, "terminated by ", ReasonTerminatedByUser, ReasonTerminatedByRunpod),
		}
	default:
		return State{Status: StatusUnknown}
	}
}

// SSHUnavailableReason explains, in one lowercase clause, why no ssh connection
// could be built for a pod in this state. It returns "" when the state says
// nothing useful, so callers keep their bare message.
//
// The running case is the non-obvious one: the container is genuinely up, so
// the only thing left that can block ssh is the pod not publishing port 22,
// which happens whenever a pod was created without it in --ports.
func SSHUnavailableReason(st State) string {
	switch st.Status {
	case StatusRunning:
		return "no public port 22 mapped; recreate the pod with --ports 22/tcp"
	case StatusInitializing:
		return "container is still starting (image pull, container create, or boot)"
	case StatusStopped:
		return "pod is stopped; start it with 'runpodctl pod start <pod-id>'"
	case StatusTerminated:
		return "pod is terminated"
	default:
		return ""
	}
}

// actor reads the "<verb> by user" / "<verb> by Runpod" attribution out of
// lastStatusChange. The match is case-insensitive on purpose: the backend
// spells the platform's name both "Runpod" (stopPod, terminatePod) and
// "RunPod" (terminateAllStoppedPods), and a case-sensitive check silently stops
// matching whenever one of those is touched.
func actor(lastStatusChange any, prefix string, byUser, byRunpod Reason) Reason {
	text, ok := lastStatusChange.(string)
	if !ok {
		return ""
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, prefix+"user"):
		return byUser
	case strings.Contains(lower, prefix+"runpod"):
		return byRunpod
	default:
		return ""
	}
}
