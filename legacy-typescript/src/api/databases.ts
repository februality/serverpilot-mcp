import type { SPClient } from "./client.js";
import type {
  SPDatabase,
  SPListResponse,
  SPSingleResponse,
  SPActionResponse,
} from "../types.js";
import type { TTLCache } from "../cache.js";

export class DatabasesAPI {
  constructor(
    private client: SPClient,
    private cache: TTLCache
  ) {}

  async list(): Promise<SPDatabase[]> {
    const cached = this.cache.get<SPDatabase[]>("databases");
    if (cached) return cached;

    const response = await this.client.get<SPListResponse<SPDatabase>>("/dbs");
    this.cache.set("databases", response.data);
    return response.data;
  }

  async get(id: string): Promise<SPDatabase> {
    const cached = this.cache.get<SPDatabase>(`database:${id}`);
    if (cached) return cached;

    const response = await this.client.get<SPSingleResponse<SPDatabase>>(
      `/dbs/${id}`
    );
    this.cache.set(`database:${id}`, response.data);
    return response.data;
  }

  async listByApp(appId: string): Promise<SPDatabase[]> {
    const dbs = await this.list();
    return dbs.filter((d) => d.appid === appId);
  }

  async listByServer(serverId: string): Promise<SPDatabase[]> {
    const dbs = await this.list();
    return dbs.filter((d) => d.serverid === serverId);
  }

  async updatePassword(
    dbId: string,
    dbUserId: string,
    password: string
  ): Promise<SPActionResponse> {
    this.cache.invalidatePrefix("database");
    return this.client.post<SPActionResponse>(`/dbs/${dbId}`, {
      user: { id: dbUserId, password },
    });
  }
}
