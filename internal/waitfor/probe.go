package waitfor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// Prober reports whether a service is answering at addr. A nil error means
// reachable; the returned error is used verbatim as the "last known state" note,
// so keep it short and lowercase.
type Prober func(ctx context.Context, addr string) error

const (
	probeDialTimeout = 5 * time.Second
	probeReadTimeout = 5 * time.Second
	sshBannerPrefix  = "SSH-"
)

// ProbeSSH reports whether an ssh server is accepting connections at addr.
//
// Why this is not just "is port 22 listed in runtime.ports": that only says the
// port was *allocated*. Verified against prod with a cpu pod running
// `alpine:3.20 sleep infinity` — the api reported privatePort 22 / isIpPublic
// with a public port within ~25s, while a tcp connect to it was refused,
// because that image runs no sshd. Port allocation is therefore not readiness.
//
// It stops at the server's identification banner (RFC 4253: the server sends it
// first) and deliberately does not attempt a handshake: that would need the
// user's private key, and `pod create --wait` must work for someone who has
// never run `runpodctl doctor`. A banner proves sshd itself is up, which a bare
// tcp connect to a port-forwarder would not.
func ProbeSSH(ctx context.Context, addr string) error {
	dialer := net.Dialer{Timeout: probeDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	deadline := time.Now().Add(probeReadTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetReadDeadline(deadline)

	banner := make([]byte, len(sshBannerPrefix))
	if _, err := io.ReadFull(conn, banner); err != nil {
		return fmt.Errorf("connected to %s but got no ssh banner: %w", addr, err)
	}
	if !bytes.HasPrefix(banner, []byte(sshBannerPrefix)) {
		return fmt.Errorf("%s answered but is not an ssh server", addr)
	}
	return nil
}
