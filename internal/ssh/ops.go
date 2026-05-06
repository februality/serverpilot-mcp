package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	xssh "golang.org/x/crypto/ssh"
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type FileType string

const (
	FileTypeFile      FileType = "file"
	FileTypeDirectory FileType = "directory"
	FileTypeSymlink   FileType = "symlink"
	FileTypeOther     FileType = "other"
)

type FileEntry struct {
	Name        string   `json:"name"`
	Type        FileType `json:"type"`
	Size        int64    `json:"size"`
	ModifiedAt  string   `json:"modifiedAt"`
	Permissions string   `json:"permissions"`
}

type Ops struct {
	pool *Pool
}

func NewOps(pool *Pool) *Ops { return &Ops{pool: pool} }

// ExecOptions controls Exec. Cwd is wrapped via ShellEscape.
type ExecOptions struct {
	Cwd       string
	TimeoutMs int
}

func (o *Ops) Exec(host, user, command string, opts ExecOptions) (*ExecResult, error) {
	client, err := o.pool.Get(host, user)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	full := command
	if opts.Cwd != "" {
		full = "cd " + ShellEscape(opts.Cwd) + " && " + command
	}

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Start(full); err != nil {
		return nil, fmt.Errorf("ssh start: %w", err)
	}

	type waitRes struct{ err error }
	doneCh := make(chan waitRes, 1)
	go func() { doneCh <- waitRes{err: session.Wait()} }()

	var ctx context.Context
	var cancel context.CancelFunc
	if opts.TimeoutMs > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(opts.TimeoutMs)*time.Millisecond)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	select {
	case res := <-doneCh:
		exitCode := 0
		if res.err != nil {
			var exitErr *xssh.ExitError
			if errors.As(res.err, &exitErr) {
				exitCode = exitErr.ExitStatus()
			} else {
				var missing *xssh.ExitMissingError
				if !errors.As(res.err, &missing) {
					return nil, res.err
				}
			}
		}
		return &ExecResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: exitCode,
		}, nil
	case <-ctx.Done():
		_ = session.Signal(xssh.SIGKILL)
		return nil, fmt.Errorf("command timed out after %dms: %s", opts.TimeoutMs, command)
	}
}

func (o *Ops) sftp(host, user string) (*sftp.Client, error) {
	client, err := o.pool.Get(host, user)
	if err != nil {
		return nil, err
	}
	c, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("sftp session: %w", err)
	}
	return c, nil
}

func (o *Ops) ReadFile(host, user, path string, maxLines int) (string, error) {
	c, err := o.sftp(host, user)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	f, err := c.Open(path)
	if err != nil {
		return "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	defer func() { _ = f.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		return "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	content := buf.String()
	if maxLines > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > maxLines {
			content = strings.Join(lines[:maxLines], "\n") +
				fmt.Sprintf("\n... (truncated, %d more lines)", len(lines)-maxLines)
		}
	}
	return content, nil
}

func (o *Ops) WriteFile(host, user, path, content string, createDirs bool) error {
	if createDirs {
		dir := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			dir = path[:i]
		}
		if dir != "" {
			if _, err := o.Exec(host, user, "mkdir -p "+ShellEscape(dir), ExecOptions{}); err != nil {
				return err
			}
		}
	}
	c, err := o.sftp(host, user)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	f, err := c.Create(path)
	if err != nil {
		return fmt.Errorf("Failed to write %s: %s", path, err.Error())
	}
	if _, err := f.Write([]byte(content)); err != nil {
		_ = f.Close()
		return fmt.Errorf("Failed to write %s: %s", path, err.Error())
	}
	return f.Close()
}

func (o *Ops) ListDir(host, user, path string, showHidden bool) ([]FileEntry, error) {
	c, err := o.sftp(host, user)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	infos, err := c.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to list %s: %s", path, err.Error())
	}
	out := make([]FileEntry, 0, len(infos))
	for _, info := range infos {
		name := info.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		// pkg/sftp exposes the raw POSIX mode (with type bits) via Sys().
		var rawMode uint32
		if st, ok := info.Sys().(*sftp.FileStat); ok {
			rawMode = st.Mode
		}
		out = append(out, FileEntry{
			Name:        name,
			Type:        classifyMode(rawMode),
			Size:        info.Size(),
			ModifiedAt:  info.ModTime().UTC().Format("2006-01-02T15:04:05.000Z"),
			Permissions: fmt.Sprintf("%03o", rawMode&0o777),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, bj := out[i], out[j]
		if ai.Type == FileTypeDirectory && bj.Type != FileTypeDirectory {
			return true
		}
		if ai.Type != FileTypeDirectory && bj.Type == FileTypeDirectory {
			return false
		}
		return ai.Name < bj.Name
	})
	return out, nil
}

// classifyMode decodes the S_IFMT bits of a POSIX file mode. Mirrors
// src/ssh/operations.ts:getFileType.
func classifyMode(mode uint32) FileType {
	switch mode & 0o170000 {
	case 0o040000:
		return FileTypeDirectory
	case 0o100000:
		return FileTypeFile
	case 0o120000:
		return FileTypeSymlink
	default:
		return FileTypeOther
	}
}
