package ssh

import (
	"bufio"
	"strings"
	"testing"
)

func read(t *testing.T, content string, opts ReadOptions) (string, string) {
	t.Helper()
	out, note, err := readLines(bufio.NewReaderSize(strings.NewReader(content), 16), int64(len(content)), opts)
	if err != nil {
		t.Fatal(err)
	}
	return out, note
}

func TestReadLines_WholeSmallFileIsByteExact(t *testing.T) {
	for _, in := range []string{"", "a", "a\n", "a\nb", "a\r\nb\r\n", "x\n\n\ny\n"} {
		if got, note := read(t, in, ReadOptions{}); got != in || note != "" {
			t.Errorf("read(%q) = %q, note %q", in, got, note)
		}
	}
}

func TestReadLines_LimitAndOffset(t *testing.T) {
	in := "1\n2\n3\n4\n5\n"
	got, note := read(t, in, ReadOptions{Limit: 2})
	if got != "1\n2\n" || !strings.Contains(note, "showing lines 1-2; 3 more lines. Pass offset=3") {
		t.Errorf("limit: %q / %q", got, note)
	}
	got, note = read(t, in, ReadOptions{Offset: 3, Limit: 2})
	if got != "3\n4\n" || !strings.Contains(note, "showing lines 3-4; 1 more lines. Pass offset=5") {
		t.Errorf("offset: %q / %q", got, note)
	}
	if got, note := read(t, in, ReadOptions{Offset: 4}); got != "4\n5\n" || note != "" {
		t.Errorf("offset to end: %q / %q", got, note)
	}
	for _, off := range []int{6, 9} { // 6 = exactly one past the last line
		if _, _, err := readLines(bufio.NewReader(strings.NewReader(in)), 10, ReadOptions{Offset: off}); err == nil {
			t.Errorf("offset %d past EOF should error", off)
		}
	}
	if got, note := read(t, "", ReadOptions{Offset: 1}); got != "" || note != "" {
		t.Errorf("empty file: %q", got)
	}
}

func TestReadLines_ByteBudgetStopsAtLineBoundary(t *testing.T) {
	in := strings.Repeat("abcdefghi\n", 10) // 10 lines x 10 bytes
	got, note := read(t, in, ReadOptions{MaxBytes: 35})
	if got != strings.Repeat("abcdefghi\n", 3) || !strings.Contains(note, "7 more lines. Pass offset=4") {
		t.Errorf("got %q / %q", got, note)
	}
}

func TestReadLines_HugeSingleLineIsCutWithoutDraining(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	r := strings.NewReader(huge + "\nnext\n")
	out, note, err := readLines(bufio.NewReaderSize(r, 4096), int64(r.Len()), ReadOptions{MaxBytes: 100})
	if err != nil || out != strings.Repeat("x", 100) || !strings.Contains(note, "line 1 is longer") || !strings.Contains(note, "offset=2") {
		t.Errorf("got %q / %q / %v", out[:min(len(out), 120)], note, err)
	}
	if consumed := 5_000_006 - r.Len(); consumed > 64_000 {
		t.Errorf("read %d bytes of the source to show 100; it should stop at the cut", consumed)
	}
}

func TestReadLines_PendingLineCountedOnce(t *testing.T) {
	in := "short\n" + strings.Repeat("y", 500) + "\nlast\n"
	got, note := read(t, in, ReadOptions{MaxBytes: 50})
	if got != "short\n" || !strings.Contains(note, "2 more lines. Pass offset=2") {
		t.Errorf("got %q / %q", got, note)
	}
}

func TestReadLines_InvalidUTF8IsFlagged(t *testing.T) {
	got, note := read(t, "caf\xe9\n", ReadOptions{})
	if got != "caf\uFFFD\n" || !strings.Contains(note, "not valid UTF-8") {
		t.Errorf("got %q / %q", got, note)
	}
}

func TestTailLines(t *testing.T) {
	in := []byte("1\n2\n3\n4\n5\n")
	if got, note := tailLines(in, false, 2, 0, 10); got != "4\n5\n" || note != "Showing the last 2 lines of a 10-byte file." {
		t.Errorf("tail 2: %q / %q", got, note)
	}
	if got, note := tailLines(in, false, 10, 0, 10); got != "1\n2\n3\n4\n5\n" || note != "" {
		t.Errorf("tail more than file: %q / %q", got, note)
	}
	if got, _ := tailLines([]byte("1\n2\n3"), false, 1, 0, 5); got != "3" {
		t.Errorf("no trailing newline: %q", got)
	}
	// partial start: the first (incomplete) line is dropped
	if got, note := tailLines([]byte("art\n4\n5\n"), true, 10, 0, 100); got != "4\n5\n" || !strings.Contains(note, "last 2 lines") {
		t.Errorf("partial start: %q / %q", got, note)
	}
	// one enormous final line: keep its end rather than returning nothing
	got, note := tailLines([]byte(strings.Repeat("z", 1000)), true, 5, 100, 9_000_000)
	if got != strings.Repeat("z", 100) || !strings.Contains(note, "longer than") {
		t.Errorf("huge last line: %q / %q", got, note)
	}
}

func TestHeadTailBuffer(t *testing.T) {
	b := newHeadTail(5, 5)
	_, _ = b.Write([]byte("hello"))
	if b.String() != "hello" || b.Truncated() {
		t.Fatalf("small: %q", b.String())
	}
	for i := 0; i < 100; i++ {
		_, _ = b.Write([]byte("0123456789"))
	}
	_, _ = b.Write([]byte("WORLD"))
	got := b.String()
	if !b.Truncated() || !strings.HasPrefix(got, "hello\n... [1000 bytes omitted] ...\nWORLD") {
		t.Fatalf("got %q", got)
	}
}

func TestApplyEdit(t *testing.T) {
	src := "define('WP_DEBUG', false);\ndefine('WP_CACHE', false);\n"
	out, n, err := applyEdit(src, "'WP_DEBUG', false", "'WP_DEBUG', true", false, "wp-config.php")
	if err != nil || n != 1 || !strings.Contains(out, "'WP_DEBUG', true") || !strings.Contains(out, "'WP_CACHE', false") {
		t.Fatalf("single: %q %d %v", out, n, err)
	}
	if _, _, err := applyEdit(src, "false", "true", false, "f"); err == nil || !strings.Contains(err.Error(), "appears 2 times") {
		t.Errorf("ambiguous: %v", err)
	}
	if out, n, _ := applyEdit(src, "false", "true", true, "f"); n != 2 || strings.Contains(out, "false") {
		t.Errorf("replace_all: %q %d", out, n)
	}
	if _, _, err := applyEdit(src, "nope", "x", false, "f"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing: %v", err)
	}
	if _, _, err := applyEdit(src, "", "x", false, "f"); err == nil {
		t.Error("empty old_string must error")
	}
	if _, _, err := applyEdit("a\r\nb\r\n", "a\nb", "x", false, "f"); err == nil || !strings.Contains(err.Error(), "CRLF") {
		t.Errorf("crlf hint: %v", err)
	}
}

func TestIsBinary(t *testing.T) {
	if isBinary([]byte("plain <?php text\n")) || !isBinary([]byte{0x89, 'P', 'N', 'G', 0, 1}) {
		t.Error("isBinary misclassified")
	}
}
