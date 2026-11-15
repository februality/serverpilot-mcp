//go:build !windows

package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	xssh "golang.org/x/crypto/ssh"
)

// startLocalSSH runs an in-process SSH server on 127.0.0.1 that serves the
// sftp subsystem against the local filesystem and runs exec requests with
// /bin/sh. It lets the SFTP/exec code paths run in CI without a real host.
func startLocalSSH(t *testing.T) (*Ops, Target) {
	t.Helper()
	return startLocalSSHMode(t, false, 5000)
}

// startLocalSSHMode: with stall, the server completes the handshake but never
// answers channel-open requests (a wedged sshd / half-open link).
func startLocalSSHMode(t *testing.T, stall bool, timeoutMs int) (*Ops, Target) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "client_key")
	if _, err := EnsureKeyPair(keyPath, "test"); err != nil {
		t.Fatal(err)
	}
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, err := xssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &xssh.ServerConfig{
		PublicKeyCallback: func(xssh.ConnMetadata, xssh.PublicKey) (*xssh.Permissions, error) { return nil, nil },
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if stall {
				go stallConn(conn, cfg)
			} else {
				go serveConn(conn, cfg)
			}
		}
	}()

	pool := NewPool(keyPath, timeoutMs, NewHostKeyVerifier(filepath.Join(dir, "known_hosts"), true))
	pool.port = ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(pool.Close)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewOps(pool, false), Target{Host: "127.0.0.1", User: "tester", Base: base}
}

func stallConn(conn net.Conn, cfg *xssh.ServerConfig) {
	_, chans, reqs, err := xssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go xssh.DiscardRequests(reqs)
	for range chans { // never Accept or Reject
	}
}

func serveConn(conn net.Conn, cfg *xssh.ServerConfig) {
	_, chans, reqs, err := xssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go xssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(xssh.UnknownChannelType, "")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go serveSession(ch, chReqs)
	}
}

func serveSession(ch xssh.Channel, reqs <-chan *xssh.Request) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for req := range reqs {
		payload := string(req.Payload[4:]) // single SSH string
		switch req.Type {
		case "subsystem":
			_ = req.Reply(payload == "sftp", nil)
			go func() {
				srv, _ := sftp.NewServer(ch)
				_ = srv.Serve()
				_ = ch.Close()
			}()
		case "exec":
			_ = req.Reply(true, nil)
			go func() {
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", payload)
				cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
				code := 0
				if err := cmd.Run(); err != nil {
					var ee *exec.ExitError
					if errors.As(err, &ee) {
						code = ee.ExitCode()
					} else {
						code = 255
					}
				}
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, uint32(code))
				_, _ = ch.SendRequest("exit-status", false, status)
				_ = ch.Close()
			}()
		default:
			_ = req.Reply(false, nil)
		}
	}
	// Channel closed by the client: kill a still-running command.
}

