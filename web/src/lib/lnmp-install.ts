import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

export type InstallModule =
  "vts" | "geoip2" | "fancyindex" | "substitutions_filter";
export const defaultModules: InstallModule[] = [
  "vts",
  "geoip2",
  "fancyindex",
  "substitutions_filter",
];
export interface InstallOptions {
  revision: string;
  databases: { id: number; name: string }[];
  php: { id: number; name: string }[];
  default_database: number;
  default_php: number;
  default_allocator: number;
  architecture: string;
  ipv4: string;
  modules: { id: InstallModule; name: string; repository: string }[];
}
export interface InstallRequest {
  id: string;
  revision: string;
  database: number;
  binary: boolean;
  database_password: string;
  innodb: boolean;
  php: number;
  allocator: number;
  protection: "random" | "basic";
  auth_user: string;
  auth_password: string;
  ipv4: string;
  nginx_modules: InstallModule[];
}
export interface InstallJob {
  id: string;
  status:
    "preparing" | "ready" | "running" | "succeeded" | "failed" | "interrupted";
  phase: string;
  created_at: string;
  started_at?: string;
  updated_at: string;
  error?: string;
  options?: InstallOptions;
  nginx_modules?: InstallModule[];
  module_revisions?: Record<string, string>;
  result?: string;
}
export interface InstallSnapshot {
  installed: boolean;
  blocked: string[];
  job?: InstallJob;
}
export interface InstallLine {
  id: number;
  time: string;
  phase: string;
  text: string;
}
export interface InstallLogPage {
  lines: InstallLine[];
  cursor: number;
  more: boolean;
}
export const installationKey = ["lnmp-installation"];
export const activeInstall = (job?: InstallJob) =>
  job?.status === "preparing" || job?.status === "running";
// Preparation and configuration are outside installation time. Legacy jobs
// without a recorded start must not fall back to task creation time.
export function installElapsedSeconds(
  job: InstallJob | undefined,
  now: number,
): number | null {
  if (
    !job ||
    job.status === "preparing" ||
    job.status === "ready" ||
    !job.started_at
  )
    return null;
  const start = Date.parse(job.started_at);
  const end = job.status === "running" ? now : Date.parse(job.updated_at);
  if (!Number.isFinite(start) || start <= 0 || !Number.isFinite(end))
    return null;
  return Math.max(0, Math.floor((end - start) / 1000));
}
export function binaryAllowed(db: number, arch: string) {
  if (db === 0 || db === 1) return false;
  if (arch === "amd64" || arch === "x86_64") return true;
  if (arch === "386" || arch === "i686") return db <= 8 || db >= 11;
  return (arch === "arm64" || arch === "aarch64") && (db === 5 || db >= 11);
}
export function initialInstallRequest(job: InstallJob): InstallRequest {
  const o = job.options!;
  return {
    id: job.id,
    revision: o.revision,
    database: o.default_database,
    binary: binaryAllowed(o.default_database, o.architecture),
    database_password: "",
    innodb: true,
    php: o.default_php,
    allocator: o.default_allocator,
    protection: "random",
    auth_user: "",
    auth_password: "",
    ipv4: o.ipv4,
    nginx_modules: [...defaultModules],
  };
}
export function useInstallation() {
  return useQuery({
    queryKey: installationKey,
    queryFn: () => api<InstallSnapshot>("/lnmp-installation"),
    refetchInterval: (q) => (activeInstall(q.state.data?.job) ? 3000 : 30000),
    retry: 1,
  });
}
