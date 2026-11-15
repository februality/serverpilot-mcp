package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ValidateLocalPath confines a path on THIS machine to root, the transfer
// directory used by site_download_file / site_upload_file. The MCP server runs
// with the user's full local privileges, so without this a model (or a prompt
// injection in a file it read) could overwrite ~/.zshrc or upload ~/.ssh keys.
//
// Relative paths resolve under root. Absolute paths must already be inside it.
// Symlinks are resolved on the deepest existing ancestor so a link inside root
// can't point the transfer elsewhere.
func ValidateLocalPath(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("local path must not be empty")
	}
	root = filepath.Clean(root)
	var full string
	if filepath.IsAbs(p) {
		full = filepath.Clean(p)
	} else {
		full = filepath.Join(root, p)
	}
	outside := func() error {
		return fmt.Errorf("local path %q is outside the transfer directory %s (relative paths resolve inside it; set SP_TRANSFER_DIR to use a different directory)", p, root)
	}
	realRoot, err := resolveExisting(root)
	if err != nil {
		return "", err
	}
	realFull, err := resolveExisting(full)
	if err != nil {
		if errors.Is(err, errDanglingLink) {
			return "", fmt.Errorf("local path %q goes through a broken symlink", p)
		}
		return "", err
	}
	// The real (symlink-resolved) location is what counts. Checking it alone
	// also accepts the root spelled through its real path (macOS /var →
	// /private/var), while lexical tricks like ".." are already gone after
	// Clean/Join.
	if !within(realRoot, realFull) {
		return "", outside()
	}
	return full, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

var errDanglingLink = errors.New("dangling symlink")

// resolveExisting evaluates symlinks on the longest existing prefix of p and
// re-appends the components that don't exist yet. A component that exists as
// a symlink to a missing target is an error: its target is unknown territory,
// and creating through it (MkdirAll, OpenFile) would land wherever it points.
func resolveExisting(p string) (string, error) {
	var rest []string
	cur := p
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{real}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", errDanglingLink
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

// EnsureDir creates root (mode 0700) if it doesn't exist.
func EnsureDir(root string) error { return os.MkdirAll(root, 0o700) }
