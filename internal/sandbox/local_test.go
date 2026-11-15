package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLocalPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	ok := map[string]string{
		"backup.sql":                   filepath.Join(root, "backup.sql"),
		"site/db.sql":                  filepath.Join(root, "site/db.sql"),
		filepath.Join(root, "abs.zip"): filepath.Join(root, "abs.zip"),
		"a/../b.txt":                   filepath.Join(root, "b.txt"),
	}
	for in, want := range ok {
		got, err := ValidateLocalPath(root, in)
		if err != nil || got != want {
			t.Errorf("ValidateLocalPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	// An absolute path outside root, valid on every OS ("/etc/passwd" is
	// relative on Windows and would land inside root).
	sysAbs := filepath.Join(filepath.VolumeName(outside)+string(filepath.Separator), "etc", "passwd")
	bad := []string{"", "../escape", "a/../../escape", filepath.Join(outside, "x"), sysAbs, root + "-sibling/x"}
	for _, in := range bad {
		if _, err := ValidateLocalPath(root, in); err == nil {
			t.Errorf("ValidateLocalPath(%q) should be rejected", in)
		}
	}

	// A symlink inside root that points outside must not be followed.
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := ValidateLocalPath(root, "link/stolen.txt"); err == nil {
		t.Error("symlink escape should be rejected")
	}
	// Dangling symlink pointing outside root.
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateLocalPath(root, "dangling"); err == nil {
		t.Error("dangling symlink should be rejected")
	}
	// Dangling symlink as a middle component.
	if err := os.Symlink(filepath.Join(outside, "newdir"), filepath.Join(root, "d")); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateLocalPath(root, "d/x.sql"); err == nil {
		t.Error("path through a dangling middle symlink should be rejected")
	}
}

func TestValidateLocalPath_RootThroughRealPath(t *testing.T) {
	root := t.TempDir() // on macOS: /var/folders/... (a symlink to /private/var/...)
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateLocalPath(root, filepath.Join(real, "x.sql")); err != nil {
		t.Errorf("same directory spelled via its real path was rejected: %v", err)
	}
	if _, err := ValidateLocalPath(real, filepath.Join(root, "x.sql")); err != nil {
		t.Errorf("same directory spelled via its symlinked path was rejected: %v", err)
	}
}
