import type { StorageMount } from "./types";

export type MountSortKey =
  "path" | "device" | "fs_type" | "used" | "total" | "free" | "percent";
export type MountSort = {
  key: MountSortKey;
  direction: "ascending" | "descending";
};
const collator = new Intl.Collator("zh-CN", {
  numeric: true,
  sensitivity: "base",
});

function value(mount: StorageMount, key: MountSortKey): string | number | null {
  if (key === "path" || key === "device" || key === "fs_type")
    return mount[key];
  if (mount.error || mount.total == null || mount.total <= 0) return null;
  const number = mount[key];
  return number != null && Number.isFinite(number) ? number : null;
}

export function sortMounts(
  mounts: readonly StorageMount[],
  sort: MountSort | null,
) {
  if (!sort) return [...mounts];
  return [...mounts].sort((a, b) => {
    const left = value(a, sort.key),
      right = value(b, sort.key);
    if (left == null && right != null) return 1;
    if (right == null && left != null) return -1;
    const compared =
      left == null || right == null
        ? 0
        : typeof left === "number" && typeof right === "number"
          ? left - right
          : collator.compare(String(left), String(right));
    return (
      compared * (sort.direction === "ascending" ? 1 : -1) ||
      collator.compare(a.path, b.path) ||
      collator.compare(a.device, b.device)
    );
  });
}
