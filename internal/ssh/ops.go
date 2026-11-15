package ssh

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	xssh "golang.org/x/crypto/ssh"

	"github.com/februality/serverpilot-mcp/internal/sandbox"
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	// Truncated reports that stdout and/or stderr exceeded the output budget
	// and the middle was replaced with an "[N bytes omitted]" marker.
	Truncated bool
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

// Target is where an operation runs: the server, the system user to log in
// as, and the sandbox root (the sysuser's home) that paths must stay inside.
type Target struct {
	Host string
	User string
	Base string
}

type Ops struct {
	pool *Pool
	// strictPaths additionally resolves symlinks server-side and rejects paths
	// whose real location is outside Target.Base. Enabled in read-only mode,
	// where the sandbox is the only boundary (site_exec is not available).
	strictPaths bool
}

func NewOps(pool *Pool, strictPaths bool) *Ops { return &Ops{pool: pool, strictPaths: strictPaths} }

// ExecOptions controls Exec. Cwd is wrapped via ShellEscape.
type ExecOptions struct {
	Cwd       string
	TimeoutMs int
}

// ErrExecTimeout and ErrExecCancelled are returned alongside a non-nil
// *ExecResult holding whatever output arrived before the command was killed.
var (
	ErrExecTimeout   = errors.New("command timed out")
	ErrExecCancelled = errors.New("command cancelled by the client")
	ErrExecNoStatus  = errors.New("connection closed before the command reported an exit status; it may have been killed or may still be running")
)

