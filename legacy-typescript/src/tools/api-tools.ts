import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { ServersAPI } from "../api/servers.js";
import type { AppsAPI } from "../api/apps.js";
import type { SysUsersAPI } from "../api/sysusers.js";
import type { DatabasesAPI } from "../api/databases.js";

export function registerAPITools(
  server: McpServer,
  servers: ServersAPI,
  apps: AppsAPI,
  sysusers: SysUsersAPI,
  databases: DatabasesAPI
): void {
  // sp_list_servers
  server.tool(
    "sp_list_servers",
    "List all ServerPilot servers with IPs, plans, and available runtimes",
    {},
    async () => {
      const list = await servers.list();
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify(
              list.map((s) => ({
                id: s.id,
                name: s.name,
                ip: s.lastaddress,
                plan: s.plan,
                runtimes: s.available_runtimes,
                firewall: s.firewall,
                autoupdates: s.autoupdates,
              })),
              null,
              2
            ),
          },
        ],
      };
    }
  );

  // sp_get_server
  server.tool(
    "sp_get_server",
    "Get details for a specific server by ID or name",
    { server: z.string().describe("Server ID or name") },
    async ({ server: serverIdOrName }) => {
      const s = await servers.resolve(serverIdOrName);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(s, null, 2) }],
      };
    }
  );

  // sp_list_apps
  server.tool(
    "sp_list_apps",
    "List all apps, optionally filtered by server",
    {
      server: z
        .string()
        .optional()
        .describe("Filter by server ID or name"),
    },
    async ({ server: serverFilter }) => {
      let list;
      if (serverFilter) {
        const s = await servers.resolve(serverFilter);
        list = await apps.listByServer(s.id);
      } else {
        list = await apps.list();
      }
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify(
              list.map((a) => ({
                id: a.id,
                name: a.name,
                domains: a.domains,
                runtime: a.runtime,
                ssl: a.autossl ? "auto" : a.ssl ? "custom" : "none",
                serverid: a.serverid,
                wordpress: !!a.wordpress,
              })),
              null,
              2
            ),
          },
        ],
      };
    }
  );

  // sp_get_app
  server.tool(
    "sp_get_app",
    "Get details for a specific app by ID, name, or domain",
    {
      app: z.string().describe("App ID, name, or domain"),
    },
    async ({ app: appIdentifier }) => {
      const a = await apps.resolve(appIdentifier);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(a, null, 2) }],
      };
    }
  );

  // sp_update_app_runtime
  server.tool(
    "sp_update_app_runtime",
    "Change an app's PHP runtime version",
    {
      app: z.string().describe("App ID, name, or domain"),
      runtime: z.string().describe("PHP runtime (e.g. php8.0, php8.3)"),
    },
    async ({ app: appIdentifier, runtime }) => {
      const a = await apps.resolve(appIdentifier);
      const result = await apps.updateRuntime(a.id, runtime);
      return {
        content: [
          {
            type: "text" as const,
            text: `PHP runtime for "${a.name}" updated to ${runtime}. Action ID: ${result.actionid}`,
          },
        ],
      };
    }
  );

  // sp_list_databases
  server.tool(
    "sp_list_databases",
    "List databases, optionally filtered by app or server",
    {
      app: z
        .string()
        .optional()
        .describe("Filter by app ID, name, or domain"),
      server: z
        .string()
        .optional()
        .describe("Filter by server ID or name"),
    },
    async ({ app: appFilter, server: serverFilter }) => {
      let list;
      if (appFilter) {
        const a = await apps.resolve(appFilter);
        list = await databases.listByApp(a.id);
      } else if (serverFilter) {
        const s = await servers.resolve(serverFilter);
        list = await databases.listByServer(s.id);
      } else {
        list = await databases.list();
      }
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify(
              list.map((d) => ({
                id: d.id,
                name: d.name,
                user: d.user.name,
                userId: d.user.id,
                appId: d.appid,
                serverId: d.serverid,
              })),
              null,
              2
            ),
          },
        ],
      };
    }
  );

  // sp_update_db_password
  server.tool(
    "sp_update_db_password",
    "Change a database user's MySQL password",
    {
      database: z.string().describe("Database ID"),
      password: z
        .string()
        .min(8)
        .describe("New password (min 8 characters)"),
    },
    async ({ database: dbId, password }) => {
      const db = await databases.get(dbId);
      const result = await databases.updatePassword(
        dbId,
        db.user.id,
        password
      );
      return {
        content: [
          {
            type: "text" as const,
            text: `Password updated for database user "${db.user.name}" on database "${db.name}". Action ID: ${result.actionid}`,
          },
        ],
      };
    }
  );

  // sp_list_sysusers
  server.tool(
    "sp_list_sysusers",
    "List system users, optionally filtered by server",
    {
      server: z
        .string()
        .optional()
        .describe("Filter by server ID or name"),
    },
    async ({ server: serverFilter }) => {
      let list;
      if (serverFilter) {
        const s = await servers.resolve(serverFilter);
        list = await sysusers.listByServer(s.id);
      } else {
        list = await sysusers.list();
      }
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify(
              list.map((u) => ({
                id: u.id,
                name: u.name,
                serverId: u.serverid,
              })),
              null,
              2
            ),
          },
        ],
      };
    }
  );
}
