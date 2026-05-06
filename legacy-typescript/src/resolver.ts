import type { AppsAPI } from "./api/apps.js";
import type { ServersAPI } from "./api/servers.js";
import type { SysUsersAPI } from "./api/sysusers.js";

export interface ResolvedSite {
  appId: string;
  appName: string;
  serverIp: string;
  serverId: string;
  sysUserName: string;
  sysUserId: string;
  domains: string[];
  basePath: string; // /srv/users/USERNAME
  appPath: string; // /srv/users/USERNAME/apps/APPNAME
  publicPath: string; // /srv/users/USERNAME/apps/APPNAME/public
}

export class SiteResolver {
  constructor(
    private apps: AppsAPI,
    private servers: ServersAPI,
    private sysusers: SysUsersAPI
  ) {}

  async resolve(siteIdentifier: string): Promise<ResolvedSite> {
    const app = await this.apps.resolve(siteIdentifier);
    const [server, sysuser] = await Promise.all([
      this.servers.get(app.serverid),
      this.sysusers.get(app.sysuserid),
    ]);

    const basePath = `/srv/users/${sysuser.name}`;

    return {
      appId: app.id,
      appName: app.name,
      serverIp: server.lastaddress,
      serverId: server.id,
      sysUserName: sysuser.name,
      sysUserId: sysuser.id,
      domains: app.domains,
      basePath,
      appPath: `${basePath}/apps/${app.name}`,
      publicPath: `${basePath}/apps/${app.name}/public`,
    };
  }

  /**
   * Validate that a path stays within the user's base directory.
   * Returns the resolved absolute path.
   */
  validatePath(site: ResolvedSite, relativePath: string): string {
    // If it's already absolute, check it's under basePath
    if (relativePath.startsWith("/")) {
      const normalized = normalizePath(relativePath);
      if (!normalized.startsWith(site.basePath)) {
        throw new Error(
          `Path "${relativePath}" is outside the allowed directory: ${site.basePath}`
        );
      }
      return normalized;
    }

    // Relative path — resolve from basePath
    const absolute = `${site.basePath}/${relativePath}`;
    const normalized = normalizePath(absolute);
    if (!normalized.startsWith(site.basePath)) {
      throw new Error(
        `Path "${relativePath}" resolves outside the allowed directory: ${site.basePath}`
      );
    }
    return normalized;
  }
}

/**
 * Simple path normalization that resolves . and .. segments
 * without requiring filesystem access.
 */
function normalizePath(path: string): string {
  const parts = path.split("/");
  const resolved: string[] = [];
  for (const part of parts) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      resolved.pop();
    } else {
      resolved.push(part);
    }
  }
  return "/" + resolved.join("/");
}