func (o *Ops) Exec(ctx context.Context, t Target, command string, opts ExecOptions) (*ExecResult, error) {
	v, release, err := o.openChannel(ctx, t, "session", func(c *xssh.Client) (io.Closer, error) { return c.NewSession() })
	if err != nil {
		return nil, err
	}
	defer release()
	session := v.(*xssh.Session)
	defer session.Close()

	full := command
	if opts.Cwd != "" {
		// "cd X && a; b" would still run b (in the home directory) when cd
		// fails, so abort the whole command instead.
		full = "cd " + ShellEscape(opts.Cwd) + " || exit 1\n" + command
	}

	stdout := newHeadTail(stdoutHeadBytes, stdoutTailBytes)
	stderr := newHeadTail(stderrHeadBytes, stderrTailBytes)
	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Start(full); err != nil {
		return nil, fmt.Errorf("ssh start: %w", err)
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- session.Wait() }()

	// A separate timer (not a context deadline) so a deadline on the
	// caller's ctx isn't misreported as this command's timeout.
	var timeoutCh <-chan time.Time
	if opts.TimeoutMs > 0 {
		timer := time.NewTimer(time.Duration(opts.TimeoutMs) * time.Millisecond)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	result := func(exit int) *ExecResult {
		return &ExecResult{
			Stdout:    stdout.String(),
			Stderr:    stderr.String(),
			ExitCode:  exit,
			Truncated: stdout.Truncated() || stderr.Truncated(),
		}
	}

	select {
	case werr := <-doneCh:
		exitCode := 0
		if werr != nil {
			var exitErr *xssh.ExitError
			var missing *xssh.ExitMissingError
			switch {
			case errors.As(werr, &exitErr):
				exitCode = exitErr.ExitStatus()
			case errors.As(werr, &missing):
				// The channel closed without an exit status (connection
				// dropped, or the command was killed by a signal the server
				// didn't report). Don't present this as success.
				return result(-1), fmt.Errorf("%w: %s", ErrExecNoStatus, command)
			default:
				return nil, werr
			}
		}
		return result(exitCode), nil
	case <-timeoutCh:
		_ = session.Signal(xssh.SIGKILL)
		_ = session.Close()
		return result(-1), fmt.Errorf("%w after %dms: %s", ErrExecTimeout, opts.TimeoutMs, command)
	case <-ctx.Done():
		_ = session.Signal(xssh.SIGKILL)
		_ = session.Close()
		return result(-1), fmt.Errorf("%w: %s", ErrExecCancelled, command)
	}
}

// sftpClient wraps an SFTP session so Close also releases the pooled SSH
// connection it runs on.
type sftpClient struct {
	*sftp.Client
	release func()
}

func (c *sftpClient) Close() error {
	err := c.Client.Close()
	c.release()
	return err
}

func (o *Ops) sftp(t Target) (*sftpClient, error) {
	v, release, err := o.openChannel(context.Background(), t, "sftp session", func(c *xssh.Client) (io.Closer, error) { return sftp.NewClient(c) })
	if err != nil {
		return nil, err
	}
	return &sftpClient{Client: v.(*sftp.Client), release: release}, nil
}

// openChannel opens a session-type channel (exec session or SFTP) on the
// pooled client for t, with a deadline. A connection can look alive yet never
// answer a channel-open (wedged sshd, half-open TCP after a NAT drop); without
// a deadline that call, and every later one reusing the same pooled client,
// would hang forever. On a stall the client is evicted and
// closed (which also unblocks the stuck open) and one retry is made on a
// fresh connection. A failed open is retried only if the connection turns out
// to be dead. The returned release must be called when done.
func (o *Ops) openChannel(ctx context.Context, t Target, what string, open func(*xssh.Client) (io.Closer, error)) (io.Closer, func(), error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		client, release, err := o.pool.Get(t.Host, t.User)
		if err != nil {
			return nil, nil, err
		}
		type result struct {
			v   io.Closer
			err error
		}
		done := make(chan result, 1)
		go func() {
			v, err := open(client)
			done <- result{v, err}
		}()
		// If we stop waiting, close whatever the open eventually returns.
		abandon := func() {
			go func() {
				if r := <-done; r.err == nil {
					_ = r.v.Close()
				}
			}()
		}
		timer := time.NewTimer(o.pool.openTimeout())
		select {
		case r := <-done:
			timer.Stop()
			if r.err == nil {
				return r.v, release, nil
			}
			release()
			lastErr = fmt.Errorf("ssh %s: %w", what, r.err)
			// Only evict (and retry) if the connection itself is dead; a live
			// connection may be shared with other in-flight operations, and a
			// refusal such as "too many sessions" won't be fixed by retrying.
			if _, _, perr := client.SendRequest("keepalive@openssh.com", true, nil); perr == nil {
				return nil, nil, lastErr
			}
			o.pool.Evict(t.Host, t.User, client)
		case <-timer.C:
			release()
			abandon()
			o.pool.Evict(t.Host, t.User, client)
			lastErr = fmt.Errorf("ssh %s to %s@%s stalled for %s; the connection was reset", what, t.User, t.Host, o.pool.openTimeout())
		case <-ctx.Done():
			timer.Stop()
			release()
			abandon()
			return nil, nil, fmt.Errorf("%w: %v", ErrExecCancelled, ctx.Err())
		}
	}
	return nil, nil, lastErr
}

// contained enforces strictPaths: with symlinks resolved, the path must still
// be inside the sandbox root. Resolution is done client-side (see
// resolveLinks) rather than with SFTP REALPATH, whose symlink handling varies
// by server implementation. Fails closed on lookup errors other than "does
// not exist" (a missing tail is resolved lexically; the operation itself will
// then fail or create it).
func (o *Ops) contained(c *sftpClient, t Target, path string) error {
	if !o.strictPaths || t.Base == "" {
		return nil
	}
	realBase, err := resolveLinks(c, t.Base)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", t.Base, err)
	}
	real, err := resolveLinks(c, path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	if _, err := sandbox.ValidatePath(realBase, real); err != nil {
		return fmt.Errorf("path %q resolves through a symlink to %s, outside the allowed directory: %s", path, real, t.Base)
	}
	return nil
}

