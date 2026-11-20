// Function purposes follow the PHP manual. Availability still depends on the
// installed PHP version, extensions and SAPI; this dictionary is explanatory.
const descriptions: Record<string, string> = {
  passthru:
    "执行外部命令并直接输出原始结果，包括二进制数据。禁用后，依赖此方式生成图片或调用外部工具的代码无法使用它。",
  exec: "执行外部命令，并可读取输出和退出状态。禁用后，依赖此函数的命令行工具调用会失败。",
  system: "执行外部命令并输出结果。禁用后，脚本无法通过此函数运行系统命令。",
  chroot:
    "改变当前进程的根目录。此功能需要 root 权限，并受操作系统和 PHP 运行方式限制；禁用后无法通过它切换根目录。",
  chgrp:
    "修改文件所属的用户组。禁用后，依赖此函数调整文件组权限的代码无法执行该操作。",
  chown:
    "修改文件所有者。禁用后，脚本无法通过此函数变更文件归属；操作本身仍受系统权限约束。",
  shell_exec:
    "通过 shell 执行命令并返回完整输出。禁用后，依赖此函数读取外部命令结果的代码无法正常运行。",
  proc_open:
    "启动外部进程，并建立标准输入、输出和错误流。禁用后，使用此函数的子进程、任务执行及部分组件会受影响。",
  proc_get_status:
    "查询由 proc_open 创建的进程状态，包括 PID、运行状态和退出信息。禁用后，相关代码无法用它跟踪子进程。",
  popen:
    "执行命令并打开单向管道，用于读取输出或写入输入。禁用后，依赖此管道方式的外部工具调用会失败。",
  ini_alter:
    "ini_set 的别名，用于在脚本执行期间修改允许调整的 PHP 配置。禁用此别名不代表同时禁用 ini_set。",
  ini_restore:
    "将指定 PHP 配置项恢复为初始值。禁用后，脚本无法通过此函数撤销运行期间的配置调整。",
  dl: "在运行时加载 PHP 扩展。仅部分 PHP 运行方式支持，PHP-FPM 通常不可用；禁用后也无法通过它动态加载扩展。",
  openlog:
    "设置系统日志连接的标识和选项。禁用后，依赖此函数初始化 syslog 日志的代码会受影响。",
  syslog:
    "向系统日志服务写入消息。禁用后，使用此函数记录日志的代码无法向 syslog 输出。",
  readlink:
    "读取符号链接指向的路径。禁用后，依赖此函数解析链接目标的文件处理代码会受影响。",
  symlink:
    "创建符号链接。禁用后，脚本无法通过此函数创建链接，依赖链接的部署或文件操作会受影响。",
  popepassthru:
    "未识别的标准 PHP 函数名，可能来自历史配置或拼写错误。此处保留原名，不会自动替换为其他函数。",
  stream_socket_server:
    "创建网络或 Unix 域服务端套接字。禁用后，依赖此函数监听连接的服务或异步任务组件会受影响。",
};

export function phpFunctionDescription(name: string): string {
  const key = name.toLowerCase();
  return Object.hasOwn(descriptions, key)
    ? descriptions[key]
    : "暂未收录此函数的用途说明。此项来自当前配置；移除会将它从全局禁用列表中删除，进程池仍可能额外禁用同名函数。";
}
