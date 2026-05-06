import { Client } from "ssh2";
import { readFileSync } from "fs";
import type { Config } from "../config.js";

interface PoolEntry {
  client: Client;
  lastUsed: number;
}

const IDLE_TIMEOUT_MS = 60_000;

export class SSHConnectionPool {
  private pool = new Map<string, PoolEntry>();
  private cleanupInterval: ReturnType<typeof setInterval>;

  constructor(private config: Config) {
    // Clean up idle connections every 30s
    this.cleanupInterval = setInterval(() => this.cleanup(), 30_000);
    // Don't let the timer keep the process alive
    this.cleanupInterval.unref();
  }

  private poolKey(host: string, username: string): string {
    return `${username}@${host}`;
  }

  async getConnection(host: string, username: string): Promise<Client> {
    const key = this.poolKey(host, username);
    const existing = this.pool.get(key);

    if (existing) {
      // Check if the connection is still alive
      try {
        existing.lastUsed = Date.now();
        return existing.client;
      } catch {
        this.pool.delete(key);
      }
    }

    const client = await this.connect(host, username);
    this.pool.set(key, { client, lastUsed: Date.now() });

    client.on("error", () => {
      this.pool.delete(key);
    });
    client.on("close", () => {
      this.pool.delete(key);
    });

    return client;
  }

  private connect(host: string, username: string): Promise<Client> {
    return new Promise((resolve, reject) => {
      const client = new Client();
      const privateKey = readFileSync(this.config.sshKeyPath, "utf-8");

      const timeout = setTimeout(() => {
        client.end();
        reject(new Error(`SSH connection to ${username}@${host} timed out`));
      }, this.config.sshTimeoutMs);

      client
        .on("ready", () => {
          clearTimeout(timeout);
          resolve(client);
        })
        .on("error", (err) => {
          clearTimeout(timeout);
          reject(
            new Error(
              `SSH connection to ${username}@${host} failed: ${err.message}`
            )
          );
        })
        .connect({
          host,
          port: 22,
          username,
          privateKey,
          readyTimeout: this.config.sshTimeoutMs,
        });
    });
  }

  private cleanup(): void {
    const now = Date.now();
    for (const [key, entry] of this.pool) {
      if (now - entry.lastUsed > IDLE_TIMEOUT_MS) {
        entry.client.end();
        this.pool.delete(key);
      }
    }
  }

  async closeAll(): Promise<void> {
    clearInterval(this.cleanupInterval);
    for (const [key, entry] of this.pool) {
      entry.client.end();
      this.pool.delete(key);
    }
  }
}
