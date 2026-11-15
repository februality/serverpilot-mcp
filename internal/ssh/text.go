package ssh

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// Output budgets. Claude Code moves any text result over 50,000 characters
// out of the conversation into a file (and caps results at 25k tokens), so
// every budget here keeps a single result comfortably below that. Bytes are
// an upper bound on characters, so byte budgets are safe.
const (
	DefaultReadLines = 2000
	DefaultReadBytes = 40_000
	// MaxEditBytes bounds site_edit_file, which holds the whole file in memory.
	MaxEditBytes = 10 << 20
	// maxTailScan is how far back from EOF a tail read looks.
	maxTailScan = 8 << 20
	// exactCountLimit: above this size, a truncated head read doesn't scan
	// the rest of the file just to report how many lines remain.
	exactCountLimit = 10 << 20

	stdoutHeadBytes = 6_000
	stdoutTailBytes = 28_000
	stderrHeadBytes = 2_000
	stderrTailBytes = 8_000
	// ExecOutputBudget is the most site_exec output a result can carry
	// (excluding short markers).
	ExecOutputBudget = stdoutHeadBytes + stdoutTailBytes + stderrHeadBytes + stderrTailBytes
)

// headTailBuffer keeps the first headMax and last tailMax bytes written to it
// and counts everything in between. For command output the tail usually
// matters most (errors, summaries), but the head shows what started.
type headTailBuffer struct {
	mu      sync.Mutex
	headMax int
	tailMax int
	head    []byte
	tail    []byte
	total   int64
}

func newHeadTail(headMax, tailMax int) *headTailBuffer {
	return &headTailBuffer{headMax: headMax, tailMax: tailMax}
}

func (b *headTailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.total += int64(n)
	if room := b.headMax - len(b.head); room > 0 {
		k := min(room, len(p))
		b.head = append(b.head, p[:k]...)
		p = p[k:]
	}
	if len(p) > 0 {
		b.tail = append(b.tail, p...)
		if len(b.tail) > 2*b.tailMax {
			b.tail = append([]byte(nil), b.tail[len(b.tail)-b.tailMax:]...)
		}
	}
	return n, nil
}

func (b *headTailBuffer) parts() (head, tail []byte, omitted int64) {
	tail = b.tail
	if len(tail) > b.tailMax {
		tail = tail[len(tail)-b.tailMax:]
	}
	return b.head, tail, b.total - int64(len(b.head)) - int64(len(tail))
}

// String renders the buffer, inserting an explicit marker where bytes were
// dropped. Invalid UTF-8 at the cut points is replaced rather than passed on.
func (b *headTailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	head, tail, omitted := b.parts()
	if omitted <= 0 {
		return strings.ToValidUTF8(string(head)+string(tail), "�")
	}
	return strings.ToValidUTF8(string(head), "�") +
		fmt.Sprintf("\n... [%d bytes omitted] ...\n", omitted) +
		strings.ToValidUTF8(string(tail), "�")
}

func (b *headTailBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _, omitted := b.parts()
	return omitted > 0
}

// isBinary uses the same heuristic as git and grep: a NUL byte in the sniffed
// prefix means binary.
func isBinary(sniff []byte) bool { return bytes.IndexByte(sniff, 0) >= 0 }

// readLineCapped reads one line (including its '\n') but keeps at most
// capBytes of it. When the line is longer, it stops right there (cut=true)
// without reading the rest, so a multi-gigabyte single-line file (minified JS,
// SQL dumps) costs only the budget, not a full transfer. ended reports
// whether the line's terminating newline was consumed (on a cut it may or may
// not have been, depending on what was already buffered).
func readLineCapped(r *bufio.Reader, capBytes int) (line []byte, cut, ended bool, err error) {
	for {
		chunk, e := r.ReadSlice('\n')
		room := capBytes - len(line)
		if room < len(chunk) {
			if room > 0 {
				line = append(line, chunk[:room]...)
			}
			return line, true, e == nil, nil
		}
		line = append(line, chunk...)
		if errors.Is(e, bufio.ErrBufferFull) {
			continue
		}
		return line, false, e == nil, e
	}
}

