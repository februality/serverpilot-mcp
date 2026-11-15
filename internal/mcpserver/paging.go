package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/februality/serverpilot-mcp/internal/spapi"
)

// Paging for list tools. Claude Code moves any tool result over 50,000
// characters out of the conversation into a file, so a page stops at
// whichever comes first: the item limit or pageBudgetBytes of JSON.
//
// The first content block is always the same JSON value (array or object)
// the tool returned before paging existed, now compact-encoded; when the
// result is partial, a second text block says what was shown and how to get
// the rest. Callers that parse content[0] keep working, and lists smaller
// than one page decode to exactly what they did before.
const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
	pageBudgetBytes  = 40_000
)

func pagingParams(noun string, defLimit, maxLimit int) []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithNumber("limit", mcp.Min(1), mcp.Max(maxLimit),
			mcp.Description(fmt.Sprintf("Maximum %s to return (default %d). A page also stops at about %d KB of output.", noun, defLimit, pageBudgetBytes/1000))),
		mcp.WithNumber("offset", mcp.Min(0),
			mcp.Description(fmt.Sprintf("Number of %s to skip, for the next page (default 0). The result says which offset to use next.", noun))),
	}
}

func queryParam(what string) mcp.ToolOption {
	return mcp.WithString("query", mcp.Description("Only return "+what+" (case-insensitive substring)"))
}

// pageArgs reads limit/offset. Out-of-range values are clamped rather than
// rejected: a model asking for limit 5000 should get a big page, not an error.
func pageArgs(req mcp.CallToolRequest, defLimit int) (offset, limit int) {
	offset = max(int(req.GetFloat("offset", 0)), 0)
	limit = int(req.GetFloat("limit", float64(defLimit)))
	if limit <= 0 {
		limit = defLimit
	}
	return offset, min(limit, maxPageLimit)
}

// paginate encodes items[offset:] as a JSON array, stopping at limit items or
// when the next item would push the array past budget bytes. At least one
// item is always included (when any remain) so paging always makes progress.
func paginate[T any](items []T, offset, limit, budget int) (out []byte, shown int, err error) {
	if offset > 0 && offset >= len(items) {
		return nil, 0, fmt.Errorf("offset %d is past the end of the list (%d items)", offset, len(items))
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i := offset; i < len(items) && shown < limit; i++ {
		b, err := json.Marshal(items[i])
		if err != nil {
			return nil, 0, err
		}
		if shown > 0 && buf.Len()+1+len(b)+1 > budget {
			break
		}
		if shown > 0 {
			buf.WriteByte(',')
		}
		buf.Write(b)
		shown++
	}
	buf.WriteByte(']')
	return buf.Bytes(), shown, nil
}

// pageNote describes a partial page, or returns "" when the whole list fits.
// narrow is an optional hint such as "filter with query or server".
func pageNote(noun string, offset, shown, total int, narrow string) string {
	if offset == 0 && shown >= total {
		return ""
	}
	note := fmt.Sprintf("Showing %s %d-%d of %d.", noun, offset+1, offset+shown, total)
	if next := offset + shown; next < total {
		note += fmt.Sprintf(" Pass offset=%d (with the same filters) for the next page", next)
		if narrow != "" {
			note += ", or " + narrow
		}
		note += "."
	} else {
		note += " This is the last page."
	}
	return note
}

// pagedJSON is the standard list-tool result: one page of items as a JSON
// array, plus a paging note when the list didn't fit.
func pagedJSON[T any](items []T, req mcp.CallToolRequest, defLimit int, noun, narrow string) (*mcp.CallToolResult, error) {
	offset, limit := pageArgs(req, defLimit)
	out, shown, err := paginate(items, offset, limit, pageBudgetBytes)
	if err != nil {
		return errResult(err)
	}
	res := mcp.NewToolResultText(string(out))
	if note := pageNote(noun, offset, shown, len(items), narrow); note != "" {
		res.Content = append(res.Content, mcp.NewTextContent(note))
	}
	return res, nil
}

// matchesQuery reports whether any field contains q (case-insensitive). A
// query that looks like a URL is reduced to its host first.
func matchesQuery(q string, fields ...string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if strings.Contains(q, "://") {
		q = spapi.NormalizeDomain(q)
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}
