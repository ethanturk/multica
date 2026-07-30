package dettools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUITestRedactionRecursivelyRemovesSecrets(t *testing.T) {
	const (
		bearer = "ui-test-bearer-value"
		jwt    = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.signature123"
		cookie = "session=ui-test-cookie"
		token  = "ui-test-query-token"
	)
	input := map[string]any{
		"console":  "request failed: Authorization: Bearer " + bearer + " at /api/issues",
		"network":  "GET http://127.0.0.1:3000/api?token=" + token + "&page=2",
		"markdown": "## Failure\nCookie: " + cookie + "\nJWT " + jwt,
		"machine_data": map[string]any{
			"access_token":  "nested-access-token",
			"Set-Cookie":    "session=map-key-cookie",
			"AUTHORIZATION": "Bearer map-key-authorization",
			"Cookie":        "session=map-key-cookie-exact",
			"events": []any{
				map[string]any{"authorization": "Bearer nested-authorization"},
				"Set-Cookie: refresh=nested-refresh; HttpOnly",
			},
		},
	}

	redacted := redactUITestValue(input)
	raw, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)

	for _, secret := range []string{
		bearer, jwt, cookie, token, "nested-access-token", "nested-authorization", "nested-refresh",
		"map-key-cookie", "map-key-authorization", "map-key-cookie-exact",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted output contains secret %q: %s", secret, got)
		}
	}
	for _, context := range []string{
		"Authorization: Bearer [REDACTED]",
		"Cookie: [REDACTED]",
		"Set-Cookie: [REDACTED]",
		"/api/issues",
	} {
		if !strings.Contains(got, context) {
			t.Errorf("redacted output lost diagnostic context %q: %s", context, got)
		}
	}
	network := redacted.(map[string]any)["network"].(string)
	if want := "http://127.0.0.1:3000/api?token=[REDACTED]&page=2"; !strings.Contains(network, want) {
		t.Errorf("network redaction = %q, want context %q", network, want)
	}
}

func TestUITestRedactionUsesNameValueAndCollectionContext(t *testing.T) {
	input := map[string]any{
		"log": map[string]any{
			"entries": []any{
				map[string]any{
					"request": map[string]any{
						"headers": []any{
							map[string]any{"name": "Authorization", "value": "Bearer structural-auth"},
							map[string]any{"NAME": "set_cookie", "VALUE": "session=structural-set-cookie"},
							map[string]any{"name": "X-API-Token", "value": "structural-api-token"},
							map[string]any{"name": "X-Debug-ID", "value": "diagnostic-123"},
						},
						"cookies": []any{
							map[string]any{"name": "session", "value": "structural-har-cookie"},
						},
						"queryString": []any{
							map[string]any{"name": "refresh-token", "value": "structural-refresh-token"},
							map[string]any{"name": "page", "value": "2"},
						},
					},
				},
			},
		},
		"machine_data": []any{
			map[string]any{"Name": "PASSWORD", "Value": "structural-password"},
			map[string]any{"name": "duration", "value": "42ms"},
		},
	}

	redacted := redactUITestValue(input)
	raw, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, secret := range []string{
		"structural-auth",
		"structural-set-cookie",
		"structural-api-token",
		"structural-har-cookie",
		"structural-refresh-token",
		"structural-password",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted output contains structural secret %q: %s", secret, got)
		}
	}
	for _, diagnostic := range []string{"diagnostic-123", `"value":"2"`, `"value":"42ms"`} {
		if !strings.Contains(got, diagnostic) {
			t.Errorf("redacted output lost non-secret diagnostic %q: %s", diagnostic, got)
		}
	}
}
