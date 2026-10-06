package app

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	maxFilesPerPaste = 500
	maxPasteBytes    = 25 << 20 // 25 MiB
	maxPathDepth     = 20
	maxPathLength    = 512
)

// normalizePastePath validates and normalizes an uploaded path to POSIX relative form.
// It rejects absolute, traversal, Windows, UNC, NUL, empty, and over-depth/length paths.
func normalizePastePath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("path contains NUL")
	}
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("path is not valid UTF-8")
	}
	// Reject absolute POSIX and Windows drive / UNC forms before slash rewriting.
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "\\") {
		return "", fmt.Errorf("absolute path rejected")
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "\\\\") {
		return "", fmt.Errorf("UNC path rejected")
	}
	if len(raw) >= 2 && raw[1] == ':' {
		c := raw[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return "", fmt.Errorf("Windows drive path rejected")
		}
	}
	if strings.HasPrefix(strings.ToLower(raw), "\\\\") || strings.HasPrefix(strings.ToLower(raw), "//") {
		return "", fmt.Errorf("UNC path rejected")
	}

	// Normalize separators to POSIX.
	cleaned := strings.ReplaceAll(raw, "\\", "/")
	cleaned = path.Clean(cleaned)
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("invalid path")
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("absolute path rejected")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path traversal rejected")
	}
	segments := strings.Split(cleaned, "/")
	if len(segments) > maxPathDepth {
		return "", fmt.Errorf("path depth exceeds limit")
	}
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("invalid path segment")
		}
	}
	if len(cleaned) > maxPathLength {
		return "", fmt.Errorf("path length exceeds limit")
	}
	return cleaned, nil
}