func TestLocalSSH_WriteIsAtomicAndKeepsMode(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/wp-config.php"
	if err := os.WriteFile(p, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	if err := ops.WriteFile(tg, p, "new content", false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if os.SameFile(before, after) {
		t.Error("file was rewritten in place, not replaced via temp file + rename")
	}
	got, _ := os.ReadFile(p)
	st, _ := os.Stat(p)
	if string(got) != "new content" || st.Mode().Perm() != 0o640 {
		t.Errorf("content=%q mode=%o", got, st.Mode().Perm())
	}
	entries, _ := os.ReadDir(tg.Base)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".sp-tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestLocalSSH_WriteCreatesDirs(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/a/b/c.txt"
	if err := ops.WriteFile(tg, p, "x", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "x" {
		t.Errorf("got %q", got)
	}
}

func TestLocalSSH_WriteThroughSymlinkKeepsLink(t *testing.T) {
	ops, tg := startLocalSSH(t)
	real := tg.Base + "/real.txt"
	link := tg.Base + "/link.txt"
	_ = os.WriteFile(real, []byte("a"), 0o644)
	_ = os.Symlink(real, link)
	if err := ops.WriteFile(tg, link, "b", false); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced by a regular file")
	}
	if got, _ := os.ReadFile(real); string(got) != "b" {
		t.Errorf("target = %q", got)
	}
}

func TestLocalSSH_EditFile(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/f.php"
	_ = os.WriteFile(p, []byte("define('WP_DEBUG', false);\n"), 0o600)
	n, err := ops.EditFile(tg, p, "false", "true", false)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got, _ := os.ReadFile(p); string(got) != "define('WP_DEBUG', true);\n" {
		t.Errorf("got %q", got)
	}
	if _, err := ops.EditFile(tg, p, "nope", "x", false); err == nil {
		t.Error("expected not-found error")
	}
}

func TestLocalSSH_ReadFileModes(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/log.txt"
	var b strings.Builder
	for i := 1; i <= 5000; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n")
	}
	_ = os.WriteFile(p, []byte(b.String()), 0o644)

	head, note, err := ops.ReadFile(tg, p, ReadOptions{})
	if err != nil || !strings.HasPrefix(b.String(), head) || !strings.Contains(note, "3000 more lines. Pass offset=2001") {
		t.Errorf("default cap: err=%v note=%q", err, note)
	}
	tail, note, err := ops.ReadFile(tg, p, ReadOptions{Tail: 3})
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	want := strings.Join(lines[len(lines)-3:], "\n") + "\n"
	if err != nil || tail != want || !strings.Contains(note, "last 3 lines") {
		t.Errorf("tail: err=%v got %q / %q", err, tail, note)
	}

	bin := tg.Base + "/img.png"
	_ = os.WriteFile(bin, []byte{0x89, 'P', 'N', 'G', 0, 0, 1}, 0o644)
	if _, _, err := ops.ReadFile(tg, bin, ReadOptions{}); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Errorf("binary: %v", err)
	}
	if _, _, err := ops.ReadFile(tg, tg.Base, ReadOptions{}); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("dir: %v", err)
	}
}

func TestLocalSSH_ListDirLimit(t *testing.T) {
	ops, tg := startLocalSSH(t)
	for _, n := range []string{"b.txt", "a.txt", "c.txt"} {
		_ = os.WriteFile(tg.Base+"/"+n, nil, 0o644)
	}
	_ = os.Mkdir(tg.Base+"/zdir", 0o755)
	entries, total, err := ops.ListDir(tg, tg.Base, false, 2)
	if err != nil || total != 4 || len(entries) != 2 || entries[0].Name != "zdir" || entries[1].Name != "a.txt" {
		t.Errorf("entries=%+v total=%d err=%v", entries, total, err)
	}
}

