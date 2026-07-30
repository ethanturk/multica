package uitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

const maxStorageStateBytes = 1 << 20

type storageState struct {
	Cookies []*storageCookie `json:"cookies"`
	Origins []*storageOrigin `json:"origins"`
}

type storageCookie struct {
	Name     *string  `json:"name"`
	Value    *string  `json:"value"`
	Domain   *string  `json:"domain"`
	Path     *string  `json:"path"`
	Expires  *float64 `json:"expires"`
	HTTPOnly *bool    `json:"httpOnly"`
	Secure   *bool    `json:"secure"`
	SameSite *string  `json:"sameSite"`
}

type storageOrigin struct {
	Origin       *string             `json:"origin"`
	LocalStorage []*storageNameValue `json:"localStorage"`
}

type storageNameValue struct {
	Name  *string `json:"name"`
	Value *string `json:"value"`
}

func writeEmptyStorageState(path string) error {
	return writeAtomic0600(path, []byte("{\"cookies\":[],\"origins\":[]}\n"))
}

func validateStorageState(path string) error {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat storage state: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return fmt.Errorf("storage state must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open storage state: %w", err)
	}
	defer file.Close()
	openInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat open storage state: %w", err)
	}
	if !openInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openInfo) {
		return fmt.Errorf("storage state changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStorageStateBytes+1))
	if err != nil {
		return fmt.Errorf("read storage state: %w", err)
	}
	if len(data) > maxStorageStateBytes {
		return fmt.Errorf("storage state exceeds 1 MiB")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state storageState
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode storage state: %w", err)
	}
	if state.Cookies == nil || state.Origins == nil {
		return fmt.Errorf("storage state requires cookies and origins arrays")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("storage state contains multiple JSON values")
		}
		return fmt.Errorf("decode storage state: %w", err)
	}
	for _, cookie := range state.Cookies {
		if cookie == nil ||
			cookie.Name == nil || cookie.Value == nil ||
			cookie.Domain == nil || cookie.Path == nil ||
			cookie.Expires == nil || cookie.HTTPOnly == nil ||
			cookie.Secure == nil || cookie.SameSite == nil {
			return fmt.Errorf("storage state cookie requires every supported field")
		}
		domain := strings.TrimPrefix(strings.TrimSpace(*cookie.Domain), ".")
		if domain == "" || !IsLoopbackHost(domain) {
			return fmt.Errorf("storage state cookie domain must be loopback")
		}
		if *cookie.Name == "" || *cookie.Path == "" ||
			math.IsNaN(*cookie.Expires) || math.IsInf(*cookie.Expires, 0) {
			return fmt.Errorf("storage state cookie is invalid")
		}
		switch *cookie.SameSite {
		case "Strict", "Lax", "None":
		default:
			return fmt.Errorf("storage state cookie SameSite is invalid")
		}
	}
	for _, origin := range state.Origins {
		if origin == nil || origin.Origin == nil {
			return fmt.Errorf("storage state origin is invalid")
		}
		parsed, err := ValidateLoopbackURL(*origin.Origin)
		if err != nil {
			return fmt.Errorf("storage state origin: %w", err)
		}
		if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" {
			return fmt.Errorf("storage state origin must not contain a path or query")
		}
		if origin.LocalStorage == nil {
			return fmt.Errorf("storage state origin requires localStorage")
		}
		for _, entry := range origin.LocalStorage {
			if entry == nil || entry.Name == nil || entry.Value == nil {
				return fmt.Errorf("storage state localStorage entry requires name and value")
			}
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close storage state: %w", err)
	}
	return writeAtomic0600(path, data)
}
