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
	"accesstoken":   true,
	"idtoken":       true,
	"refreshtoken":  true,
	"authorization": true,
	"cookie":        true,
	"setcookie":     true,
	"password":      true,
	"secret":        true,
}

func redactUITestValue(value any) any {
	return redactUITestValueInContext(value, "")
}

func redactUITestValueInContext(value any, collection string) any {
	switch value := value.(type) {
	case map[string]any:
		redactNamedValue := uiTestMapNamesCredential(value) || normalizeUITestSecretKey(collection) == "cookies"
		redacted := make(map[string]any, len(value))
		for key, item := range value {
			normalizedKey := normalizeUITestSecretKey(key)
			if uiTestSecretKeys[normalizedKey] || normalizedKey == "value" && redactNamedValue {
				redacted[key] = uiTestRedacted
				continue
			}
			redacted[key] = redactUITestValueInContext(item, key)
		}
		return redacted
	case []any:
		redacted := make([]any, len(value))
		for i, item := range value {
			redacted[i] = redactUITestValueInContext(item, collection)
		}
		return redacted
	case string:
		return redactUITestString(value)
	default:
		return value
	}
}

func uiTestMapNamesCredential(value map[string]any) bool {
	names := 0
	for key, item := range value {
		if normalizeUITestSecretKey(key) != "name" {
			continue
		}
		names++
		name, ok := item.(string)
		if ok && isUITestCredentialName(name) {
			return true
		}
	}
	return names > 1
}

func isUITestCredentialName(name string) bool {
	normalized := normalizeUITestSecretKey(name)
	if uiTestSecretKeys[normalized] {
		return true
	}
	for _, suffix := range []string{"authorization", "token", "password", "secret"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func isUITestStorageStateValue(value any) bool {
	storageState, ambiguous := inspectUITestStorageStateValue(value)
	return storageState || ambiguous
}

func inspectUITestStorageStateValue(value any) (bool, bool) {
	switch value := value.(type) {
	case map[string]any:
		type shape struct {
			seen  bool
			array bool
		}
		shapes := map[string]shape{}
		nestedStorage := false
		ambiguous := false
		for key, item := range value {
			if storage, conflict := inspectUITestStorageStateValue(item); storage || conflict {
				nestedStorage = nestedStorage || storage
				ambiguous = ambiguous || conflict
			}
			normalized := normalizeUITestSecretKey(key)
			if normalized != "cookies" && normalized != "origins" && normalized != "localstorage" {
				continue
			}
			_, array := item.([]any)
			prior := shapes[normalized]
			if prior.seen && prior.array != array {
				ambiguous = true
			}
			shapes[normalized] = shape{seen: true, array: prior.array || array}
		}
		cookies := shapes["cookies"]
		origins := shapes["origins"]
		return nestedStorage || cookies.array && origins.array, ambiguous
	case []any:
		storageState := false
		ambiguous := false
		for _, item := range value {
			storage, conflict := inspectUITestStorageStateValue(item)
			storageState = storageState || storage
			ambiguous = ambiguous || conflict
		}
		return storageState, ambiguous
	default:
		return false, false
	}
}

func normalizeUITestSecretKey(key string) string {
	return strings.Map(func(character rune) rune {
		switch character {
		case '-', '_', ' ', '\t', '\r', '\n':
			return -1
		default:
			return character
		}
	}, strings.ToLower(key))
}

func redactUITestString(value string) string {
	value = uiTestBearerPattern.ReplaceAllString(value, `${1}`+uiTestRedacted)
	value = uiTestCookiePattern.ReplaceAllString(value, `${1}${2}`+uiTestRedacted)
	value = uiTestJWTShape.ReplaceAllString(value, uiTestRedacted)
	return uiTestSecretQuery.ReplaceAllString(value, `${1}`+uiTestRedacted)
}