// linkReader is the subset of *sftp.Client that resolveLinks needs, so it can
// be unit-tested against the local filesystem.
type linkReader interface {
	Lstat(p string) (os.FileInfo, error)
	ReadLink(p string) (string, error)
}

const maxSymlinkHops = 40

// resolveLinks returns p with every symlink in it resolved, walking one
// component at a time with Lstat/ReadLink. Components that don't exist are
// appended lexically.
func resolveLinks(fs linkReader, p string) (string, error) {
	parts := splitPath(p)
	cur := "/"
	hops := 0
	for i := 0; i < len(parts); i++ {
		next := pathpkg.Join(cur, parts[i])
		fi, err := fs.Lstat(next)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return pathpkg.Join(append([]string{next}, parts[i+1:]...)...), nil
			}
			return "", err
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			cur = next
			continue
		}
		if hops++; hops > maxSymlinkHops {
			return "", fmt.Errorf("too many levels of symbolic links in %s", p)
		}
		target, err := fs.ReadLink(next)
		if err != nil {
			return "", err
		}
		if !pathpkg.IsAbs(target) {
			target = pathpkg.Join(cur, target)
		}
		parts = append(splitPath(target), parts[i+1:]...)
		cur, i = "/", -1
	}
	return cur, nil
}

func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(pathpkg.Clean("/"+p), "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ReadFile returns the requested part of a text file and a note for the model
// (empty when the whole file was returned unchanged). The note is never part
// of content.
func (o *Ops) ReadFile(t Target, path string, opts ReadOptions) (content, note string, err error) {
	c, err := o.sftp(t)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = c.Close() }()
	if err := o.contained(c, t, path); err != nil {
		return "", "", err
	}
	f, err := c.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	if st.IsDir() {
		return "", "", fmt.Errorf("Failed to read %s: it is a directory (use site_list_files)", path)
	}
	size := st.Size()

	sniff := make([]byte, 8192)
	n, err := io.ReadFull(f, sniff)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	if isBinary(sniff[:n]) {
		return "", "", fmt.Errorf("%s is a binary file (%d bytes) and was not returned. "+
			"Use site_download_file to copy it to this machine, or site_exec (e.g. `file`, `xxd | head`) to inspect it", path, size)
	}

	if opts.Tail > 0 {
		start := max(size-maxTailScan, 0)
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return "", "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
		}
		data, err := io.ReadAll(f)
		if err != nil {
			return "", "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
		}
		content, note := tailLines(data, start > 0, opts.Tail, opts.MaxBytes, size)
		return content, note, nil
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", "", fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	return readLines(bufio.NewReaderSize(f, 64<<10), size, opts)
}

// WriteFile replaces path with content atomically (see writeAtomic).
func (o *Ops) WriteFile(t Target, path, content string, createDirs bool) error {
	c, err := o.sftp(t)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if createDirs {
		if err := c.MkdirAll(remoteDir(path)); err != nil {
			return fmt.Errorf("Failed to write %s: create parent directories: %s", path, err.Error())
		}
	}
	_, err = writeAtomic(c, path, func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content)), nil
	})
	return err
}

// EditFile applies an exact-string replacement (see applyEdit) and writes the
// result atomically. Returns the number of replacements made.
func (o *Ops) EditFile(t Target, path, oldStr, newStr string, replaceAll bool) (int, error) {
	c, err := o.sftp(t)
	if err != nil {
		return 0, err
	}
	defer func() { _ = c.Close() }()
	if err := o.contained(c, t, path); err != nil {
		return 0, err
	}
	st, err := c.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	if st.IsDir() {
		return 0, fmt.Errorf("Failed to read %s: it is a directory", path)
	}
	if st.Size() > MaxEditBytes {
		return 0, fmt.Errorf("%s is %d bytes; site_edit_file handles files up to %d bytes. Use site_exec (e.g. sed) instead",
			path, st.Size(), MaxEditBytes)
	}
	f, err := c.Open(path)
	if err != nil {
		return 0, fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return 0, fmt.Errorf("Failed to read %s: %s", path, err.Error())
	}
	if isBinary(data[:min(len(data), 8192)]) {
		return 0, fmt.Errorf("%s is a binary file; site_edit_file only edits text", path)
	}
	updated, n, err := applyEdit(string(data), oldStr, newStr, replaceAll, path)
	if err != nil {
		return 0, err
	}
	if _, err := writeAtomic(c, path, func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(updated)), nil
	}); err != nil {
		return 0, err
	}
	return n, nil
}

