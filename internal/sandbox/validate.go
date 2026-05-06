package sandbox

import (
	"fmt"
	"strings"
)

// ValidatePath ensures the given relative or absolute path stays within
// basePath. Returns the normalized absolute path on success.
//
// The stack-based normalization pops on ".." even when the stack is empty —
// this differs from filepath.Clean and is enforced by the fuzz test in
// validate_test.go.
func ValidatePath(basePath, path string) (string, error) {
	if strings.HasPrefix(path, "/") {
		normalized := normalize(path)
		if !withinBase(normalized, basePath) {
			return "", fmt.Errorf("path %q is outside the allowed directory: %s", path, basePath)
		}
		return normalized, nil
	}

	absolute := basePath + "/" + path
	normalized := normalize(absolute)
	if !withinBase(normalized, basePath) {
		return "", fmt.Errorf("path %q resolves outside the allowed directory: %s", path, basePath)
	}
	return normalized, nil
}

func withinBase(normalized, basePath string) bool {
	if normalized == basePath {
		return true
	}
	return strings.HasPrefix(normalized, basePath+"/")
}

func normalize(path string) string {
	parts := strings.Split(path, "/")
	resolved := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
			continue
		}
		resolved = append(resolved, part)
	}
	return "/" + strings.Join(resolved, "/")
}
