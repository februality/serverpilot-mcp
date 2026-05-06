import { homedir } from "os";
import { join } from "path";

export interface Config {
  clientId: string;
  apiKey: string;
  sshKeyPath: string;
  sshKeyName: string;
  cacheTtlSeconds: number;
  sshTimeoutMs: number;
}

function required(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`Missing required environment variable: ${name}`);
  }
  return value;
}

function expandHome(p: string): string {
  if (p.startsWith("~")) {
    return join(homedir(), p.slice(1));
  }
  return p;
}

export function loadConfig(): Config {
  return {
    clientId: required("SERVERPILOT_CLIENT_ID"),
    apiKey: required("SERVERPILOT_API_KEY"),
    sshKeyPath: expandHome(
      process.env.SP_SSH_KEY_PATH || "~/.ssh/serverpilot-mcp"
    ),
    sshKeyName: process.env.SP_SSH_KEY_NAME || "claude-mcp-serverpilot",
    cacheTtlSeconds: parseInt(process.env.SP_CACHE_TTL_SECONDS || "300", 10),
    sshTimeoutMs: parseInt(process.env.SP_SSH_TIMEOUT_MS || "30000", 10),
  };
}
