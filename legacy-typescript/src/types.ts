// ServerPilot API response types

export interface SPServer {
  id: string;
  name: string;
  autoupdates: boolean;
  firewall: boolean;
  deny_unknown_domains: boolean;
  lastaddress: string;
  lastconn: string;
  datecreated: number;
  plan: string;
  available_runtimes: string[];
}

export interface SPApp {
  id: string;
  name: string;
  sysuserid: string;
  serverid: string;
  runtime: string;
  ssl: {
    key: string;
    cert: string;
    cacerts: string | null;
    auto: boolean;
    force: boolean;
  } | null;
  autossl: boolean;
  domains: string[];
  datecreated: number;
  wordpress: {
    site_title: string;
    admin_user: string;
    admin_password?: string;
    admin_email: string;
    login_url: string;
  } | null;
}

export interface SPSysUser {
  id: string;
  name: string;
  serverid: string;
}

export interface SPDatabase {
  id: string;
  name: string;
  appid: string;
  serverid: string;
  user: {
    id: string;
    name: string;
    password?: string;
  };
}

export interface SPSSHKey {
  id: string;
  name: string;
  sha256_fingerprint: string;
  date_created: string;
}

export interface SPActionResponse {
  actionid: string;
  data: Record<string, unknown>;
}

// API response wrappers
export interface SPListResponse<T> {
  data: T[];
}

export interface SPSingleResponse<T> {
  data: T;
}
