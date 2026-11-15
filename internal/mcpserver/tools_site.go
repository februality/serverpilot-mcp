package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/sandbox"
	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

// RegisterSiteTools registers the site_* tools. site_exec (arbitrary shell),
// site_write_file, site_edit_file and site_upload_file are skipped when
// d.Cfg.ReadOnly is true — no command-level allowlist is safe enough to treat
// site_exec as read-only. site_download_file stays, without its overwrite
// option: it reads from the server and only creates new files inside the
// local transfer directory, so it can't change anything that already exists.
func RegisterSiteTools(s *server.MCPServer, d *Deps) {
	s.AddTool(toolSiteReadFile(), handleSiteReadFile(d))
	s.AddTool(toolSiteListFiles(), handleSiteListFiles(d))
	s.AddTool(toolSiteDownloadFile(d.Cfg.TransferDir, d.Cfg.ReadOnly), handleSiteDownloadFile(d))
	if !d.Cfg.ReadOnly {
		s.AddTool(toolSiteExec(), handleSiteExec(d))
		s.AddTool(toolSiteWriteFile(), handleSiteWriteFile(d))
		s.AddTool(toolSiteEditFile(), handleSiteEditFile(d))
		s.AddTool(toolSiteUploadFile(d.Cfg.TransferDir), handleSiteUploadFile(d))
	}
}

const (
	siteParamDesc    = "Site: app name or domain (e.g. example.com)"
	sitePathDesc     = "Path relative to the system user's home /srv/users/<sysuser>/ (e.g. apps/<app>/public/wp-config.php), or absolute within it"
	defaultListLimit = 300 // ~100 bytes per entry; pageBudgetBytes caps it regardless
	minExecTimeoutMs = 1000
)

func toolSiteExec() mcp.Tool {
	return newTool("site_exec", "Run shell command", kindDestructive, true,
		mcp.WithDescription("Run a non-interactive shell command on the app's server as its system user (no TTY, no sudo). "+
			"Runs in the app's web root (apps/<app>/public) unless working_directory is set, so `wp ...` works directly for WordPress. "+
			"Returns stdout, then stderr after a [stderr] marker, then [exit code: N] when non-zero. "+
			fmt.Sprintf("Output beyond ~%d KB keeps the start and end and drops the middle; redirect big output to a file and read it with site_read_file.", mcsh.ExecOutputBudget/1000)),
		mcp.WithString("site", mcp.Required(), mcp.Description(siteParamDesc)),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to execute")),
		mcp.WithString("working_directory", mcp.Description("Working directory: relative to /srv/users/<sysuser>/ or absolute within it. Defaults to the app's web root.")),
		mcp.WithNumber("timeout", mcp.Min(0), mcp.Description("Timeout in MILLISECONDS (e.g. 600000 = 10 minutes). Default 120000. 0 disables the timeout. Values between 1 and 999 are rejected as a likely seconds/milliseconds mix-up.")),
	)
}

func toolSiteReadFile() mcp.Tool {
	return newTool("site_read_file", "Read file", kindRead, false,
		mcp.WithDescription(fmt.Sprintf("Read a text file from the app's server via SFTP. The first content block is the file text exactly as stored "+
			"(invalid UTF-8 bytes, if any, become U+FFFD and a note says so). Returns at most max_lines lines (default %d) and %d KB per call; "+
			"when that is less than the whole file, a separate note says how to fetch the rest (offset=N). Use tail to read the end of a log. "+
			"Binary files are refused; use site_download_file for them.", mcsh.DefaultReadLines, mcsh.DefaultReadBytes/1000)),
		mcp.WithString("site", mcp.Required(), mcp.Description(siteParamDesc)),
		mcp.WithString("path", mcp.Required(), mcp.Description(sitePathDesc)),
		mcp.WithNumber("offset", mcp.Min(1), mcp.Description("1-based line number to start from (for paging through large files)")),
		mcp.WithNumber("max_lines", mcp.Min(1), mcp.Description(fmt.Sprintf("Maximum number of lines to return (default %d)", mcsh.DefaultReadLines))),
		mcp.WithNumber("tail", mcp.Min(1), mcp.Description("Return only the last N lines (e.g. 200 for a log). Cannot be combined with offset.")),
	)
}

