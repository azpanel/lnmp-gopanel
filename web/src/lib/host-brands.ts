import type { Host } from "./types";

export type HostBrand =
  | "ubuntu"
  | "debian"
  | "centos"
  | "rockylinux"
  | "almalinux"
  | "redhat"
  | "fedora"
  | "opensuse"
  | "alpinelinux"
  | "archlinux"
  | "linux"
  | "intel"
  | "amd"
  | "arm";

const platforms: Readonly<Record<string, HostBrand>> = {
  ubuntu: "ubuntu",
  debian: "debian",
  centos: "centos",
  centosstream: "centos",
  rocky: "rockylinux",
  rockylinux: "rockylinux",
  alma: "almalinux",
  almalinux: "almalinux",
  rhel: "redhat",
  redhat: "redhat",
  redhatenterpriselinux: "redhat",
  fedora: "fedora",
  opensuse: "opensuse",
  opensuseleap: "opensuse",
  opensusetumbleweed: "opensuse",
  alpine: "alpinelinux",
  alpinelinux: "alpinelinux",
  arch: "archlinux",
  archlinux: "archlinux",
};

export function hostBrands(host?: Host) {
  const brands: Partial<
    Record<"platform" | "kernel" | "arch" | "cpu_model", HostBrand>
  > = {};
  if (!host) return brands;

  const platform = host.platform
    .trim()
    .toLowerCase()
    .replace(/[\s_-]/g, "");
  const knownPlatform = Object.hasOwn(platforms, platform)
    ? platforms[platform]
    : undefined;
  const linux = host.os.trim().toLowerCase() === "linux" || !!knownPlatform;
  brands.platform = knownPlatform ?? (linux ? "linux" : undefined);
  if (host.kernel.trim() && linux) brands.kernel = "linux";
  if (/^(arm|arm64|aarch64)$/i.test(host.arch.trim())) brands.arch = "arm";

  const model = host.cpu_model.trim();
  if (/\bintel\b/i.test(model)) brands.cpu_model = "intel";
  else if (/\bamd\b|\badvanced micro devices\b/i.test(model))
    brands.cpu_model = "amd";
  else if (/\barm(?:v\d+\w*)?\b/i.test(model)) brands.cpu_model = "arm";

  return brands;
}
