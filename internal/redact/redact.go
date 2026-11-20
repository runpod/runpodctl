// Package redact removes secrets that ride along in URLs (presigned-URL
// signatures, credentials, userinfo) from text and errors before they are
// printed or logged.
//
// Presigned object-storage URLs are bearer credentials: anyone holding one can
// perform the signed operation until it expires. Go's *url.Error quotes the full
// URL, and storage/API error bodies often echo it, so error output has to be
// scrubbed at the point where those errors are produced.
package redact

import (
	"net/url"
	"regexp"
	"strings"
)

// Placeholder replaces every redacted secret value.
const Placeholder = "REDACTED"

// urlPattern matches an http(s) URL embedded in text. Backslash is deliberately
// part of the match so JSON-escaped separators such as "\u0026" do not end the
// URL early and leave the tail of the query string behind.
var urlPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

// secretParamPattern matches well-known signing parameters outside of a
// well-formed URL, e.g. in a body excerpt that was cut off mid-URL or in a URL
// without a scheme.
var secretParamPattern = regexp.MustCompile(
	`(?i)\b(X-Amz-(?:Signature|Credential|Security-Token|Algorithm|Date|Expires|SignedHeaders)` +
		`|X-Goog-(?:Signature|Credential)|AWSAccessKeyId|Signature)=[^&\s"'<>\\]*`)

// URL returns raw reduced to scheme://host/path: userinfo, query string and
// fragment are dropped. A value that does not parse as an absolute URL is
// returned with everything from the first '?', '#' or userinfo removed on a
// best-effort basis, never unchanged if it carried a query.
func URL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		u.RawQuery = ""
		u.ForceQuery = false
		u.Fragment = ""
		u.RawFragment = ""
		return u.String()
	}
	return stripURLText(raw)
}

// stripURLText removes userinfo, query and fragment from a URL-shaped string
// without parsing it.
func stripURLText(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if scheme := strings.Index(s, "://"); scheme >= 0 {
		rest := s[scheme+3:]
		authority, _, _ := strings.Cut(rest, "/")
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			s = s[:scheme+3] + rest[at+1:]
		}
	}
	return s
}

// Text redacts the secrets in free-form text: every http(s) URL is reduced to
// scheme://host/path, and any leftover signing parameter keeps its name but
// loses its value.
func Text(s string) string {
	if s == "" {
		return s
	}
	s = urlPattern.ReplaceAllStringFunc(s, func(m string) string {
		// keep a trailing JSON escape ("\") that is not part of the URL.
		suffix := ""
		if trimmed, ok := strings.CutSuffix(m, `\`); ok {
			m = trimmed
			suffix = `\`
		}
		return stripURLText(m) + suffix
	})
	return secretParamPattern.ReplaceAllString(s, "${1}="+Placeholder)
}

// Error returns an error that carries the same chain as err (errors.Is and
// errors.As still work for its causes) but whose message has been redacted.
// Every *url.Error anywhere in the chain, including behind fmt.Errorf("%w") and
// errors.Join wrappers, has its URL field rewritten to scheme://host/path, so
// neither the message nor a typed errors.As lookup can recover the signature.
// A wrapper that sits above a *url.Error is replaced by a plain wrapper that
// keeps its (redacted) message and its causes, so errors.As to the wrapper's own
// concrete type no longer succeeds; wrappers with no *url.Error beneath them are
// kept as they are. Error(nil) is nil.
func Error(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*redactedError); ok { //nolint:errorlint // only an already-redacted outermost error is reused
		return err
	}
	clean, _ := sanitizeChain(err)
	return &redactedError{msg: errorText(err), err: clean}
}

// errorText renders err with the URL of every *url.Error in its chain reduced
// first (the quoted URL may contain characters, such as the spaces of a malformed
// URL, that end the text patterns early), then redacts the whole text.
func errorText(err error) string {
	msg := err.Error()
	walkChain(err, func(e error) {
		if ue, ok := e.(*url.Error); ok { //nolint:errorlint // every node of the chain is inspected on purpose
			clean := &url.Error{Op: ue.Op, URL: URL(ue.URL), Err: ue.Err}
			msg = strings.ReplaceAll(msg, ue.Error(), clean.Error())
		}
	})
	return Text(msg)
}

// walkChain calls fn on err and on every error reachable through Unwrap() error
// and Unwrap() []error, depth first.
func walkChain(err error, fn func(error)) {
	if err == nil {
		return
	}
	fn(err)
	switch u := err.(type) { //nolint:errorlint // walking the chain by hand
	case interface{ Unwrap() error }:
		walkChain(u.Unwrap(), fn)
	case interface{ Unwrap() []error }:
		for _, c := range u.Unwrap() {
			walkChain(c, fn)
		}
	}
}

// sanitizeChain returns err with every *url.Error in its chain replaced by a
// copy whose URL is redacted; changed is false (and err is returned as is) when
// the chain holds none.
func sanitizeChain(err error) (clean error, changed bool) {
	switch e := err.(type) { //nolint:errorlint // walking the chain by hand
	case nil:
		return nil, false
	case *url.Error:
		// Error(e.Err) also redacts the cause's own message, which can quote a URL.
		return &url.Error{Op: e.Op, URL: URL(e.URL), Err: Error(e.Err)}, true
	case interface{ Unwrap() error }:
		inner := e.Unwrap()
		cleanInner, ch := sanitizeChain(inner)
		if !ch {
			return err, false
		}
		return &wrapError{msg: errorText(err), err: cleanInner}, true
	case interface{ Unwrap() []error }:
		children := e.Unwrap()
		cleaned := make([]error, len(children))
		found := false
		for i, c := range children {
			var ch bool
			cleaned[i], ch = sanitizeChain(c)
			found = found || ch
		}
		if !found {
			return err, false
		}
		return &joinError{msg: errorText(err), errs: cleaned}, true
	}
	return err, false
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// wrapError stands in for a single-cause wrapper above a sanitized *url.Error.
type wrapError struct {
	msg string
	err error
}

func (e *wrapError) Error() string { return e.msg }
func (e *wrapError) Unwrap() error { return e.err }

// joinError stands in for a multi-cause wrapper above a sanitized *url.Error.
type joinError struct {
	msg  string
	errs []error
}

func (e *joinError) Error() string   { return e.msg }
func (e *joinError) Unwrap() []error { return e.errs }
