package runtime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`(?i)(\d+)\.(\d+)(?:\.(\d+))?`)

// ExtractVersion returns the first numeric version found in command output.
func ExtractVersion(output string) string {
	match := versionPattern.FindStringSubmatch(output)
	if match == nil {
		return ""
	}
	if match[3] == "" {
		return match[1] + "." + match[2]
	}
	return match[1] + "." + match[2] + "." + match[3]
}

// Compatible reports whether an actual version satisfies the matrix
// requirement. Requirements may be "system"/"*" (any version), a dotted
// prefix such as "16.x" or "3.3", or a simple range such as
// ">=16.0 <17.0".
func Compatible(actual, required string) (bool, error) {
	actualParts, err := parseVersion(actual)
	if err != nil {
		return false, fmt.Errorf("actual version %q: %w", actual, err)
	}
	required = strings.TrimSpace(strings.ToLower(required))
	switch required {
	case "", "*", "any", "system":
		return true, nil
	}

	if strings.ContainsAny(required, "<>=^~") {
		return matchRange(actualParts, required)
	}
	return matchPrefix(actualParts, required)
}

func matchPrefix(actual []int, required string) (bool, error) {
	parts := strings.Split(strings.TrimPrefix(required, "v"), ".")
	if len(parts) == 0 || len(parts) > 3 {
		return false, fmt.Errorf("unsupported version requirement %q", required)
	}
	for i, part := range parts {
		if part == "x" || part == "*" {
			return true, nil
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return false, fmt.Errorf("unsupported version requirement %q", required)
		}
		if i >= len(actual) || actual[i] != value {
			return false, nil
		}
	}
	return true, nil
}

func matchRange(actual []int, required string) (bool, error) {
	tokens := strings.Fields(strings.NewReplacer(",", " ").Replace(required))
	if len(tokens) == 0 {
		return true, nil
	}
	for _, token := range tokens {
		op := ""
		for _, candidate := range []string{">=", "<=", ">", "<", "=", "==", "^", "~"} {
			if strings.HasPrefix(token, candidate) {
				op = candidate
				token = strings.TrimSpace(strings.TrimPrefix(token, candidate))
				break
			}
		}
		if op == "" {
			op = "="
		}
		bound, err := parseVersion(token)
		if err != nil {
			return false, fmt.Errorf("unsupported version requirement %q: %w", required, err)
		}
		cmp := compareVersion(actual, bound)
		ok := false
		switch op {
		case ">=":
			ok = cmp >= 0
		case "<=":
			ok = cmp <= 0
		case ">":
			ok = cmp > 0
		case "<":
			ok = cmp < 0
		case "=", "==":
			ok = cmp == 0
		case "^":
			ok = cmp >= 0 && actual[0] == bound[0]
		case "~":
			ok = cmp >= 0 && actual[0] == bound[0] && actual[1] == bound[1]
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func parseVersion(value string) ([]int, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v"))
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("not a version")
	}
	result := []int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		part := parts[i]
		if cut := strings.IndexFunc(part, func(r rune) bool {
			return r < '0' || r > '9'
		}); cut >= 0 {
			part = part[:cut]
		}
		if part == "" {
			return nil, fmt.Errorf("not a version")
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("not a version")
		}
		result[i] = n
	}
	return result, nil
}

func compareVersion(a, b []int) int {
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
