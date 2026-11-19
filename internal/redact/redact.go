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
	"errors"
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
// errors.As still work) but whose message has been redacted. A *url.Error has
// its URL field rewritten to scheme://host/path as well, so the signature is not
// reachable through the typed error either. Error(nil) is nil.
func Error(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*redactedError); ok { //nolint:errorlint // only an already-redacted outermost error is reused
		return err
	}
	msg := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) {
		// the quoted URL may contain characters (spaces from a malformed URL)
		// that end the text patterns early, so swap in the typed rewrite first.
		clean := &url.Error{Op: ue.Op, URL: URL(ue.URL), Err: ue.Err}
		msg = strings.ReplaceAll(msg, ue.Error(), clean.Error())
		if err == error(ue) { //nolint:errorlint // outermost *url.Error: hand back the rewritten typed error
			return &redactedError{msg: Text(msg), err: &url.Error{Op: ue.Op, URL: clean.URL, Err: Error(ue.Err)}}
		}
	}
	return &redactedError{msg: Text(msg), err: err}
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }
