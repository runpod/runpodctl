package sshconnect

import (
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/podstate"
)

// NotReadyMessage explains why no ssh connection could be built, replacing the
// bare "pod not ready" that used to be the only thing an agent got while an
// image was pulling (CON-690).
//
// The running case is the one that needs care. The container is genuinely up, so
// what is missing is a *publicly routable* port 22 — which BuildConnection
// requires — and there are three different reasons for that, with three
// different remedies. Getting this wrong is expensive: telling someone to
// recreate a paid pod with a flag it already has is worse than saying nothing.
//
// declaredPorts is the pod's requested port list ("22/tcp", "8888/http", ...)
// and runtimePorts is what the host actually mapped; either may be empty.
func NotReadyMessage(state podstate.State, declaredPorts []string, runtimePorts []*api.LegacyPort) string {
	detail := state.Explain()
	if state.Status == podstate.StatusRunning {
		detail = sshPortDetail(declaredPorts, runtimePorts)
	}
	if detail == "" {
		return "pod not ready"
	}
	return "pod not ready: " + detail
}

func sshPortDetail(declaredPorts []string, runtimePorts []*api.LegacyPort) string {
	if !declaresSSHPort(declaredPorts) {
		// non-destructive and verified live: `pod update --ports 22/tcp` on a
		// pod that was created without it produced a public 22 mapping within
		// seconds, no restart and no recreate. never tell someone to destroy a
		// paid pod for this.
		return "pod does not publish 22/tcp; add it with 'runpodctl pod update <pod-id> --ports 22/tcp'"
	}
	for _, port := range runtimePorts {
		if port != nil && port.PrivatePort == 22 {
			// host maps any binding that is not on 0.0.0.0 to isIpPublic=false,
			// which is what a machine with no public ip looks like. direct ssh
			// cannot work; there is nothing the caller can change on the pod.
			return "port 22 is mapped but not publicly routable on this machine"
		}
	}
	return "port 22 is declared but the host has not published a mapping for it yet"
}

// declaresSSHPort reports whether the pod asked for tcp 22. Entries look like
// "22/tcp"; a bare "22" is accepted too since the api is lenient about it.
func declaresSSHPort(declaredPorts []string) bool {
	for _, entry := range declaredPorts {
		number, _, _ := strings.Cut(strings.TrimSpace(entry), "/")
		if number == "22" {
			return true
		}
	}
	return false
}

// SplitPorts turns graphql's comma-separated `Pod.ports` string into the same
// shape rest returns as a list.
func SplitPorts(ports string) []string {
	if strings.TrimSpace(ports) == "" {
		return nil
	}
	parts := strings.Split(ports, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
