package redact

import (
	"regexp"
	"strings"

	"github.com/runpod/runpodctl/internal/configenv"
)

// minAPIKeyMaskLen is the shortest configured key that is masked by value. A
// shorter value is more likely to be a placeholder than a key, and masking it
// would mangle ordinary text.
const minAPIKeyMaskLen = 8

// keyPlaceholder replaces the configured API key wherever it appears.
const keyPlaceholder = "[REDACTED]"

// bearerPattern matches an HTTP bearer credential ("Authorization: Bearer
// <token>", or a bare "Bearer <token>"). The token ends at whitespace or at a
// character that cannot be part of a token but does delimit one inside JSON or
// markup.
var bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[^\s"'\\<>]+`)

// apiKeyPattern matches a RunPod API key by its rpa_ prefix.
var apiKeyPattern = regexp.MustCompile(`\brpa_[A-Za-z0-9_\-]+`)

// maskTokens masks credentials that a server may echo back in an error body:
// the configured API key (by value), bearer tokens and rpa_ keys.
func maskTokens(s string) string {
	if key := configenv.APIKey(); len(key) >= minAPIKeyMaskLen {
		s = strings.ReplaceAll(s, key, keyPlaceholder)
	}
	s = bearerPattern.ReplaceAllString(s, "Bearer "+keyPlaceholder)
	return apiKeyPattern.ReplaceAllString(s, "rpa_"+keyPlaceholder)
}