func toolSiteWriteFile() mcp.Tool {
	return newTool("site_write_file", "Write file", kindIdempotent, false,
		mcp.WithDescription("Create a file or replace its ENTIRE contents on the app's server via SFTP. The write is atomic and keeps the file's permissions and owner; "+
			"files without write permission (e.g. a wp-config.php locked to 0400) are refused. "+
			"To change part of an existing file use site_edit_file; never write back content from a site_read_file result that said it was truncated."),
		mcp.WithString("site", mcp.Required(), mcp.Description(siteParamDesc)),
		mcp.WithString("path", mcp.Required(), mcp.Description(sitePathDesc)),
		mcp.WithString("content", mcp.Required(), mcp.Description("Complete new file content")),
		mcp.WithBoolean("create_dirs", mcp.Description("Create missing parent directories")),
	)
}

func toolSiteEditFile() mcp.Tool {
	return newTool("site_edit_file", "Edit file", kindDestructive, false,
		mcp.WithDescription("Edit a text file on the app's server by replacing an exact string. old_string must match the file exactly, including whitespace, "+
			"and must occur exactly once unless replace_all is true; include enough surrounding lines to make it unique. "+
			"Read the file with site_read_file first. The write is atomic and keeps the file's permissions."),
		mcp.WithString("site", mcp.Required(), mcp.Description(siteParamDesc)),
		mcp.WithString("path", mcp.Required(), mcp.Description(sitePathDesc)),
		mcp.WithString("old_string", mcp.Required(), mcp.Description("Exact text to replace")),
		mcp.WithString("new_string", mcp.Required(), mcp.Description("Replacement text (may be empty to delete old_string)")),
		mcp.WithBoolean("replace_all", mcp.Description("Replace every occurrence instead of requiring exactly one")),
	)
}

func toolSiteListFiles() mcp.Tool {
	return newTool("site_list_files", "List directory", kindRead, false,
		append([]mcp.ToolOption{
			mcp.WithDescription("List a directory on the app's server via SFTP as JSON (name, type, size, modifiedAt, permissions), directories first, then by name. Not recursive; " +
				"for searches use site_exec with find. Paged: a note after the list gives the next offset when there are more."),
			mcp.WithString("site", mcp.Required(), mcp.Description(siteParamDesc)),
			mcp.WithString("path", mcp.Description("Directory: relative to /srv/users/<sysuser>/ or absolute within it. Defaults to that home directory (the app lives in apps/<app>/).")),
			mcp.WithBoolean("show_hidden", mcp.Description("Include dotfiles such as .htaccess")),
		}, pagingParams("entries", defaultListLimit, maxPageLimit)...)...,
	)
}

// toolSiteDownloadFile: with overwrite it can replace a local file, so it is
// annotated destructive; in read-only mode the overwrite option is removed
// and it only ever creates files (additive).
func toolSiteDownloadFile(transferDir string, readOnly bool) mcp.Tool {
	kind := kindIdempotent
	opts := []mcp.ToolOption{
		mcp.WithDescription("Copy a file from the app's server to this machine via SFTP, without passing its contents through the conversation. " +
			"Use it for binaries, backups (e.g. after `wp db export`), and files too large to read. " +
			"Local paths are restricted to the transfer directory " + transferDir + ". To fetch a directory, archive it with site_exec first."),
		mcp.WithString("site", mcp.Required(), mcp.Description(siteParamDesc)),
		mcp.WithString("path", mcp.Required(), mcp.Description("Remote file. "+sitePathDesc)),
		mcp.WithString("local_path", mcp.Description("Destination file (or an existing directory to put it in), relative to the transfer directory or absolute inside it. Defaults to <app>/<file name>.")),
	}
	if readOnly {
		kind = kindAdditive
	} else {
		opts = append(opts, mcp.WithBoolean("overwrite", mcp.Description("Replace the local file if it already exists (default false)")))
	}
	return newTool("site_download_file", "Download file", kind, false, opts...)
}

