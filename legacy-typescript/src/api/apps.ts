import type { SPClient } from "./client.js";
import type { SPApp, SPListResponse, SPSingleResponse } from "../types.js";
import type { TTLCache } from "../cache.js";

export class AppsAPI {
  constructor(
    private client: SPClient,
    private cache: TTLCache
  ) {}

  async list(): Promise<SPApp[]> {
    const cached = this.cache.get<SPApp[]>("apps");
    if (cached) return cached;

    const response = await this.client.get<SPListResponse<SPApp>>("/apps");
    this.cache.set("apps", response.data);
    return response.data;
  }

  async get(id: string): Promise<SPApp> {
    const cached = this.cache.get<SPApp>(`app:${id}`);
    if (cached) return cached;

    const response = await this.client.get<SPSingleResponse<SPApp>>(
      `/apps/${id}`
    );
    this.cache.set(`app:${id}`, response.data);
    return response.data;
  }

  async listByServer(serverId: string): Promise<SPApp[]> {
    const apps = await this.list();
    return apps.filter((a) => a.serverid === serverId);
  }

  async findByName(name: string): Promise<SPApp | undefined> {
    const apps = await this.list();
    return apps.find((a) => a.name.toLowerCase() === name.toLowerCase());
  }

  async findByDomain(domain: string): Promise<SPApp | undefined> {
    const apps = await this.list();
    return apps.find((a) =>
      a.domains.some((d) => d.toLowerCase() === domain.toLowerCase())
    );
  }

  async updateRuntime(id: string, runtime: string): Promise<{ actionid: string }> {
    this.cache.invalidate("apps");
    this.cache.invalidate(`app:${id}`);
    const response = await this.client.post<{ actionid: string; data: SPApp }>(
      `/apps/${id}`,
      { runtime }
    );
    return { actionid: response.actionid };
  }

  async resolve(idOrNameOrDomain: string): Promise<SPApp> {
    // Try as ID first
    try {
      return await this.get(idOrNameOrDomain);
    } catch {
      // Try as name
      const byName = await this.findByName(idOrNameOrDomain);
      if (byName) return byName;

      // Try as domain
      const byDomain = await this.findByDomain(idOrNameOrDomain);
      if (byDomain) return byDomain;

      throw new Error(`App not found: ${idOrNameOrDomain}`);
    }
  }
}
