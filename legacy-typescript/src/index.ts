import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { loadConfig } from "./config.js";
import { TTLCache } from "./cache.js";
import { SPClient } from "./api/client.js";
import { ServersAPI } from "./api/servers.js";
import { AppsAPI } from "./api/apps.js";
import { SysUsersAPI } from "./api/sysusers.js";
import { DatabasesAPI } from "./api/databases.js";
import { SSHKeysAPI } from "./api/sshkeys.js";
import { SiteResolver } from "./resolver.js";
import { SSHConnectionPool } from "./ssh/connection.js";
import { SSHOperations } from "./ssh/operations.js";
import { registerAPITools } from "./tools/api-tools.js";
import { registerBootstrapTools } from "./tools/bootstrap-tools.js";
import { registerSiteTools } from "./tools/site-tools.js";

async function main() {
  const config = loadConfig();
  const cache = new TTLCache(config.cacheTtlSeconds);

  // API layer
  const client = new SPClient(config);
  const servers = new ServersAPI(client, cache);
  const apps = new AppsAPI(client, cache);
  const sysusers = new SysUsersAPI(client, cache);
  const databases = new DatabasesAPI(client, cache);
  const sshkeys = new SSHKeysAPI(client);

  // Resolver
  const resolver = new SiteResolver(apps, servers, sysusers);

  // SSH layer
  const sshPool = new SSHConnectionPool(config);
  const sshOps = new SSHOperations(sshPool);

  // MCP Server
  const server = new McpServer({
    name: "serverpilot",
    version: "1.0.0",
  });

  // Register tools
  registerAPITools(server, servers, apps, sysusers, databases);
  registerBootstrapTools(server, config, sshkeys, sysusers);
  registerSiteTools(server, resolver, sshOps);

  // Start transport
  const transport = new StdioServerTransport();
  await server.connect(transport);

  // Cleanup on exit
  process.on("SIGINT", async () => {
    await sshPool.closeAll();
    process.exit(0);
  });
  process.on("SIGTERM", async () => {
    await sshPool.closeAll();
    process.exit(0);
  });
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