func toolSiteUploadFile(transferDir string) mcp.Tool {
	return newTool("site_upload_file", "Upload file", kindIdempotent, false,
		mcp.WithDescription("Copy a file from this machine to the app's server via SFTP, replacing any existing file atomically. "+
			"Use it for binaries and large files instead of site_write_file. The local file must be inside the transfer directory "+transferDir+"."),
		mcp.WithString("site", mcp.Required(), mcp.Description(siteParamDesc)),
		mcp.WithString("local_path", mcp.Required(), mcp.Description("Local source file, relative to the transfer directory or absolute inside it")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Remote destination. "+sitePathDesc)),
		mcp.WithBoolean("create_dirs", mcp.Description("Create missing remote parent directories")),
	)
}

func target(r *spapi.ResolvedSite) mcsh.Target {
	return mcsh.Target{Host: r.ServerIP, User: r.SysUserName, Base: r.BasePath}
}

// formatExec renders an ExecResult. The layout (stdout, "[stderr]" block,
// "[exit code: N]" trailer) is unchanged from earlier versions; a truncation
// notice is added only when output was trimmed.
func formatExec(res *mcsh.ExecResult, includeExit bool) string {
	var parts []string
	if res.Stdout != "" {
		parts = append(parts, res.Stdout)
	}
	if res.Stderr != "" {
		parts = append(parts, "[stderr]\n"+res.Stderr)
	}
	if res.Truncated {
		parts = append(parts, "[output truncated: the middle was omitted. Redirect output to a file (cmd > ~/out.txt 2>&1) and page it with site_read_file path=out.txt offset/tail]")
	}
	if includeExit && res.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("[exit code: %d]", res.ExitCode))
	}
	return strings.Join(parts, "\n")
}

func handleSiteExec(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		site, err := req.RequireString("site")
		if err != nil {
			return errResult(err)
		}
		command, err := req.RequireString("command")
		if err != nil {
			return errResult(err)
		}
		workingDir := req.GetString("working_directory", "")
		// Sentinel -1 distinguishes "param omitted" (use configured default)
		// from "param=0" (explicit no-timeout opt-out).
		timeoutMs := int(req.GetFloat("timeout", -1))
		if timeoutMs < 0 {
			timeoutMs = d.Cfg.SiteExecTimeoutMs
		} else if timeoutMs > 0 && timeoutMs < minExecTimeoutMs {
			return errResult(fmt.Errorf("timeout is in milliseconds; %d ms is too short to be intended. "+
				"For %d seconds pass %d. Pass 0 to disable the timeout", timeoutMs, timeoutMs, timeoutMs*1000))
		}

		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		var cwd string
		if workingDir != "" {
			cwd, err = sandbox.ValidatePath(resolved.BasePath, workingDir)
			if err != nil {
				return errResult(err)
			}
		} else {
			cwd = resolved.PublicPath
		}

		res, err := d.SSHOps.Exec(ctx, target(resolved), command, mcsh.ExecOptions{
			Cwd:       cwd,
			TimeoutMs: timeoutMs,
		})
		if err != nil {
			if res == nil {
				return errResult(err)
			}
			// Timed out or cancelled: keep whatever output arrived so the
			// model can see how far the command got.
			msg := err.Error()
			if errors.Is(err, mcsh.ErrExecTimeout) || errors.Is(err, mcsh.ErrExecCancelled) {
				msg += "\nA kill signal was sent and the session closed; some servers ignore the signal, so the process may still be running " +
					"(check with `ps -u $(whoami) -f`)."
			}
			if errors.Is(err, mcsh.ErrExecTimeout) {
				msg += " Pass a larger timeout (in milliseconds), or 0 for none. " +
					"For very long jobs, start them with nohup in the background and poll a log file."
			}
			if out := formatExec(res, false); out != "" {
				msg += "\n\nOutput before it stopped:\n" + out
			}
			return mcp.NewToolResultError(msg), nil
		}

		text := formatExec(res, true)
		if text == "" {
			text = fmt.Sprintf("(command completed with exit code %d)", res.ExitCode)
		}
		return mcp.NewToolResultText(text), nil
	}
}

func handleSiteReadFile(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		site, err := req.RequireString("site")
		if err != nil {
			return errResult(err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return errResult(err)
		}
		opts := mcsh.ReadOptions{
			Offset: int(req.GetFloat("offset", 0)),
			Limit:  int(req.GetFloat("max_lines", 0)),
			Tail:   int(req.GetFloat("tail", 0)),
		}
		if opts.Tail > 0 && opts.Offset > 1 {
			return errResult(fmt.Errorf("pass either tail or offset, not both"))
		}

		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		fullPath, err := sandbox.ValidatePath(resolved.BasePath, path)
		if err != nil {
			return errResult(err)
		}
		content, note, err := d.SSHOps.ReadFile(target(resolved), fullPath, opts)
		if err != nil {
			return errResult(err)
		}
		// content[0] is exactly the file text; any note goes in its own block.
		res := mcp.NewToolResultText(content)
		if note != "" {
			res.Content = append(res.Content, mcp.NewTextContent(note))
		}
		return res, nil
	}
}