// writeAtomic writes to a temp file beside path and renames it over the
// original, so a failed or interrupted write never leaves a half-written file
// (a truncated wp-config.php takes the site down).
//
// It deliberately behaves like an in-place write wherever the two differ:
//   - a file without the owner write bit is refused (on main, c.Create failed
//     there; rename would silently bypass a deliberate lock like chmod 400);
//   - the temp file is 0600 while it holds the new content, then gets the
//     original permission bits (including setuid/setgid/sticky);
//   - the original owner/group is copied; if that isn't allowed, it falls
//     back to an in-place write so a group-readable file stays readable;
//   - symlinks are written through in place so the link itself survives.
//
// SFTP v3 can't report hard-link counts, so a hard-linked file is replaced
// rather than updated through the link. If the server lacks posix-rename or
// the directory isn't writable, it falls back to an in-place write. open is
// called again for that fallback, so it must return a fresh reader each time.
func writeAtomic(c *sftpClient, path string, open func() (io.ReadCloser, error)) (int64, error) {
	fail := func(err error) (int64, error) {
		return 0, fmt.Errorf("Failed to write %s: %s", path, err.Error())
	}
	existed := false
	var mode os.FileMode
	var owner *sftp.FileStat
	if st, err := c.Lstat(path); err == nil {
		if st.IsDir() {
			return fail(errors.New("it is a directory"))
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return writeInPlace(c, path, open)
		}
		if st.Mode().Perm()&0o200 == 0 {
			return fail(fmt.Errorf("the file is read-only (mode %04o), which usually means it was locked on purpose "+
				"(e.g. a hardened wp-config.php). If the change is intended, make it writable first "+
				"(site_exec `chmod u+w <file>`), write it, then restore the mode", st.Mode().Perm()))
		}
		existed = true
		mode = st.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
		owner, _ = st.Sys().(*sftp.FileStat)
	}

	tmp := remoteDir(path) + "/." + remoteBase(path) + ".sp-tmp-" + randHex(6)
	f, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return writeInPlace(c, path, open)
	}
	discard := func() { _ = f.Close(); _ = c.Remove(tmp) }
	// Remember the server's default mode for new files, then lock the temp
	// file down before it holds any content.
	var defaultMode os.FileMode
	var tmpOwner *sftp.FileStat
	if fi, err := f.Stat(); err == nil {
		defaultMode = fi.Mode().Perm()
		tmpOwner, _ = fi.Sys().(*sftp.FileStat)
	}
	if err := c.Chmod(tmp, 0o600); err != nil {
		discard()
		return writeInPlace(c, path, open)
	}
	if existed && owner != nil && tmpOwner != nil && (owner.UID != tmpOwner.UID || owner.GID != tmpOwner.GID) {
		if err := c.Chown(tmp, int(owner.UID), int(owner.GID)); err != nil {
			discard()
			return writeInPlace(c, path, open)
		}
	}

	src, err := open()
	if err != nil {
		discard()
		return fail(err)
	}
	n, err := io.Copy(f, src)
	_ = src.Close()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = c.Remove(tmp)
		return fail(err)
	}
	final := mode
	if !existed {
		final = defaultMode
	}
	if final != 0 {
		_ = c.Chmod(tmp, final)
	}
	if err := c.PosixRename(tmp, path); err != nil {
		if !existed {
			if rerr := c.Rename(tmp, path); rerr == nil {
				return n, nil
			}
		}
		_ = c.Remove(tmp)
		return writeInPlace(c, path, open)
	}
	return n, nil
}

