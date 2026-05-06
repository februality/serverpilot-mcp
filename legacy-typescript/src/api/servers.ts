import type { SPClient } from "./client.js";
import type { SPServer, SPListResponse, SPSingleResponse } from "../types.js";
import type { TTLCache } from "../cache.js";

export class ServersAPI {
  constructor(
    private client: SPClient,
    private cache: TTLCache
  ) {}

  async list(): Promise<SPServer[]> {
    const cached = this.cache.get<SPServer[]>("servers");
    if (cached) return cached;

    const response = await this.client.get<SPListResponse<SPServer>>(
      "/servers"
    );
    this.cache.set("servers", response.data);
    return response.data;
  }

  async get(id: string): Promise<SPServer> {
    const cached = this.cache.get<SPServer>(`server:${id}`);
    if (cached) return cached;

    const response = await this.client.get<SPSingleResponse<SPServer>>(
      `/servers/${id}`
    );
    this.cache.set(`server:${id}`, response.data);
    return response.data;
  }

  async findByName(name: string): Promise<SPServer | undefined> {
    const servers = await this.list();
    return servers.find(
      (s) => s.name.toLowerCase() === name.toLowerCase()
    );
  }

  async resolve(idOrName: string): Promise<SPServer> {
    // Try as ID first
    try {
      return await this.get(idOrName);
    } catch {
      // Try as name
      const server = await this.findByName(idOrName);
      if (!server) {
        throw new Error(`Server not found: ${idOrName}`);
      }
      return server;
    }
  }
}
