import type { Config } from "../config.js";

const BASE_URL = "https://api.serverpilot.io/v1";

export class SPClient {
  private authHeader: string;

  constructor(private config: Config) {
    const credentials = Buffer.from(
      `${config.clientId}:${config.apiKey}`
    ).toString("base64");
    this.authHeader = `Basic ${credentials}`;
  }

  async request<T>(
    method: string,
    path: string,
    body?: Record<string, unknown>
  ): Promise<T> {
    const url = `${BASE_URL}${path}`;
    const headers: Record<string, string> = {
      Authorization: this.authHeader,
      "Content-Type": "application/json",
    };

    const response = await fetch(url, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
    });

    if (!response.ok) {
      const text = await response.text();
      throw new Error(
        `ServerPilot API ${method} ${path} failed (${response.status}): ${text}`
      );
    }

    const text = await response.text();
    if (!text) return undefined as T;
    return JSON.parse(text) as T;
  }

  get<T>(path: string): Promise<T> {
    return this.request<T>("GET", path);
  }

  post<T>(path: string, body: Record<string, unknown>): Promise<T> {
    return this.request<T>("POST", path, body);
  }

  delete<T>(path: string): Promise<T> {
    return this.request<T>("DELETE", path);
  }
}
