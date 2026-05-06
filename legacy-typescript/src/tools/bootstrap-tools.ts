import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { Config } from "../config.js";
import type { SSHKeysAPI } from "../api/sshkeys.js";
import type { SysUsersAPI } from "../api/sysusers.js";
import { ensureSSHKeyPair, sshKeyExists } from "../ssh/keygen.js";

export function registerBootstrapTools(
  server: McpServer,
  config: Config,
  sshkeys: SSHKeysAPI,
  sysusers: SysUsersAPI
): void {
  // sp_ssh_setup
  server.tool(
    "sp_ssh_setup",
    "Generate SSH key, register with ServerPilot, and assign to system users. Safe to re-run — skips users that already have the key.",
    {
      users: z
        .array(z.string())
        .optional()
        .describe(
          "Specific system user IDs to assign to. If omitted, assigns to all system users."
        ),
    },
    async ({ users: userFilter }) => {
      const steps: string[] = [];

      // Step 1: Ensure key pair exists on disk
      const keyPair = ensureSSHKeyPair(config.sshKeyPath, config.sshKeyName);
      if (sshKeyExists(config.sshKeyPath)) {
        steps.push(`SSH key pair at ${config.sshKeyPath} (exists or created)`);
      }

      // Step 2: Register with ServerPilot if not already
      let spKey = await sshkeys.findByName(config.sshKeyName);
      if (spKey) {
        steps.push(
          `Key "${config.sshKeyName}" already registered with ServerPilot (ID: ${spKey.id})`
        );
      } else {
        spKey = await sshkeys.create(config.sshKeyName, keyPair.publicKey);
        steps.push(
          `Key "${config.sshKeyName}" registered with ServerPilot (ID: ${spKey.id})`
        );
      }

      // Step 3: Assign to system users
      let targetUsers;
      if (userFilter && userFilter.length > 0) {
        targetUsers = await Promise.all(
          userFilter.map((id) => sysusers.get(id))
        );
      } else {
        targetUsers = await sysusers.list();
      }

      const assigned: string[] = [];
      const skipped: string[] = [];
      const failed: string[] = [];

      for (const user of targetUsers) {
        try {
          // Check if already assigned
          const userKeys = await sshkeys.listForSysUser(user.id);
          if (userKeys.some((k) => k.id === spKey!.id)) {
            skipped.push(`${user.name} (${user.id}) — already has key`);
            continue;
          }

          await sshkeys.addToSysUser(spKey.id, user.id);
          assigned.push(`${user.name} (${user.id})`);
        } catch (err) {
          failed.push(
            `${user.name} (${user.id}) — ${err instanceof Error ? err.message : String(err)}`
          );
        }
      }

      if (assigned.length > 0)
        steps.push(`Assigned to: ${assigned.join(", ")}`);
      if (skipped.length > 0) steps.push(`Skipped: ${skipped.join(", ")}`);
      if (failed.length > 0) steps.push(`Failed: ${failed.join(", ")}`);

      return {
        content: [
          {
            type: "text" as const,
            text: `SSH Bootstrap Complete\n\n${steps.join("\n")}`,
          },
        ],
      };
    }
  );

  // sp_ssh_status
  server.tool(
    "sp_ssh_status",
    "Show SSH key status: whether the key exists locally and on ServerPilot, and which system users have it assigned",
    {},
    async () => {
      const localExists = sshKeyExists(config.sshKeyPath);
      const spKey = await sshkeys.findByName(config.sshKeyName);
      const allUsers = await sysusers.list();

      const status: Record<string, unknown> = {
        keyName: config.sshKeyName,
        keyPath: config.sshKeyPath,
        localKeyExists: localExists,
        registeredWithServerPilot: !!spKey,
        serverPilotKeyId: spKey?.id ?? null,
      };

      if (spKey) {
        const userStatuses = await Promise.all(
          allUsers.map(async (user) => {
            const userKeys = await sshkeys.listForSysUser(user.id);
            return {
              id: user.id,
              name: user.name,
              serverId: user.serverid,
              hasKey: userKeys.some((k) => k.id === spKey!.id),
            };
          })
        );
        status.users = userStatuses;
      }

      return {
        content: [
          { type: "text" as const, text: JSON.stringify(status, null, 2) },
        ],
      };
    }
  );

  // sp_ssh_remove
  server.tool(
    "sp_ssh_remove",
    "Remove the SSH key from specific system users, or from all users. Optionally delete the key from ServerPilot entirely.",
    {
      users: z
        .array(z.string())
        .optional()
        .describe(
          "System user IDs to remove the key from. If omitted, removes from all."
        ),
      delete_key: z
        .boolean()
        .optional()
        .describe(
          "Also delete the key from ServerPilot after removing from users"
        ),
    },
    async ({ users: userFilter, delete_key }) => {
      const steps: string[] = [];
      const spKey = await sshkeys.findByName(config.sshKeyName);

      if (!spKey) {
        return {
          content: [
            {
              type: "text" as const,
              text: `Key "${config.sshKeyName}" is not registered with ServerPilot. Nothing to remove.`,
            },
          ],
        };
      }

      // Get target users
      let targetUsers;
      if (userFilter && userFilter.length > 0) {
        targetUsers = await Promise.all(
          userFilter.map((id) => sysusers.get(id))
        );
      } else {
        targetUsers = await sysusers.list();
      }

      // Remove from users
      const removed: string[] = [];
      const notAssigned: string[] = [];

      for (const user of targetUsers) {
        const userKeys = await sshkeys.listForSysUser(user.id);
        if (!userKeys.some((k) => k.id === spKey.id)) {
          notAssigned.push(`${user.name} (${user.id})`);
          continue;
        }
        await sshkeys.removeFromSysUser(spKey.id, user.id);
        removed.push(`${user.name} (${user.id})`);
      }

      if (removed.length > 0)
        steps.push(`Removed from: ${removed.join(", ")}`);
      if (notAssigned.length > 0)
        steps.push(`Not assigned: ${notAssigned.join(", ")}`);

      // Optionally delete the key from ServerPilot
      if (delete_key) {
        await sshkeys.delete(spKey.id);
        steps.push(
          `Key "${config.sshKeyName}" deleted from ServerPilot`
        );
      }

      return {
        content: [
          {
            type: "text" as const,
            text: `SSH Key Removal\n\n${steps.join("\n")}`,
          },
        ],
      };
    }
  );
}
