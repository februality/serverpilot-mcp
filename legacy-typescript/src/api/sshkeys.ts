import type { SPClient } from "./client.js";
import type { SPSSHKey } from "../types.js";

// The SSH key API responses have a different shape
interface SSHKeyListResponse {
  data: SPSSHKey[];
}

interface SSHKeyCreateResponse {
  data: SPSSHKey;
}

export class SSHKeysAPI {
  constructor(private client: SPClient) {}

  async list(): Promise<SPSSHKey[]> {
    const response = await this.client.get<SSHKeyListResponse>("/sshkeys");
    return response.data;
  }

  async create(name: string, publicKey: string): Promise<SPSSHKey> {
    const response = await this.client.post<SSHKeyCreateResponse>("/sshkeys", {
      name,
      public_key: publicKey,
    });
    return response.data;
  }

  async delete(id: string): Promise<void> {
    await this.client.delete(`/sshkeys/${id}`);
  }

  async addToSysUser(sshKeyId: string, sysUserId: string): Promise<void> {
    await this.client.post(`/sysusers/${sysUserId}/sshkeys`, {
      sshkey_id: sshKeyId,
    });
  }

  async removeFromSysUser(
    sshKeyId: string,
    sysUserId: string
  ): Promise<void> {
    await this.client.delete(`/sysusers/${sysUserId}/sshkeys/${sshKeyId}`);
  }

  async listForSysUser(sysUserId: string): Promise<SPSSHKey[]> {
    const response = await this.client.get<SSHKeyListResponse>(
      `/sysusers/${sysUserId}/sshkeys`
    );
    return response.data;
  }

  async findByName(name: string): Promise<SPSSHKey | undefined> {
    const keys = await this.list();
    return keys.find((k) => k.name === name);
  }
}
