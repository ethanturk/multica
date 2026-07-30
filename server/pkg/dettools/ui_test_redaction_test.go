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

func TestUITestRedactionDeterministicallyHandlesNormalizedNameConflicts(t *testing.T) {
	for iteration := 0; iteration < 1000; iteration++ {
		secret := "conflicting-secret"
		input := map[string]any{
			"nested": []any{
				map[string]any{
					"name":  "duration",
					"Name":  "Authorization",
					"value": secret,
				},
				map[string]any{
					"name":  "duration",
					"NAME":  "page",
					"value": "ambiguous-diagnostic",
				},
				map[string]any{
					"name":  "duration",
					"value": "42ms",
				},
			},
		}
		raw, err := json.Marshal(redactUITestValue(input))
		if err != nil {
			t.Fatal(err)
		}
		got := string(raw)
		for _, mustRedact := range []string{secret, "ambiguous-diagnostic"} {
			if strings.Contains(got, mustRedact) {
				t.Fatalf("iteration %d retained ambiguous value %q: %s", iteration, mustRedact, got)
			}
		}
		if !strings.Contains(got, `"value":"42ms"`) {
			t.Fatalf("iteration %d lost unrelated diagnostic: %s", iteration, got)
		}
	}
}

func TestUITestStorageStateDetectionHandlesAliasesConservatively(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{
			name: "conflicting cookie aliases",
			value: map[string]any{
				"cookies": "wrong",
				"Cookies": []any{},
				"origins": []any{},
			},
			want: true,
		},
		{
			name: "mixed case storage state",
			value: map[string]any{
				"CoOkIeS": []any{},
				"ORIGINS": []any{},
			},
			want: true,
		},
		{
			name: "nested local storage alias conflict",
			value: map[string]any{
				"origins": []any{
					map[string]any{
						"localStorage":  []any{},
						"Local_Storage": "wrong",
					},
				},
			},
			want: true,
		},
		{
			name: "cookies alone",
			value: map[string]any{
				"cookies": []any{},
			},
		},
		{
			name: "origins alone",
			value: map[string]any{
				"origins": []any{},
			},
		},
		{
			name: "consistent local storage aliases",
			value: map[string]any{
				"localStorage":  []any{},
				"Local_Storage": []any{},
			},
		},
		{
			name: "unrelated scalar fields",
			value: map[string]any{
				"cookies": "enabled",
				"origins": "source",
			},
		},
	}

	for iteration := 0; iteration < 1000; iteration++ {
		for _, test := range tests {
			if got := isUITestStorageStateValue(test.value); got != test.want {
				t.Fatalf("iteration %d %s = %t, want %t", iteration, test.name, got, test.want)
			}
		}
	}
}