// skipLine consumes one line without keeping it.
func skipLine(r *bufio.Reader) error {
	for {
		_, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return err
	}
}

// countLines counts the lines remaining in r (a final line without a trailing
// newline counts as one).
func countLines(r *bufio.Reader) int {
	n, last := 0, byte('\n')
	for {
		chunk, err := r.ReadSlice('\n')
		if len(chunk) > 0 {
			last = chunk[len(chunk)-1]
			if last == '\n' {
				n++
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if last != '\n' {
				n++
			}
			return n
		}
	}
}

// ReadOptions selects which part of a file site_read_file returns.
type ReadOptions struct {
	Offset   int // 1-based first line; 0 = 1
	Limit    int // max lines; 0 = DefaultReadLines
	Tail     int // if > 0, return the last Tail lines instead (Offset/Limit ignored)
	MaxBytes int // byte budget; 0 = DefaultReadBytes
}

func (o ReadOptions) normalized() ReadOptions {
	if o.Offset <= 0 {
		o.Offset = 1
	}
	if o.Limit <= 0 {
		o.Limit = DefaultReadLines
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultReadBytes
	}
	return o
}

// readLines returns lines [Offset, Offset+Limit) of r within the byte budget,
// exactly as stored, plus a separate note (empty when the whole file was
// returned) that tells the model how to fetch the next part. The note is kept
// out of the content so a caller can never mistake it for file data.
func readLines(r *bufio.Reader, size int64, opts ReadOptions) (content, note string, err error) {
	opts = opts.normalized()
	for skipped := 0; skipped < opts.Offset-1; skipped++ {
		if _, err := r.Peek(1); err == io.EOF {
			return "", "", fmt.Errorf("offset %d is past the end of the file (%d lines)", opts.Offset, skipped)
		}
		if err := skipLine(r); err != nil && err != io.EOF {
			return "", "", err
		}
	}
	if _, err := r.Peek(1); err == io.EOF && opts.Offset > 1 {
		return "", "", fmt.Errorf("offset %d is past the end of the file (%d lines)", opts.Offset, opts.Offset-1)
	}

	var out bytes.Buffer
	shown := 0
	cutLine := false // a single line exceeded the whole budget and was cut
	pending := false // the next line didn't fit in what was left of the budget
	pendingEnded := false
	for shown < opts.Limit {
		if _, err := r.Peek(1); err == io.EOF {
			break
		}
		line, cut, ended, err := readLineCapped(r, opts.MaxBytes-out.Len())
		if cut && shown > 0 {
			pending, pendingEnded = true, ended
			break
		}
		out.Write(line)
		shown++
		if cut {
			cutLine = true
			break
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
	}

	content, notes := sanitize(out.Bytes())
	more := pending || cutLine
	if !more {
		if _, err := r.Peek(1); err != io.EOF {
			more = true
		}
	}
	if more {
		last := opts.Offset + shown - 1
		switch {
		case cutLine:
			notes = append([]string{fmt.Sprintf("Truncated: line %d is longer than the %d-byte output budget, so only its start is shown. "+
				"Pass offset=%d to skip past it, or use site_exec (e.g. `cut -c1-2000`) or site_download_file for very long lines.",
				last, opts.MaxBytes, last+1)}, notes...)
		case size <= exactCountLimit:
			// The line that didn't fit is still partly in r unless its
			// newline was already consumed; count it exactly once.
			remaining := countLines(r)
			if pending && pendingEnded {
				remaining++
			}
			notes = append([]string{fmt.Sprintf("Truncated: showing lines %d-%d; %d more lines. Pass offset=%d to continue, or tail=N for the end. "+
				"Do not write this partial content back with site_write_file; use site_edit_file to change part of a file.",
				opts.Offset, last, remaining, last+1)}, notes...)
		default:
			notes = append([]string{fmt.Sprintf("Truncated: showing lines %d-%d of a %d-byte file. Pass offset=%d to continue, or tail=N for the end. "+
				"Do not write this partial content back with site_write_file; use site_edit_file to change part of a file.",
				opts.Offset, last, size, last+1)}, notes...)
		}
	}
	return content, strings.Join(notes, " "), nil
}

// sanitize converts raw bytes to text for the model. Invalid UTF-8 (Latin-1
// files, binary fragments) is replaced with U+FFFD, and the returned note says
// so: writing such content back would corrupt every replaced byte.
func sanitize(b []byte) (string, []string) {
	if utf8.Valid(b) {
		return string(b), nil
	}
	return strings.ToValidUTF8(string(b), "\uFFFD"), []string{"The file is not valid UTF-8; invalid bytes are shown as \uFFFD. " +
		"Do not write this content back with site_write_file (it would corrupt them); site_edit_file edits the raw bytes safely."}
}

// tailLines returns the last n lines of data, plus a note when lines before
// them were omitted. partialStart means data does not begin at the start of
// the file, so its first (partial) line is dropped — unless it is the only
// line, in which case its end is kept.
func tailLines(data []byte, partialStart bool, n, maxBytes int, size int64) (string, string) {
	if maxBytes <= 0 {
		maxBytes = DefaultReadBytes
	}
	if partialStart {
		if i := bytes.IndexByte(data, '\n'); i >= 0 && i < len(data)-1 {
			data = data[i+1:]
		}
		// else: one enormous final line; fall through and keep its end.
	}
	body := bytes.TrimSuffix(data, []byte("\n"))
	start, found := 0, 0
	for i := len(body) - 1; i >= 0; i-- {
		if body[i] == '\n' {
			found++
			if found == n {
				start = i + 1
				break
			}
		}
	}
	omittedBefore := partialStart || start > 0
	out := data[start:]
	lineCut := false
	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
		if i := bytes.IndexByte(out, '\n'); i >= 0 && i < len(out)-1 {
			out = out[i+1:]
		} else {
			lineCut = true
		}
		omittedBefore = true
	}
	text, notes := sanitize(out)
	if omittedBefore {
		lines := strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
		msg := fmt.Sprintf("Showing the last %d lines of a %d-byte file.", lines, size)
		if lineCut {
			msg = fmt.Sprintf("The last line is longer than the %d-byte output budget; showing only its end (file is %d bytes).", maxBytes, size)
		}
		notes = append([]string{msg}, notes...)
	}
	return text, strings.Join(notes, " ")
}

// applyEdit performs an exact-string replacement with the same contract as
// Claude Code's Edit tool: old must occur exactly once unless replaceAll.
func applyEdit(content, old, replacement string, replaceAll bool, path string) (string, int, error) {
	if old == "" {
		return "", 0, fmt.Errorf("old_string must not be empty (to create or overwrite a whole file, use site_write_file)")
	}
	if old == replacement {
		return "", 0, fmt.Errorf("old_string and new_string are identical; nothing to change")
	}
	n := strings.Count(content, old)
	switch {
	case n == 0:
		hint := ""
		if strings.Contains(content, "\r\n") && !strings.Contains(old, "\r\n") && strings.Contains(old, "\n") {
			hint = " The file uses CRLF line endings."
		}
		return "", 0, fmt.Errorf("old_string not found in %s. It must match exactly, including whitespace and indentation; "+
			"read the file again with site_read_file and copy the text verbatim.%s", path, hint)
	case n > 1 && !replaceAll:
		return "", 0, fmt.Errorf("old_string appears %d times in %s. Include more surrounding lines to make it unique, "+
			"or pass replace_all: true to replace every occurrence", n, path)
	}
	if replaceAll {
		return strings.ReplaceAll(content, old, replacement), n, nil
	}
	return strings.Replace(content, old, replacement, 1), 1, nil
}
