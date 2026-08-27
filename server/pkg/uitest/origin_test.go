package uitest

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		host string
		want bool
	}{
		{name: "localhost", host: "localhost", want: true},
		{name: "case insensitive localhost", host: "LOCALHOST", want: true},
		{name: "ipv4 loopback", host: "127.0.0.1", want: true},
		{name: "ipv6 loopback", host: "[::1]", want: true},
		{name: "external address", host: "192.168.1.10", want: false},
		{name: "external hostname", host: "example.com", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := IsLoopbackHost(test.host); got != test.want {
				t.Fatalf("IsLoopbackHost(%q) = %v, want %v", test.host, got, test.want)
			}
		})
	}
}

func TestValidateLoopbackURL(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "http localhost", raw: "http://localhost:3000"},
		{name: "https ipv4", raw: "https://127.0.0.1:8443"},
		{name: "https ipv6", raw: "https://[::1]:8443"},
		{name: "non http scheme", raw: "file:///tmp/app", wantErr: true},
		{name: "external host", raw: "http://example.com", wantErr: true},
		{name: "credentials", raw: "http://user:pass@localhost:3000", wantErr: true},
		{name: "fragment", raw: "http://localhost:3000/#section", wantErr: true},
		{name: "missing host", raw: "http:///path", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateLoopbackURL(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ValidateLoopbackURL(%q) succeeded with %v", test.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateLoopbackURL(%q) error = %v", test.raw, err)
			}
			if got.String() != test.raw {
				t.Fatalf("ValidateLoopbackURL(%q) = %q", test.raw, got)
			}
		})
	}
}
