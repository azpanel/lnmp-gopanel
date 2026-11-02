import type {
  Destination,
  PathDiagnostic,
  PathsResult,
} from "./backup-policies";

export function policyMessage(message: string) {
  return message.replace(
    /^(?:(?:组件安装参数无效|备份政策参数无效)[：:]\s*)+/,
    "",
  );
}

export function destinationFields(d: Destination): Record<string, string> {
  const issues: Record<string, string> = {};
  if (d.type === "local") {
    if (!d.path) issues.path = "请填写本地目录";
    else if (
      !d.path.startsWith("/") ||
      (d.path !== "/" &&
        (d.path.endsWith("/") ||
          d.path.includes("//") ||
          d.path.split("/").some((part) => part === "." || part === "..")))
    )
      issues.path = "请输入规范的绝对路径，例如 /var/backups/websites";
  } else if (d.type === "gdrive") {
    if (!d.account) issues.account = "请选择 gdrive 账户";
    if (!d.folder_id)
      issues.folder_id = "请填写文件夹 ID，或填写 root 使用根目录";
    else if (!/^[\w-]+$/.test(d.folder_id))
      issues.folder_id = "文件夹 ID 只能包含字母、数字、下划线和短横线";
  } else {
    if (!d.remote) issues.remote = "请选择 rclone 远程";
    if (/[\\:]/.test(d.path ?? "") || (d.path ?? "").split("/").includes(".."))
      issues.path = "远程内路径不能包含冒号、反斜线或上级目录";
  }
  return issues;
}

export function destinationDiagnostics(
  destinations: Destination[],
  paths: PathsResult | undefined,
  touched: Record<string, boolean>,
  submitted: boolean,
): PathDiagnostic[] {
  return destinations.flatMap((d, index) => {
    const fields = destinationFields(d);
    const value = (control: string) =>
      String((d as unknown as Record<string, unknown>)[control] ?? "");
    const server =
      paths?.errors.filter(
        (item) => item.field === "destinations" && item.index === index,
      ) ?? [];
    const visible = (control: string) =>
      submitted ||
      touched[`${index}:${control}`] ||
      (!!value(control) && server.some((item) => item.control === control));
    const issues: PathDiagnostic[] = Object.entries(fields)
      .filter(([control]) => visible(control))
      .map(([control, message]) => ({
        field: "destinations",
        index,
        control,
        message,
      }));
    for (const item of server) {
      // Incomplete newly-added rows stay quiet until blur or submit, including
      // responses from an older helper without control-level diagnostics.
      if (Object.keys(fields).length && !item.control) continue;
      if (item.control && (!visible(item.control) || fields[item.control]))
        continue;
      issues.push({ ...item, message: policyMessage(item.message) });
    }
    return issues;
  });
}
