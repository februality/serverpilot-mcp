import type { SPClient } from "./client.js";
import type { SPSysUser, SPListResponse, SPSingleResponse } from "../types.js";
import type { TTLCache } from "../cache.js";

export class SysUsersAPI {
  constructor(
    private client: SPClient,
    private cache: TTLCache
  ) {}

  async list(): Promise<SPSysUser[]> {
    const cached = this.cache.get<SPSysUser[]>("sysusers");
    if (cached) return cached;

    const response = await this.client.get<SPListResponse<SPSysUser>>(
      "/sysusers"
    );
    const data = response?.data ?? [];
    this.cache.set("sysusers", data);
    return data;
  }

  async get(id: string): Promise<SPSysUser> {
    const cached = this.cache.get<SPSysUser>(`sysuser:${id}`);
    if (cached) return cached;

    const response = await this.client.get<SPSingleResponse<SPSysUser>>(
      `/sysusers/${id}`
    );
    if (!response?.data) throw new Error(`System user ${id} not found`);
    this.cache.set(`sysuser:${id}`, response.data);
    return response.data;
  }

  async listByServer(serverId: string): Promise<SPSysUser[]> {
    const sysusers = await this.list();
    return sysusers.filter((u) => u.serverid === serverId);
  }
}