func writeInPlace(c *sftpClient, path string, open func() (io.ReadCloser, error)) (int64, error) {
	src, err := open()
	if err != nil {
		return 0, fmt.Errorf("Failed to write %s: %s", path, err.Error())
	}
	defer src.Close()
	f, err := c.Create(path)
	if err != nil {
		return 0, fmt.Errorf("Failed to write %s: %s", path, err.Error())
	}
	n, err := io.Copy(f, src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, fmt.Errorf("Failed to write %s: %s", path, err.Error())
	}
	return n, nil
}

// ListDir lists path. At most limit entries are returned (directories first,
// then by name); total is the count before the limit was applied.
func (o *Ops) ListDir(t Target, path string, showHidden bool, limit int) (entries []FileEntry, total int, err error) {
	c, err := o.sftp(t)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = c.Close() }()
	if err := o.contained(c, t, path); err != nil {
		return nil, 0, err
	}
	infos, err := c.ReadDir(path)
	if err != nil {
		return nil, 0, fmt.Errorf("Failed to list %s: %s", path, err.Error())
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
	total = len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

// Download copies a remote file to localPath via a ".part" file and rename,
// so an interrupted transfer never leaves a truncated file at localPath.
func (o *Ops) Download(t Target, remotePath, localPath string) (int64, error) {
	c, err := o.sftp(t)
	if err != nil {
		return 0, err
	}
	defer func() { _ = c.Close() }()
	if err := o.contained(c, t, remotePath); err != nil {
		return 0, err
	}
	f, err := c.Open(remotePath)
	if err != nil {
		return 0, fmt.Errorf("Failed to read %s: %s", remotePath, err.Error())
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.IsDir() {
		return 0, fmt.Errorf("%s is a directory. Archive it first with site_exec (e.g. `tar czf ~/archive.tgz -C DIR .`) and download the archive", remotePath)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return 0, fmt.Errorf("create local directory: %w", err)
	}
	// CreateTemp uses O_EXCL with a random name, so a pre-planted symlink
	// in the transfer directory can't redirect the write.
	out, err := os.CreateTemp(filepath.Dir(localPath), "."+filepath.Base(localPath)+".*.part")
	if err != nil {
		return 0, fmt.Errorf("open local file: %w", err)
	}
	part := out.Name()
	n, err := io.Copy(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(part)
		return 0, fmt.Errorf("download %s: %w", remotePath, err)
	}
	if err := os.Rename(part, localPath); err != nil {
		_ = os.Remove(part)
		return 0, fmt.Errorf("finalize local file: %w", err)
	}
	return n, nil
}

// Upload copies a local file to remotePath atomically (see writeAtomic).
func (o *Ops) Upload(t Target, localPath, remotePath string, createDirs bool) (int64, error) {
	st, err := os.Stat(localPath)
	if err != nil {
		return 0, fmt.Errorf("read local file: %w", err)
	}
	if st.IsDir() {
		return 0, fmt.Errorf("%s is a directory; upload a single file (e.g. a .tar.gz) and extract it with site_exec", localPath)
	}
	c, err := o.sftp(t)
	if err != nil {
		return 0, err
	}
	defer func() { _ = c.Close() }()
	if createDirs {
		if err := c.MkdirAll(remoteDir(remotePath)); err != nil {
			return 0, fmt.Errorf("Failed to write %s: create parent directories: %s", remotePath, err.Error())
		}
	}
	return writeAtomic(c, remotePath, func() (io.ReadCloser, error) { return os.Open(localPath) })
}

func remoteDir(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "/"
}

func remoteBase(p string) string { return p[strings.LastIndex(p, "/")+1:] }

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
