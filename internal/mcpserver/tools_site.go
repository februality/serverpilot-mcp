package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/sandbox"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

// RegisterSiteTools registers site_exec, site_read_file, site_write_file,
// site_list_files.
func RegisterSiteTools(s *server.MCPServer, d *Deps) {
	s.AddTool(toolSiteExec(), handleSiteExec(d))
	s.AddTool(toolSiteReadFile(), handleSiteReadFile(d))
	s.AddTool(toolSiteWriteFile(), handleSiteWriteFile(d))
	s.AddTool(toolSiteListFiles(), handleSiteListFiles(d))
}

func toolSiteExec() mcp.Tool {
	return mcp.NewTool("site_exec",
		mcp.WithDescription("Execute a command on a site's server as the site's system user. The command runs in the app's public directory by default."),
		mcp.WithString("site", mcp.Required(), mcp.Description("Site identifier: app name or domain")),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to execute")),
		mcp.WithString("working_directory", mcp.Description("Working directory (relative to /srv/users/USERNAME/ or absolute). Defaults to app's public directory.")),
		mcp.WithNumber("timeout", mcp.Description("Command timeout in milliseconds (default: 30000)")),
	)
}

func toolSiteReadFile() mcp.Tool {
	return mcp.NewTool("site_read_file",
		mcp.WithDescription("Read a file from a site's server via SFTP"),
		mcp.WithString("site", mcp.Required(), mcp.Description("Site identifier: app name or domain")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path (relative to /srv/users/USERNAME/ or absolute)")),
		mcp.WithNumber("max_lines", mcp.Description("Maximum number of lines to return")),
	)
}

func toolSiteWriteFile() mcp.Tool {
	return mcp.NewTool("site_write_file",
		mcp.WithDescription("Write a file on a site's server via SFTP"),
		mcp.WithString("site", mcp.Required(), mcp.Description("Site identifier: app name or domain")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path (relative to /srv/users/USERNAME/ or absolute)")),
		mcp.WithString("content", mcp.Required(), mcp.Description("File content to write")),
		mcp.WithBoolean("create_dirs", mcp.Description("Create parent directories if they don't exist")),
	)
}

func toolSiteListFiles() mcp.Tool {
	return mcp.NewTool("site_list_files",
		mcp.WithDescription("List directory contents on a site's server via SFTP"),
		mcp.WithString("site", mcp.Required(), mcp.Description("Site identifier: app name or domain")),
		mcp.WithString("path", mcp.Description("Directory path (relative to /srv/users/USERNAME/ or absolute). Defaults to user home.")),
		mcp.WithBoolean("show_hidden", mcp.Description("Include hidden files (dotfiles)")),
	)
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
		timeoutMs := int(req.GetFloat("timeout", 0))

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

		res, err := d.SSHOps.Exec(resolved.ServerIP, resolved.SysUserName, command, mcsh.ExecOptions{
			Cwd:       cwd,
			TimeoutMs: timeoutMs,
		})
		if err != nil {
			return errResult(err)
		}

		var parts []string
		if res.Stdout != "" {
			parts = append(parts, res.Stdout)
		}
		if res.Stderr != "" {
			parts = append(parts, "[stderr]\n"+res.Stderr)
		}
		if res.ExitCode != 0 {
			parts = append(parts, fmt.Sprintf("[exit code: %d]", res.ExitCode))
		}
		text := strings.Join(parts, "\n")
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
		maxLines := int(req.GetFloat("max_lines", 0))

		resolved, err := d.Resolver.Resolve(site)
		if err != nil {
			return errResult(err)
		}
		fullPath, err := sandbox.ValidatePath(resolved.BasePath, path)
		if err != nil {
			return errResult(err)
		}
		content, err := d.SSHOps.ReadFile(resolved.ServerIP, resolved.SysUserName, fullPath, maxLines)
		if err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText(content), nil
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
		if err := d.SSHOps.WriteFile(resolved.ServerIP, resolved.SysUserName, fullPath, content, createDirs); err != nil {
			return errResult(err)
		}
		return mcp.NewToolResultText("File written: " + fullPath), nil
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
		entries, err := d.SSHOps.ListDir(resolved.ServerIP, resolved.SysUserName, dirPath, showHidden)
		if err != nil {
			return errResult(err)
		}
		return jsonText(entries)
	}
}
