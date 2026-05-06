import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { SiteResolver } from "../resolver.js";
import type { SSHOperations } from "../ssh/operations.js";

export function registerSiteTools(
  server: McpServer,
  resolver: SiteResolver,
  sshOps: SSHOperations
): void {
  // site_exec
  server.tool(
    "site_exec",
    "Execute a command on a site's server as the site's system user. The command runs in the app's public directory by default.",
    {
      site: z
        .string()
        .describe("Site identifier: app name or domain"),
      command: z.string().describe("Shell command to execute"),
      working_directory: z
        .string()
        .optional()
        .describe(
          "Working directory (relative to /srv/users/USERNAME/ or absolute). Defaults to app's public directory."
        ),
      timeout: z
        .number()
        .optional()
        .describe("Command timeout in milliseconds (default: 30000)"),
    },
    async ({ site, command, working_directory, timeout }) => {
      const resolved = await resolver.resolve(site);
      let cwd: string;

      if (working_directory) {
        cwd = resolver.validatePath(resolved, working_directory);
      } else {
        cwd = resolved.publicPath;
      }

      const result = await sshOps.exec(
        resolved.serverIp,
        resolved.sysUserName,
        command,
        { cwd, timeout }
      );

      const parts: string[] = [];
      if (result.stdout) parts.push(result.stdout);
      if (result.stderr) parts.push(`[stderr]\n${result.stderr}`);
      if (result.exitCode !== 0)
        parts.push(`[exit code: ${result.exitCode}]`);

      return {
        content: [
          {
            type: "text" as const,
            text:
              parts.join("\n") ||
              `(command completed with exit code ${result.exitCode})`,
          },
        ],
      };
    }
  );

  // site_read_file
  server.tool(
    "site_read_file",
    "Read a file from a site's server via SFTP",
    {
      site: z
        .string()
        .describe("Site identifier: app name or domain"),
      path: z
        .string()
        .describe(
          "File path (relative to /srv/users/USERNAME/ or absolute)"
        ),
      max_lines: z
        .number()
        .optional()
        .describe("Maximum number of lines to return"),
    },
    async ({ site, path, max_lines }) => {
      const resolved = await resolver.resolve(site);
      const fullPath = resolver.validatePath(resolved, path);

      const content = await sshOps.readFile(
        resolved.serverIp,
        resolved.sysUserName,
        fullPath,
        max_lines
      );

      return {
        content: [{ type: "text" as const, text: content }],
      };
    }
  );

  // site_write_file
  server.tool(
    "site_write_file",
    "Write a file on a site's server via SFTP",
    {
      site: z
        .string()
        .describe("Site identifier: app name or domain"),
      path: z
        .string()
        .describe(
          "File path (relative to /srv/users/USERNAME/ or absolute)"
        ),
      content: z.string().describe("File content to write"),
      create_dirs: z
        .boolean()
        .optional()
        .describe("Create parent directories if they don't exist"),
    },
    async ({ site, path, content, create_dirs }) => {
      const resolved = await resolver.resolve(site);
      const fullPath = resolver.validatePath(resolved, path);

      await sshOps.writeFile(
        resolved.serverIp,
        resolved.sysUserName,
        fullPath,
        content,
        create_dirs
      );

      return {
        content: [
          {
            type: "text" as const,
            text: `File written: ${fullPath}`,
          },
        ],
      };
    }
  );

  // site_list_files
  server.tool(
    "site_list_files",
    "List directory contents on a site's server via SFTP",
    {
      site: z
        .string()
        .describe("Site identifier: app name or domain"),
      path: z
        .string()
        .optional()
        .describe(
          "Directory path (relative to /srv/users/USERNAME/ or absolute). Defaults to user home."
        ),
      show_hidden: z
        .boolean()
        .optional()
        .describe("Include hidden files (dotfiles)"),
    },
    async ({ site, path, show_hidden }) => {
      const resolved = await resolver.resolve(site);
      const dirPath = path
        ? resolver.validatePath(resolved, path)
        : resolved.basePath;

      const entries = await sshOps.listDir(
        resolved.serverIp,
        resolved.sysUserName,
        dirPath,
        show_hidden
      );

      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify(entries, null, 2),
          },
        ],
      };
    }
  );
}
