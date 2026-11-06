export function hasNewerStableVersion(installed?: string, latest?: string) {
  const current = /^(\d+)\.(\d+)(?:\.(\d+))?([a-zA-Z0-9.+-]*)$/.exec(
    installed ?? "",
  );
  const target = /^(\d+)\.(\d+)(?:\.(\d+))?$/.exec(latest ?? "");
  if (!current || !target) return false;

  for (let index = 1; index <= 3; index++) {
    const difference = Number(target[index] ?? 0) - Number(current[index] ?? 0);
    if (difference !== 0) return difference > 0;
  }

  // A stable release also upgrades a prerelease of the same version.
  return /^-?(?:alpha|beta|rc|dev|preview|a|b)\d*(?:[.-]|$)/i.test(current[4]);
}
