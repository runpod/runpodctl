package configenv

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// AllowInsecureAPIEnv lets a non-loopback http:// api url through. Like the other
// RUNPOD_* variables it is read from the environment; the matching command line
// switch is the global --insecure flag (see SetAllowInsecureAPI).
const AllowInsecureAPIEnv = "RUNPOD_ALLOW_INSECURE_API"

// ErrInsecureAPIURL is returned (wrapped) when a request that carries the api
// key would go out over plain http to a host that is not loopback.
var ErrInsecureAPIURL = errors.New("refusing to send the api key over plain http")

var allowInsecureFlag atomic.Bool

// SetAllowInsecureAPI records the global --insecure flag.
func SetAllowInsecureAPI(allow bool) { allowInsecureFlag.Store(allow) }

// AllowInsecureAPI reports whether --insecure was given or
// RUNPOD_ALLOW_INSECURE_API is truthy (1, true, ...).
func AllowInsecureAPI() bool {
	if allowInsecureFlag.Load() {
		return true
	}
	allowed, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(AllowInsecureAPIEnv)))
	return err == nil && allowed
}

// IsLoopbackHost reports whether host (no port) is localhost or a loopback ip.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckAPIURL refuses an http:// url whose host is not loopback unless insecure
// access was explicitly allowed. Every request that attaches the api key calls it
// first: over plain http the key travels in the clear and anyone on the path can
// read or rewrite it. https and loopback http (a local dev stack) always pass.
// The url is reduced to scheme://host in the message so userinfo and query are
// never echoed.
func CheckAPIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return nil //nolint:nilerr // not a parseable http url: https and other schemes are not this check's concern, and net/http reports malformed urls itself
	}
	if IsLoopbackHost(u.Hostname()) || AllowInsecureAPI() {
		return nil
	}
	return fmt.Errorf("%w: api url %s://%s is not https; use an https url, or pass --insecure / set %s=1 to allow it", ErrInsecureAPIURL, u.Scheme, u.Host, AllowInsecureAPIEnv)
}
