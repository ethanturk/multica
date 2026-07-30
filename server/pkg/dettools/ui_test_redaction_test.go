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
