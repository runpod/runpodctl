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

// minKeyPrefixMaskLen is the shortest trailing fragment of the configured key
// that is masked as a cut-off copy of it. Shorter fragments carry too little of
// the key to matter and are too likely to be ordinary text.
const minKeyPrefixMaskLen = 4

// maskTokens masks credentials that a server may echo back in an error body:
// the configured API key (by value), bearer tokens and rpa_ keys.
//
// Text is often a body that was cut at a fixed size, so the configured key can
// end the text half-written. The complete key is replaced first, then a proper
// prefix of it that ends the text is masked too (a cut leaves the start of a
// token, never the end). Bearer and rpa_ tokens are matched by pattern, which
// already covers a truncated token.
func maskTokens(s string) string {
	if key := configenv.APIKey(); len(key) >= minAPIKeyMaskLen {
		s = strings.ReplaceAll(s, key, keyPlaceholder)
		s = maskTrailingKeyPrefix(s, key)
	}
	s = bearerPattern.ReplaceAllString(s, "Bearer "+keyPlaceholder)
	return apiKeyPattern.ReplaceAllString(s, "rpa_"+keyPlaceholder)
}

// maskTrailingKeyPrefix replaces the longest proper prefix of key (at least
// minKeyPrefixMaskLen bytes) that ends s with keyPlaceholder.
func maskTrailingKeyPrefix(s, key string) string {
	for n := min(len(key)-1, len(s)); n >= minKeyPrefixMaskLen; n-- {
		if strings.HasSuffix(s, key[:n]) {
			return s[:len(s)-n] + keyPlaceholder
		}
	}
	return s
}
