import type { SiteCertificate } from "./types";

export type SSLInput = {
  mode: "none" | "letsencrypt" | "zerossl" | "files" | "existing";
  email: string;
  cert_file: string;
  key_file: string;
  certificate_id: string;
  redirect: boolean;
};
export type VhostCreateInput = {
  request_id: string;
  name?: string;
  domain: string;
  aliases: string[];
  root: string;
  php: string;
  path_info: boolean;
  rewrite: string;
  access_log: boolean;
  log_name: string;
  ipv6: boolean;
  ssl: SSLInput;
  database: {
    enabled: boolean;
    name: string;
    password: string;
    admin_password: string;
  };
};
export type VhostCreateOptions = {
  php: { id: string; label: string; path_info_available: boolean }[];
  rewrites: string[];
  certificates: {
    id: string;
    cert_file: string;
    key_file: string;
    certificate: SiteCertificate;
    usable: boolean;
    error?: string;
  }[];
  acme_available: boolean;
  database_available: boolean;
  database_auth_available: boolean;
  warnings: string[];
};
export type VhostJobLine = {
  id: number;
  time: string;
  phase: string;
  text: string;
};
export type VhostJob = {
  id: string;
  name: string;
  status: "running" | "succeeded" | "partial" | "failed";
  phase: string;
  created_at: string;
  updated_at: string;
  lines: VhostJobLine[];
  result?: {
    name: string;
    site_created: boolean;
    database: string;
    ssl: string;
    message: string;
  };
  error?: string;
};
export type DNSCheck = {
  server_ips: string[];
  warnings: string[];
  records: {
    domain: string;
    addresses: string[];
    matched: string[];
    status: "matched" | "partial" | "mismatch" | "no_records" | "unknown";
    error?: string;
  }[];
};
export function splitDomains(value: string): string[] {
  return [...new Set(value.split(/[\s,，]+/).filter(Boolean))];
}
function hostname(value: string): string {
  try {
    return new URL(`http://${value.trim()}`).hostname
      .toLowerCase()
      .replace(/\.$/, "");
  } catch {
    return "";
  }
}
export function certificateCovers(names: string[], domains: string[]): boolean {
  return (
    domains.length > 0 &&
    domains.every((value) => {
      const domain = hostname(value);
      return (
        domain !== "" &&
        names.some((name) => {
          name = name.toLowerCase();
          if (!name.startsWith("*.")) return hostname(name) === domain;
          const suffix = hostname(name.slice(2));
          return (
            domain.endsWith(`.${suffix}`) &&
            domain.split(".").length === suffix.split(".").length + 1
          );
        })
      );
    })
  );
}
export function newVhostInput(): VhostCreateInput {
  return {
    request_id: crypto.randomUUID(),
    domain: "",
    aliases: [],
    root: "",
    php: "default",
    path_info: false,
    rewrite: "none",
    access_log: false,
    log_name: "",
    ipv6: false,
    ssl: {
      mode: "none",
      email: "",
      cert_file: "",
      key_file: "",
      certificate_id: "",
      redirect: true,
    },
    database: { enabled: false, name: "", password: "", admin_password: "" },
  };
}
