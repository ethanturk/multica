package dettools

import (
	"regexp"
	"strings"
)

const uiTestRedacted = "[REDACTED]"

var (
	uiTestBearerPattern = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)\S+`)
	uiTestCookiePattern = regexp.MustCompile(`(?im)\b(cookie|set-cookie)(\s*:\s*)[^\r\n]+`)
	uiTestJWTShape      = regexp.MustCompile(`\b[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\b`)
	uiTestSecretQuery   = regexp.MustCompile(`(?i)([?&](?:token|access_token|id_token|refresh_token|authorization|cookie|password|secret)=)[^&#\s]+`)
)

var uiTestSecretKeys = map[string]bool{
	"token":         true,
	"access_token":  true,
	"id_token":      true,
	"refresh_token": true,
	"authorization": true,
	"cookie":        true,
	"password":      true,
	"secret":        true,
}

func redactUITestValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(value))
		for key, item := range value {
			if uiTestSecretKeys[strings.ToLower(key)] {
				redacted[key] = uiTestRedacted
				continue
			}
			redacted[key] = redactUITestValue(item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(value))
		for i, item := range value {
			redacted[i] = redactUITestValue(item)
		}
		return redacted
	case string:
		return redactUITestString(value)
	default:
		return value
	}
}

func redactUITestString(value string) string {
	value = uiTestBearerPattern.ReplaceAllString(value, `${1}`+uiTestRedacted)
	value = uiTestCookiePattern.ReplaceAllString(value, `${1}${2}`+uiTestRedacted)
	value = uiTestJWTShape.ReplaceAllString(value, uiTestRedacted)
	return uiTestSecretQuery.ReplaceAllString(value, `${1}`+uiTestRedacted)
}
