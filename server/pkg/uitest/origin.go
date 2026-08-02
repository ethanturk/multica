package uitest

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func ValidateLoopbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid UI test URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("UI test URL must use http or https")
	}
	if u.User != nil || u.Fragment != "" || !IsLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("UI test URL must target loopback without credentials or fragments")
	}
	return u, nil
}

func IsLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
