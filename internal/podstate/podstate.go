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
//     "runtime == null" usually means "the host daemon is not reporting a
//     container for this pod yet" — that absence is the only start-up signal
//     there is — but it is also what a 2s timeout or a hapi 5xx looks like, so
//     it is evidence and not proof. See StatusInitializing.
//   - The host daemon does track image pulls, and does expose them, but not on
//     this path: podsync pushes "create container: still fetching image <img>"
//     into pkg/userlogs, which hapi serves as *log text* at
//     GET /v1/pod/:podId/logs (public: /v2/pods/{id}/logs). What is dropped is
//     the structured pull state: the telemetry hapi serves at
//     /v1/internal/pod/{id} is stats/gpus/ports/uptime only, so nothing
//     machine-readable about the pull reaches `Pod.runtime`. That log route is
//     on a different base url than this cli's (v2-rest.runpod.io, not
//     rest.runpod.io/v1) but it does accept this cli's key, so reading it is a
//     plausible follow-up — as free text to show a human, not as a state to
//     branch on.
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
//
// The one machine-readable cause the platform does record is an outbid on a
// spot/community pod (`lastStatusChange` = "Outbid: <date>", written by
// model/src/pod/resumePod.ts and model/src/utils/index.ts on both the EXITED
// and TERMINATED paths), so that one gets its own reason.
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

	// StatusInitializing means desiredStatus is RUNNING and the platform
	// reports no runtime telemetry. Usually the pod is placed and the container
	// is not up yet — image pull, container create or container boot, which the
	// platform does not distinguish — but the same absence is what an upstream
	// telemetry lookup failure looks like, so this is "no container reported",
	// not "the container is provably down". Either way the action is the same:
	// keep polling.
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
	// ReasonAwaitingContainer accompanies StatusInitializing: no container is
	// being reported for a pod that should be running. Could be image pull,
	// container create or boot — indistinguishable from outside — or an
	// upstream telemetry lookup that failed.
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

	// ReasonStoppedOutbid means the pod was a spot/community pod and lost its
	// machine to a higher bid. This is the one involuntary stop the platform
	// records a real cause for, and it is the only one worth retrying on a
	// different machine or at on-demand pricing.
	ReasonStoppedOutbid Reason = "stopped_outbid"

	// ReasonTerminatedOutbid is ReasonStoppedOutbid on the path where the
	// backend terminates the pod outright instead of exiting it.
	ReasonTerminatedOutbid Reason = "terminated_outbid"

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
			Reason: stopReason(s.LastStatusChange, "exited by ", ReasonStoppedByUser, ReasonStoppedByRunpod, ReasonStoppedOutbid),
		}
	case "TERMINATED":
		return State{
			Status: StatusTerminated,
			Reason: stopReason(s.LastStatusChange, "terminated by ", ReasonTerminatedByUser, ReasonTerminatedByRunpod, ReasonTerminatedOutbid),
		}
	default:
		return State{Status: StatusUnknown}
	}
}

// IsKnownDown reports whether the platform positively says the container is
// gone. StatusUnknown is deliberately not included: "we could not tell" is not
// evidence that a pod is down, so callers must not throw away information (an
// ssh connection they already built, say) on the strength of it.
func (st State) IsKnownDown() bool {
	return st.Status == StatusStopped || st.Status == StatusTerminated
}

// Explain describes the state in one lowercase clause, or "" when the state
// says nothing worth adding. It explains the *pod*, never a particular way of
// reaching it: ssh-specific advice lives with the ssh code, in
// internal/sshconnect.
func (st State) Explain() string {
	switch st.Status {
	case StatusInitializing:
		return "no container reported yet (image pull, container create or boot)"
	case StatusStopped:
		return "pod is stopped; start it with 'runpodctl pod start <pod-id>'"
	case StatusTerminated:
		return "pod is terminated"
	default:
		return ""
	}
}

// stopReason reads what little cause lastStatusChange records: the
// "<verb> by user" / "<verb> by Runpod" attribution, or an outbid.
//
// The match is case-insensitive on purpose: the backend spells the platform's
// name both "Runpod" (stopPod, terminatePod) and "RunPod"
// (terminateAllStoppedPods), and a case-sensitive check silently stops matching
// whenever one of those is touched.
//
// Anything else yields "", and the caller still has the raw lastStatusChange in
// the same output — `pod get` and `pod list` both publish it — so an unknown
// phrasing degrades to "no token" rather than to a wrong token.
func stopReason(lastStatusChange any, prefix string, byUser, byRunpod, outbid Reason) Reason {
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
	case strings.Contains(lower, "outbid"):
		return outbid
	default:
		return ""
	}
}