func handleSiteWriteFile(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		site, err := req.RequireString("site")
		if err != nil {
			return errResult(err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return errResult(err)
		}
		content, err := req.RequireString("content")
		if err != nil {
			return errResult(err)
		}
		createDirs := req.GetBool("create_dirs", false)

		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		fullPath, err := sandbox.ValidatePath(resolved.BasePath, path)
		if err != nil {
			return errResult(err)
		}
		if err := d.SSHOps.WriteFile(target(resolved), fullPath, content, createDirs); err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("File written: %s (%d bytes)", fullPath, len(content))), nil
	}
}

func handleSiteEditFile(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		site, err := req.RequireString("site")
		if err != nil {
			return errResult(err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return errResult(err)
		}
		oldStr, err := req.RequireString("old_string")
		if err != nil {
			return errResult(err)
		}
		newStr, err := req.RequireString("new_string")
		if err != nil {
			return errResult(err)
		}
		replaceAll := req.GetBool("replace_all", false)

		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		fullPath, err := sandbox.ValidatePath(resolved.BasePath, path)
		if err != nil {
			return errResult(err)
		}
		n, err := d.SSHOps.EditFile(target(resolved), fullPath, oldStr, newStr, replaceAll)
		if err != nil {
			return errResult(err)
		}
		noun := "replacement"
		if n != 1 {
			noun = "replacements"
		}
		return mcp.NewToolResultText(fmt.Sprintf("File edited: %s (%d %s)", fullPath, n, noun)), nil
	}
}

func handleSiteListFiles(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		site, err := req.RequireString("site")
		if err != nil {
			return errResult(err)
		}
		path := req.GetString("path", "")
		showHidden := req.GetBool("show_hidden", false)

		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		dirPath := resolved.BasePath
		if path != "" {
			dirPath, err = sandbox.ValidatePath(resolved.BasePath, path)
			if err != nil {
				return errResult(err)
			}
		}
		entries, _, err := d.SSHOps.ListDir(target(resolved), dirPath, showHidden, 0)
		if err != nil {
			return errResult(err)
		}
		return pagedJSON(entries, req, defaultListLimit, "entries",
			fmt.Sprintf("use site_exec (e.g. `ls %s | grep ...` or find) to search", mcsh.ShellEscape(dirPath)))
	}
}

func handleSiteDownloadFile(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		site, err := req.RequireString("site")
		if err != nil {
			return errResult(err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return errResult(err)
		}
		overwrite := req.GetBool("overwrite", false) && !d.Cfg.ReadOnly

		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		remote, err := sandbox.ValidatePath(resolved.BasePath, path)
		if err != nil {
			return errResult(err)
		}
		localArg := req.GetString("local_path", "")
		if localArg == "" {
			localArg = filepath.Join(resolved.AppName, filepath.Base(remote))
		}
		local, err := sandbox.ValidateLocalPath(d.Cfg.TransferDir, localArg)
		if err != nil {
			return errResult(err)
		}
		if fi, err := os.Stat(local); err == nil && fi.IsDir() {
			// An existing directory means "put it in here".
			if local, err = sandbox.ValidateLocalPath(d.Cfg.TransferDir, filepath.Join(local, filepath.Base(remote))); err != nil {
				return errResult(err)
			}
		}
		if fi, err := os.Stat(local); err == nil {
			switch {
			case fi.IsDir():
				return errResult(fmt.Errorf("local path %s is a directory; choose a file path", local))
			case !overwrite && d.Cfg.ReadOnly:
				return errResult(fmt.Errorf("local file %s already exists and read-only mode never replaces files; choose another local_path", local))
			case !overwrite:
				return errResult(fmt.Errorf("local file %s already exists; pass overwrite: true or choose another local_path", local))
			}
		}
		n, err := d.SSHOps.Download(target(resolved), remote, local)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Downloaded %s to %s (%d bytes)", remote, local, n)), nil
	}
}

func handleSiteUploadFile(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		site, err := req.RequireString("site")
		if err != nil {
			return errResult(err)
		}
		localArg, err := req.RequireString("local_path")
		if err != nil {
			return errResult(err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return errResult(err)
		}
		createDirs := req.GetBool("create_dirs", false)

		local, err := sandbox.ValidateLocalPath(d.Cfg.TransferDir, localArg)
		if err != nil {
			return errResult(err)
		}
		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		remote, err := sandbox.ValidatePath(resolved.BasePath, path)
		if err != nil {
			return errResult(err)
		}
		n, err := d.SSHOps.Upload(target(resolved), local, remote, createDirs)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Uploaded %s to %s (%d bytes)", local, remote, n)), nil
	}
}
