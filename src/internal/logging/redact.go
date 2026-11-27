package logging

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

const maxRegisteredSecrets = 512

var (
	secretMu       sync.RWMutex
	registered     []string
	userinfoRegexp = regexp.MustCompile(`(://)[^/@\s]+@`)
)

// RegisterSecret adds a value that must never appear in log output. Values
// shorter than six characters are ignored to avoid over-redacting.
func RegisterSecret(value string) {
	if len(value) < 6 {
		return
	}
	secretMu.Lock()
	defer secretMu.Unlock()
	for _, existing := range registered {
		if existing == value {
			return
		}
	}
	registered = append(registered, value)
	if len(registered) > maxRegisteredSecrets {
		registered = registered[len(registered)-maxRegisteredSecrets:]
	}
	// Replace longer values first so overlapping secrets are fully removed.
	sort.Slice(registered, func(i, j int) bool { return len(registered[i]) > len(registered[j]) })
}

// Redact removes registered secrets and URL userinfo from s.
func Redact(s string) string {
	if s == "" {
		return s
	}
	secretMu.RLock()
	values := append([]string(nil), registered...)
	secretMu.RUnlock()
	for _, v := range values {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return userinfoRegexp.ReplaceAllString(s, "${1}[REDACTED]@")
}

func sensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, fragment := range []string{
		"password", "passwd", "secret", "token", "authorization",
		"cookie", "api_key", "apikey", "credential", "private_key",
	} {
		if strings.Contains(k, fragment) {
			return true
		}
	}
	return false
}
