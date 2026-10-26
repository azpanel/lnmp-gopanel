export type Memory = { used: number; total: number; percent: number };
export type Rates = { read: number; write: number };
export type Disk = {
  path: string;
  device: string;
  used: number;
  total: number;
  percent: number;
};
export type Snapshot = {
  timestamp: string;
  cpu: number | null;
  memory: Memory | null;
  swap: Memory | null;
  load: { one: number; five: number; fifteen: number } | null;
  uptime: number | null;
  network: Rates | null;
  disk_io: Rates | null;
  disks: Disk[];
  errors: Record<string, string>;
};
export type Host = {
  hostname: string;
  os: string;
  platform: string;
  version: string;
  kernel: string;
  arch: string;
  cores: number;
  cpu_model: string;
};
export type Service = {
  name: string;
  state: "running" | "stopped" | "unknown" | "transitioning";
  manager: string;
  actions: string[];
  message: string;
  checked_at: string;
  busy: boolean;
  version: string;
  version_error?: string;
  version_checked_at: string;
};
export type ServiceRelease = {
  status: "loading" | "ready" | "failed";
  version: string;
  source_url: string;
  changelog_url: string;
  changelog: string;
  changelog_error?: string;
  error?: string;
  checked_at: string | null;
  attempted_at: string | null;
};
export type Session = {
  username: string;
  csrf_token: string;
  expires_at: string;
};

export type StorageMount = {
  path: string;
  device: string;
  fs_type: string;
  options: string[];
  total: number | null;
  used: number | null;
  free: number | null;
  percent: number | null;
  error?: string;
};
export type MountList = { timestamp: string; mounts: StorageMount[] };
export type LargeFile = { path: string; size: number };
export type StorageScan = {
  status: "idle" | "running" | "completed" | "partial" | "failed";
  root: string;
  started_at: string | null;
  finished_at: string | null;
  message: string;
  files: LargeFile[];
  scanned: number;
  total_bytes: number;
  complete: boolean;
  partial: boolean;
};

export type SiteCertificate = {
  path: string;
  issuer: string;
  subject: string;
  dns_names: string[];
  not_before: string | null;
  not_after: string | null;
  days_remaining: number;
  serial: string;
  fingerprint: string;
  status: "valid" | "expired" | "expiring" | "not_yet_valid" | "unavailable";
  error?: string;
};
export type SiteRoot = {
  configured: string;
  path: string;
  public_parent: boolean;
  error?: string;
};
export type Vhost = {
  name: string;
  filename: string;
  enabled: boolean;
  domains: string[];
  listens: string[];
  roots: SiteRoot[];
  proxies: string[];
  certificates: SiteCertificate[];
  modified: string;
  warnings: string[];
  error?: string;
};
export type SiteUsage = {
  path: string;
  bytes: number | null;
  error?: string;
  updated_at: string;
};
export type VhostSnapshot = {
  status: "idle" | "loading" | "scanning" | "ready" | "failed";
  sites: Vhost[];
  usage: Record<string, SiteUsage>;
  updated_at: string | null;
  error?: string;
};
export type VhostConfig = { filename: string; content: string };

export type WebsiteScreenshot = {
  name: string;
  url: string;
  language: string;
  load_timeout_seconds: number;
  auth_enabled: boolean;
  username: string;
  password_saved: boolean;
  status: "queued" | "running" | "ready" | "failed";
  error?: string;
  captured_at: string | null;
  captured_url?: string;
  image_version?: string;
};
export type ScreenshotSnapshot = {
  items: Record<string, WebsiteScreenshot>;
  browser_available: boolean;
  browser_error?: string;
};

export type DockerContainer = {
  id: string;
  name: string;
  image: string;
  command: string;
  created_at: string;
  running_for: string;
  state: string;
  status: string;
  ports: string;
  size: string;
};
export type DockerDiskUsage = {
  type: string;
  total: number;
  active: number;
  size: string;
  reclaimable: string;
};
export type DockerSnapshot = {
  status: "idle" | "loading" | "ready" | "partial" | "failed" | "unavailable";
  containers: DockerContainer[] | null;
  disk_usage: DockerDiskUsage[] | null;
  updated_at: string | null;
  message?: string;
  errors: Record<string, string>;
};
export interface CronTask {
  line: number;
  schedule: string;
  command: string;
}

export interface CronSnapshot {
  tasks: CronTask[];
  raw: string;
  warnings: string[];
  updated_at: string;
}

export interface CronPreview {
  line: number;
  schedule: string;
  timezone: string;
  times: string[];
  message?: string;
}

export type ProgrammingComponent = {
  name: string;
  status: "loading" | "ready" | "missing" | "failed";
  version: string;
  path: string;
  error?: string;
  checked_at: string | null;
};
export type ComponentSnapshot = {
  status: "idle" | "loading" | "ready" | "partial" | "failed";
  items: ProgrammingComponent[];
  error?: string;
};
