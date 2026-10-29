package jre

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionRE = regexp.MustCompile(`(?m)^\S+ version "([^"]+)"`)

// ParseVersion extracts the feature version (8 for "1.8.0_402", 25 for
// "25.0.5") and the full quoted version string from `java -version` output.
func ParseVersion(javaVersionOutput string) (feature int, full string, err error) {
	m := versionRE.FindStringSubmatch(javaVersionOutput)
	if m == nil {
		return 0, "", fmt.Errorf("parse java version: no version line in %q", strings.TrimSpace(javaVersionOutput))
	}
	full = m[1]
	s := strings.TrimPrefix(full, "1.")
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, "", fmt.Errorf("parse java version: malformed version %q", full)
	}
	feature, err = strconv.Atoi(s[:end])
	if err != nil {
		return 0, "", fmt.Errorf("parse java version %q: %w", full, err)
	}
	return feature, full, nil
}
