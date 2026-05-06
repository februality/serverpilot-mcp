import type { Client, SFTPWrapper } from "ssh2";
import type { SSHConnectionPool } from "./connection.js";

export interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

export interface FileEntry {
  name: string;
  type: "file" | "directory" | "symlink" | "other";
  size: number;
  modifiedAt: string;
  permissions: string;
}

export class SSHOperations {
  constructor(private pool: SSHConnectionPool) {}

  /**
   * Execute a command on a remote host.
   */
  async exec(
    host: string,
    username: string,
    command: string,
    options?: { cwd?: string; timeout?: number }
  ): Promise<ExecResult> {
    const client = await this.pool.getConnection(host, username);
    const fullCommand = options?.cwd
      ? `cd ${shellEscape(options.cwd)} && ${command}`
      : command;

    return new Promise((resolve, reject) => {
      const timeout = options?.timeout
        ? setTimeout(() => {
            reject(
              new Error(
                `Command timed out after ${options.timeout}ms: ${command}`
              )
            );
          }, options.timeout)
        : null;

      client.exec(fullCommand, (err, stream) => {
        if (err) {
          if (timeout) clearTimeout(timeout);
          reject(err);
          return;
        }

        let stdout = "";
        let stderr = "";

        stream
          .on("close", (code: number) => {
            if (timeout) clearTimeout(timeout);
            resolve({ stdout, stderr, exitCode: code ?? 0 });
          })
          .on("data", (data: Buffer) => {
            stdout += data.toString();
          })
          .stderr.on("data", (data: Buffer) => {
            stderr += data.toString();
          });
      });
    });
  }

  /**
   * Read a file from the remote host via SFTP.
   */
  async readFile(
    host: string,
    username: string,
    path: string,
    maxLines?: number
  ): Promise<string> {
    const sftp = await this.getSFTP(host, username);
    return new Promise((resolve, reject) => {
      sftp.readFile(path, "utf-8", (err, data) => {
        if (err) {
          reject(
            new Error(`Failed to read ${path}: ${err.message}`)
          );
          return;
        }
        let content = data.toString();
        if (maxLines) {
          const lines = content.split("\n");
          if (lines.length > maxLines) {
            content =
              lines.slice(0, maxLines).join("\n") +
              `\n... (truncated, ${lines.length - maxLines} more lines)`;
          }
        }
        resolve(content);
      });
    });
  }

  /**
   * Write a file on the remote host via SFTP.
   */
  async writeFile(
    host: string,
    username: string,
    path: string,
    content: string,
    createDirs?: boolean
  ): Promise<void> {
    if (createDirs) {
      const dir = path.substring(0, path.lastIndexOf("/"));
      await this.exec(host, username, `mkdir -p ${shellEscape(dir)}`);
    }

    const sftp = await this.getSFTP(host, username);
    return new Promise((resolve, reject) => {
      sftp.writeFile(path, content, "utf-8", (err) => {
        if (err) {
          reject(
            new Error(`Failed to write ${path}: ${err.message}`)
          );
          return;
        }
        resolve();
      });
    });
  }

  /**
   * List directory contents on the remote host via SFTP.
   */
  async listDir(
    host: string,
    username: string,
    path: string,
    showHidden?: boolean
  ): Promise<FileEntry[]> {
    const sftp = await this.getSFTP(host, username);
    return new Promise((resolve, reject) => {
      sftp.readdir(path, (err, list) => {
        if (err) {
          reject(
            new Error(`Failed to list ${path}: ${err.message}`)
          );
          return;
        }

        const entries: FileEntry[] = list
          .filter((item) => showHidden || !item.filename.startsWith("."))
          .map((item) => ({
            name: item.filename,
            type: getFileType(item.attrs.mode),
            size: item.attrs.size,
            modifiedAt: new Date(item.attrs.mtime * 1000).toISOString(),
            permissions: formatPermissions(item.attrs.mode),
          }))
          .sort((a, b) => {
            // Directories first, then alphabetical
            if (a.type === "directory" && b.type !== "directory") return -1;
            if (a.type !== "directory" && b.type === "directory") return 1;
            return a.name.localeCompare(b.name);
          });

        resolve(entries);
      });
    });
  }

  private getSFTP(host: string, username: string): Promise<SFTPWrapper> {
    return new Promise(async (resolve, reject) => {
      const client = await this.pool.getConnection(host, username);
      client.sftp((err, sftp) => {
        if (err) reject(new Error(`SFTP session failed: ${err.message}`));
        else resolve(sftp);
      });
    });
  }
}

function shellEscape(s: string): string {
  return `'${s.replace(/'/g, "'\\''")}'`;
}

function getFileType(
  mode: number
): "file" | "directory" | "symlink" | "other" {
  // S_IFMT = 0o170000
  const fmt = mode & 0o170000;
  switch (fmt) {
    case 0o040000:
      return "directory";
    case 0o100000:
      return "file";
    case 0o120000:
      return "symlink";
    default:
      return "other";
  }
}

function formatPermissions(mode: number): string {
  const perms = mode & 0o777;
  return perms.toString(8).padStart(3, "0");
}
