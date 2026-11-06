export type BackupArtifact = {
  object: string;
  name: string;
  category: "databases" | "files";
  backup_at: string;
  bytes?: number;
  size_display?: string;
};
export type BackupArtifactList = {
  items: BackupArtifact[];
  checked_at: string;
  truncated: boolean;
  warning?: string;
};
export const backupArtifactPageSize = 20;
export function artifactPage(items: BackupArtifact[], page: number) {
  const pages = Math.max(1, Math.ceil(items.length / backupArtifactPageSize));
  const current = Math.min(pages, Math.max(1, page));
  return {
    current,
    pages,
    items: items.slice(
      (current - 1) * backupArtifactPageSize,
      current * backupArtifactPageSize,
    ),
  };
}
