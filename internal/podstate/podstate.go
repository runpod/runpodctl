// Package podstate derives a pod's observable runtime state from the signals
// the platform actually exposes, and nothing more.
//
// `desiredStatus` answers "what did you ask for", never "what is happening": a
// pod that has been serving for an hour and a pod whose 20 GB image is still
// downloading are both RUNNING. The only signal for "is the container up" is
// whether the platform reports runtime telemetry for the pod: rest v2 returns
// `runtime` (uptime and port mappings) in the same read as `status`, and
// `runtime == null` is the only start-up signal there is. There is no
// pulling/starting/ready state anywhere, so `initializing` deliberately covers
// image pull, container create and boot rather than inventing a `pulling`
// value that would be a guess.
//
// The platform does not record why a pod stopped in any machine-readable form
// that rest v2 reports, so stopped and terminated pods carry no reason.
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
	// reports no runtime telemetry: the container is not up yet (image pull,
	// container create or boot, which the platform does not distinguish). keep
	// polling.
	StatusInitializing Status = "initializing"

	// StatusStopped means desiredStatus is EXITED. The container is gone; the
	// pod's disk survives and it can be started again.
	StatusStopped Status = "stopped"

	// StatusTerminated means desiredStatus is TERMINATED. a terminated pod
	// drops out of the pod list shortly after, so this is a narrow window.
	StatusTerminated Status = "terminated"

	// StatusUnknown means the state cannot be derived: a desiredStatus other
	// than the ones above (such as ERROR), or no runtime lookup was made. Read
	// desiredStatus, which is always in the same output.
	StatusUnknown Status = "unknown"
)

// Reason is a stable lowercase token explaining a Status. It is empty when
// there is nothing to add, and callers should omit the field in that case.
type Reason string

const (
	// ReasonAwaitingContainer accompanies StatusInitializing: no container is
	// being reported for a pod that should be running.
	ReasonAwaitingContainer Reason = "awaiting_container"

	// ReasonRuntimeUnavailable accompanies StatusUnknown for a RUNNING pod:
	// the runtime lookup was not made, so running cannot be told apart from
	// initializing. This is a gap in our knowledge, not in the pod.
	ReasonRuntimeUnavailable Reason = "runtime_unavailable"
)

// Signals is every input Derive looks at.
type Signals struct {
	// DesiredStatus is the pod's desiredStatus, matched case-insensitively.
	DesiredStatus string

	// RuntimeProbed reports whether runtime telemetry was actually looked up.
	// False means "we did not ask", which is NOT the same as "the container is
	// down".
	RuntimeProbed bool

	// RuntimeReported is true when the platform returned a runtime object for
	// this pod. Only meaningful when RuntimeProbed is true.
	//
	// a stopped pod keeps reporting stale runtime telemetry for a while, so
	// this must only ever be consulted after desiredStatus says RUNNING.
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
		return State{Status: StatusStopped}
	case "TERMINATED":
		return State{Status: StatusTerminated}
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
// internal/sshconnect. podID is interpolated into suggested commands so an
// agent can run them verbatim; "" falls back to the <pod-id> placeholder.
func (st State) Explain(podID string) string {
	switch st.Status {
	case StatusInitializing:
		return "no container reported yet (image pull, container create or boot)"
	case StatusStopped:
		return "pod is stopped; start it with 'runpodctl pod start " + IDOrPlaceholder(podID) + "'"
	case StatusTerminated:
		return "pod is terminated"
	default:
		return ""
	}
}

// IDOrPlaceholder keeps suggested commands readable when a caller has no pod
// id: an empty id falls back to the <pod-id> placeholder.
func IDOrPlaceholder(podID string) string {
	if podID = strings.TrimSpace(podID); podID == "" {
		return "<pod-id>"
	}
	return podID
}