func TestLocalSSH_DownloadUpload(t *testing.T) {
	ops, tg := startLocalSSH(t)
	data := make([]byte, 300_000)
	_, _ = rand.Read(data)
	remote := tg.Base + "/backup.bin"
	_ = os.WriteFile(remote, data, 0o644)

	local := filepath.Join(t.TempDir(), "sub", "backup.bin")
	n, err := ops.Download(tg, remote, local)
	if err != nil || n != int64(len(data)) {
		t.Fatalf("download n=%d err=%v", n, err)
	}
	if got, _ := os.ReadFile(local); string(got) != string(data) {
		t.Error("downloaded bytes differ")
	}

	up := tg.Base + "/up/copy.bin"
	if _, err := ops.Upload(tg, local, up, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(up); string(got) != string(data) {
		t.Error("uploaded bytes differ")
	}
}

func TestLocalSSH_Exec(t *testing.T) {
	ops, tg := startLocalSSH(t)
	res, err := ops.Exec(context.Background(), tg, "echo out; echo err >&2; exit 3", ExecOptions{Cwd: tg.Base})
	if err != nil || res.Stdout != "out\n" || res.Stderr != "err\n" || res.ExitCode != 3 || res.Truncated {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	res, err = ops.Exec(context.Background(), tg, "head -c 200000 /dev/zero | tr '\\0' a; echo; echo THE-END", ExecOptions{})
	if err != nil || !res.Truncated || !strings.HasSuffix(res.Stdout, "THE-END\n") || len(res.Stdout) > 50_000 {
		t.Fatalf("big output: truncated=%v len=%d err=%v", res.Truncated, len(res.Stdout), err)
	}

	start := time.Now()
	res, err = ops.Exec(context.Background(), tg, "echo started; sleep 5", ExecOptions{TimeoutMs: 1000})
	if !errors.Is(err, ErrExecTimeout) || res == nil || res.Stdout != "started\n" || time.Since(start) > 4*time.Second {
		t.Fatalf("timeout: res=%+v err=%v elapsed=%v", res, err, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	_, err = ops.Exec(ctx, tg, "sleep 5", ExecOptions{})
	if !errors.Is(err, ErrExecCancelled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestLocalSSH_WriteFallsBackInPlaceWhenDirNotWritable(t *testing.T) {
	ops, tg := startLocalSSH(t)
	dir := tg.Base + "/ro"
	_ = os.Mkdir(dir, 0o755)
	p := dir + "/f.txt"
	_ = os.WriteFile(p, []byte("old"), 0o644)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so the fallback can't be forced")
	}
	_ = os.Chmod(dir, 0o555) // temp file can't be created; file itself is writable
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	before, _ := os.Stat(p)
	if err := ops.WriteFile(tg, p, "new", false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Errorf("got %q", got)
	}
	if !os.SameFile(before, after) {
		t.Error("expected the in-place fallback (same inode), but the file was replaced")
	}
}

func TestLocalSSH_ExecAbortsWhenCwdMissing(t *testing.T) {
	ops, tg := startLocalSSH(t)
	marker := tg.Base + "/should-not-exist"
	res, err := ops.Exec(context.Background(), tg, "true; touch "+ShellEscape(marker), ExecOptions{Cwd: tg.Base + "/missing"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Error("expected non-zero exit when cd fails")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("command after ';' ran even though cd failed")
	}
}

func TestPool_ReaperSkipsInUseEntries(t *testing.T) {
	ops, tg := startLocalSSH(t)
	client, release, err := ops.pool.Get(tg.Host, tg.User)
	if err != nil {
		t.Fatal(err)
	}
	key := poolKey(tg.Host, tg.User)
	ops.pool.mu.Lock()
	ops.pool.entries[key].lastUsed = time.Now().Add(-time.Hour) // long-running op
	ops.pool.mu.Unlock()
	ops.pool.cleanup()
	if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil && strings.Contains(err.Error(), "closed") {
		t.Fatal("reaper closed a connection that was still in use")
	}
	ops.pool.mu.Lock()
	_, stillPooled := ops.pool.entries[key]
	ops.pool.mu.Unlock()
	if !stillPooled {
		t.Fatal("in-use entry was reaped")
	}

	release()
	ops.pool.mu.Lock()
	ops.pool.entries[key].lastUsed = time.Now().Add(-time.Hour)
	ops.pool.mu.Unlock()
	ops.pool.cleanup()
	ops.pool.mu.Lock()
	_, stillPooled = ops.pool.entries[key]
	ops.pool.mu.Unlock()
	if stillPooled {
		t.Error("idle entry should be reaped after release")
	}
}

type osLinks struct{}

func (osLinks) Lstat(p string) (os.FileInfo, error) { return os.Lstat(p) }
func (osLinks) ReadLink(p string) (string, error)   { return os.Readlink(p) }

func TestResolveLinks(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	_ = os.MkdirAll(root+"/a/b", 0o755)
	_ = os.Symlink(root+"/a", root+"/abs")    // absolute target
	_ = os.Symlink("a/b", root+"/rel")        // relative target
	_ = os.Symlink("../../etc", root+"/a/up") // relative, climbs out
	_ = os.Symlink(root+"/loop2", root+"/loop1")
	_ = os.Symlink(root+"/loop1", root+"/loop2")
	cases := map[string]string{
		root + "/a/b":         root + "/a/b",
		root + "/abs/b":       root + "/a/b",
		root + "/rel/new.txt": root + "/a/b/new.txt", // missing tail kept lexically
		root + "/a/up/passwd": pathpkg.Join(pathpkg.Dir(root), "etc/passwd"),
		root + "/missing/x/y": root + "/missing/x/y",
	}
	for in, want := range cases {
		got, err := resolveLinks(osLinks{}, in)
		if err != nil || got != want {
			t.Errorf("resolveLinks(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := resolveLinks(osLinks{}, root+"/loop1/x"); err == nil {
		t.Error("symlink loop should error")
	}
}

func TestLocalSSH_StrictPathsRefusesSymlinkEscape(t *testing.T) {
	ops, tg := startLocalSSH(t)
	ops.strictPaths = true
	outside := t.TempDir()
	_ = os.WriteFile(outside+"/secret", []byte("TOPSECRET"), 0o644)
	_ = os.Symlink(outside, tg.Base+"/escape")
	_ = os.WriteFile(tg.Base+"/ok.txt", []byte("fine"), 0o644)

	if out, _, err := ops.ReadFile(tg, tg.Base+"/escape/secret", ReadOptions{}); err == nil || strings.Contains(out, "TOPSECRET") {
		t.Errorf("read through escaping symlink: out=%q err=%v", out, err)
	}
	if _, _, err := ops.ListDir(tg, tg.Base+"/escape", false, 0); err == nil {
		t.Error("list through escaping symlink should be refused")
	}
	if _, err := ops.Download(tg, tg.Base+"/escape/secret", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("download through escaping symlink should be refused")
	}
	if out, _, err := ops.ReadFile(tg, tg.Base+"/ok.txt", ReadOptions{}); err != nil || out != "fine" {
		t.Errorf("normal read: %q %v", out, err)
	}
	// A symlink that stays inside the base is fine.
	_ = os.Symlink(tg.Base+"/ok.txt", tg.Base+"/inside")
	if out, _, err := ops.ReadFile(tg, tg.Base+"/inside", ReadOptions{}); err != nil || out != "fine" {
		t.Errorf("inside symlink: %q %v", out, err)
	}
}

func TestLocalSSH_StalledConnectionTimesOut(t *testing.T) {
	ops, tg := startLocalSSHMode(t, true, 300)
	start := time.Now()
	_, err := ops.Exec(context.Background(), tg, "true", ExecOptions{})
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("expected a stall error, got %v", err)
	}
	if _, _, err := ops.ReadFile(tg, tg.Base+"/x", ReadOptions{}); err == nil {
		t.Fatal("expected the SFTP open to fail too")
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("took %v; stalled opens must be bounded by the pool timeout", el)
	}
	ops.pool.mu.Lock()
	n := len(ops.pool.entries)
	ops.pool.mu.Unlock()
	if n != 0 {
		t.Errorf("stalled connection still pooled (%d entries)", n)
	}
}

func TestLocalSSH_RefusesReadOnlyFile(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/wp-config.php"
	_ = os.WriteFile(p, []byte("locked"), 0o400)
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	err := ops.WriteFile(tg, p, "changed", false)
	if err == nil || !strings.Contains(err.Error(), "read-only") || !strings.Contains(err.Error(), "chmod u+w") {
		t.Fatalf("expected a read-only refusal, got %v", err)
	}
	if _, err := ops.EditFile(tg, p, "locked", "x", false); err == nil {
		t.Error("EditFile should refuse too")
	}
	if got, _ := os.ReadFile(p); string(got) != "locked" {
		t.Errorf("content changed: %q", got)
	}
}

func TestLocalSSH_TempFileIsPrivateWhileWriting(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/secret.php"
	_ = os.WriteFile(p, []byte("old"), 0o640)
	c, err := ops.sftp(tg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var seen os.FileMode
	open := func() (io.ReadCloser, error) {
		matches, _ := filepath.Glob(tg.Base + "/.secret.php.sp-tmp-*")
		if len(matches) == 1 {
			if fi, err := os.Stat(matches[0]); err == nil {
				seen = fi.Mode().Perm()
			}
		}
		return io.NopCloser(strings.NewReader("DB_PASSWORD=hunter2")), nil
	}
	if _, err := writeAtomic(c, p, open); err != nil {
		t.Fatal(err)
	}
	if seen != 0o600 {
		t.Errorf("temp file mode while holding content = %o, want 600", seen)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
		t.Errorf("final mode = %o, want 640", st.Mode().Perm())
	}
}

func TestLocalSSH_WriteKeepsSetgid(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/tool.sh"
	_ = os.WriteFile(p, []byte("old"), 0o755)
	if err := os.Chmod(p, 0o755|os.ModeSetgid); err != nil {
		t.Skip("can't set setgid here:", err)
	}
	if st, _ := os.Stat(p); st.Mode()&os.ModeSetgid == 0 {
		t.Skip("filesystem dropped setgid")
	}
	if err := ops.WriteFile(tg, p, "new", false); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode()&os.ModeSetgid == 0 || st.Mode().Perm() != 0o755 {
		t.Errorf("mode after write = %v", st.Mode())
	}
}

func TestLocalSSH_NewFileGetsDefaultMode(t *testing.T) {
	ops, tg := startLocalSSH(t)
	p := tg.Base + "/fresh.txt"
	if err := ops.WriteFile(tg, p, "x", false); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() == 0o600 {
		t.Error("new file kept the private temp mode instead of the server default")
	}
}
