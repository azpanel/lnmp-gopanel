# LNMP GoPanel

面向 [lnmp.org](https://lnmp.org/) 安装环境的单机服务器监控与管理面板。支持实时监控、LNMP 服务管理、网站与 SSL 管理，以及空白服务器上的 LNMP 在线安装。

中文界面，支持深浅主题和移动端导航。面板自身无需外部数据库或 Redis，生产运行无需安装 Go、Node.js；网站截图另需本机 Chromium / Chrome。

## 功能概览

| 功能      | 当前支持                                                                                                               |
| --------- | ---------------------------------------------------------------------------------------------------------------------- |
| 系统监控  | CPU、内存、Swap、负载、运行时间、磁盘与网络；可选进程排行及 Nginx、PHP-FPM、数据库状态指标                             |
| LNMP 服务 | Nginx、PHP-FPM、MySQL / MariaDB 状态与启停、重启；Nginx 重载前检查配置、管理方式修复；Redis 状态只读                   |
| 版本检查  | 服务及 phpMyAdmin、Python 3、Go、Node.js、Codex CLI、Composer 本机版本、最新正式版本和更新日志；部分组件可设置查询范围 |
| LNMP 安装 | 准备源码、选择数据库与 PHP、可选 Nginx 模块、后台安装任务和实时日志                                                    |
| 网站管理  | 查看配置与证书、启停站点、添加 PHP / 静态站点、可选建库、SSL 申请与批量提前续签、Basic Auth、配置格式化、网站截图      |
| 日志读取  | 登录、网站、慢查询与 PHP 错误日志，以及自定义文本日志；历史加载、实时追踪、关键词／正则筛选、表格与 IP 属地展示        |
| 定时任务  | 只读查看 root crontab、原文及之后 5 次预计执行时间                                                                     |
| Docker    | 只读查看运行中容器及镜像、容器、数据卷、构建缓存空间统计                                                               |
| 硬盘空间  | 挂载容量、筛选与排序、主文件系统大文件 Top 30 与矩形树图                                                               |
| 账户安全  | 单管理员、密码修改、TOTP 双重验证、一次性恢复码和登录审计                                                              |

## 安装与维护

支持 Linux amd64 / arm64，需要运行中的 systemd。以 root 或具备 sudo 权限的账号执行；Debian / Ubuntu 可先安装依赖：

```bash
sudo apt-get install -y curl ca-certificates tar gzip sudo
curl -fsSL https://raw.githubusercontent.com/azpanel/lnmp-gopanel/main/install.sh -o install.sh
sudo bash install.sh
```

直接运行显示中文管理菜单，可选择安装、更新、卸载、重置密码或配置服务监控。脚本从 GitHub Release 下载对应架构的安装包并校验 SHA-256，创建专用普通账号和 systemd 服务。**安装面板后，LNMP 需按需另行安装。**

首次安装设置管理员密码并选择自签名证书或已有证书。面板仅提供 HTTPS，默认在所有 IPv4 网卡上随机选择空闲端口；安装结束后显示访问地址，请在安全组和防火墙中放行对应 TCP 端口。使用自签名证书时，浏览器需要信任证书。

```bash
sudo bash install.sh --update             # 更新面板，保留现有配置与认证状态
sudo bash install.sh --configure-metrics  # 交互配置服务监控
sudo bash install.sh --reset-password     # 重置密码，同时清除 TOTP 和恢复码
sudo bash install.sh --recover            # 离线恢复中断的安装／升级
sudo bash install.sh --uninstall          # 确认后删除面板、配置、数据及备份
```

更新会重启面板，已有会话需重新登录。卸载保留 LNMP、网站、数据库、Docker、网站证书与续期任务、系统依赖；面板配置及备份会永久删除，请先保存需要保留的内容。

<details>
<summary>安装参数、离线安装与版本兼容</summary>

安装脚本还依赖 `tar`、`gzip`、`sudo`（含 `visudo`）和常见系统工具；运行时无需 OpenSSL，网站 Basic Auth 的哈希生成另需可信的 `/usr/bin/openssl`。

- `--install` 安装面板，已有安装会覆盖升级；`--update` 要求已有完整安装，并从 GitHub Release 下载。
- `--version TAG` 指定 Release 标签，`--username NAME` 设置首次安装的用户名。
- `--check` 只下载并验证安装包，不安装系统依赖、不修改面板，也不恢复待处理事务。
- `--archive FILE` 使用本地安装包，默认同时读取 `FILE.sha256`；可用 `--checksum-file FILE` 指定校验文件。适用于离线安装和当前源码打包部署。
- `--no-start` 安装后不启用或重启面板。检查后执行 `sudo systemctl enable --now lnmp-gopanel`；已有运行实例升级后需执行 `sudo systemctl restart lnmp-gopanel`。
- `--password-stdin` 从标准输入读取新密码。非交互首次安装还需指定 `--tls-mode self-signed`，或 `--tls-mode existing --certificate-dir /usr/local/nginx/conf/ssl/域名`。密码不接受命令行参数。
- `--uninstall --yes` 非交互完全卸载，无需联网。

本地安装示例，arm64 使用对应文件名：

```bash
sudo bash install.sh --archive ./dist/lnmp-gopanel_linux_amd64.tar.gz
```

主分支管理脚本和已发布安装包可能有版本差异。脚本会在询问证书和密码前检查 HTTPS 初始化能力，不兼容时明确拒绝；`--check` 也会检查。请使用配套 Release，或从当前源码构建、打包后用 `--archive` 安装。旧版脚本缺少新参数时，应重新下载脚本；仅替换脚本不会增加旧二进制的功能。

</details>

<details>
<summary>HTTPS 证书、监听地址与续期</summary>

证书有两种来源：

- 自动生成自签名证书：无需填写域名、组织等表单，自动使用 ECDSA P-256 密钥、随机序列号、一年有效期，以及 localhost、本机主机名和网卡 IP 的 SAN。TLS 最低版本为 1.2，浏览器需要信任该证书；服务器公网 IP 经 NAT 映射时可能不在自动发现的 SAN 中。
- 借用现有证书：扫描 `/usr/local/nginx/conf/ssl` 下的目录，仅列出存在有效证书及匹配的未加密私钥的目录，支持 `.pem`、`.crt`、`.cer`、`.key` 及 ACME 符号链接，优先使用 fullchain。选择后复制到面板专用目录，不修改源证书或私钥权限。使用证书覆盖的域名访问。

证书和私钥安装到 `/etc/lnmp-gopanel/tls/`，目录权限 `0750`、文件权限 `0640`、所有者 `root:lnmp-panel`。借用的证书不会自动跟随原证书续期；续期后重新执行 `sudo bash install.sh --certificate-dir /usr/local/nginx/conf/ssl/域名`，同步证书并重启面板，已有 HTTPS 端口保持不变。自签名证书可通过 `--tls-mode self-signed` 重新生成。

脚本下载最新 Release 中对应架构的预编译安装包，验证 SHA-256，创建普通服务账号并安装面板、特权辅助程序、精确的 sudoers 规则及 systemd 服务。首次安装交互选择 SSL 证书并设置管理员密码（12–72 字节），检测本机 LNMP / Redis 服务，并启动面板。面板仅提供 HTTPS，监听 `0.0.0.0` 上由系统随机分配的空闲端口；安装完成后显示监听地址和端口，需在安全组及防火墙中放行该 TCP 端口。运行时无需 Go、Node.js 或 OpenSSL。

</details>

<details>
<summary>可选截图依赖与 Ubuntu 浏览器配置</summary>

如需网站截图，可在 Debian 12/13 或 Ubuntu 22.04/24.04/26.04（amd64 / arm64）使用 `sudo bash install.sh --with-screenshots`，额外安装 Chromium、沙箱组件和中文字体，并以面板普通账号运行截图冒烟检查。已有原生 Chromium/Chrome 时优先复用；其他 Linux 发行版请自行安装浏览器，通过 `screenshots.browser` 指定其绝对路径。普通安装不安装浏览器，缺少浏览器只影响截图功能。`--check` 不安装系统依赖；可选安装产生的系统软件包和 APT 源配置不会在面板文件回滚或卸载时删除。

Ubuntu 的官方 `chromium-browser` 是转到 Snap 的过渡包，Snap 的账号主目录和文件访问限制与面板账号及截图临时目录不兼容。因此 Ubuntu 自动安装使用第三方 [XtraDeb PPA](https://launchpad.net/~xtradeb/+archive/ubuntu/apps) 提供的原生 deb 包，跳过已有的 Snap 启动脚本，不关闭浏览器沙箱。脚本通过独立密钥文件及完整指纹校验限定签名来源，并将该源限定为仅提供 `chromium*` 软件包；后续可通过 APT 更新。相关配置位于 `/etc/apt/sources.list.d/lnmp-gopanel-chromium.sources`、`/etc/apt/preferences.d/lnmp-gopanel-chromium` 和 `/etc/apt/keyrings/lnmp-gopanel-chromium.asc`。不希望添加第三方源时，请先自行安装原生 Chromium/Chrome。

Ubuntu 24.04 及更新版本若启用了 AppArmor 用户命名空间限制，脚本会加载系统已有的 `/etc/apparmor.d/chromium`；没有该配置时创建 `/etc/apparmor.d/lnmp-gopanel-chromium`，仅允许 `/usr/lib/chromium/chromium` 使用用户命名空间，并在缺少解析器时安装 `apparmor`。不会修改全局内核限制。该浏览器配置与 Chromium 一同保留，面板回滚或卸载时不删除。

</details>

<details>
<summary>升级备份、授权自检与中断恢复</summary>

在菜单选择更新或执行 `sudo bash install.sh --update` 即可从 GitHub Release 升级；带安装参数再次执行也可覆盖升级。已有 HTTPS 配置、端口、证书、管理员密码和 `services.yaml` 会保留。旧版 HTTP 安装升级时会询问证书来源、迁移为随机端口的 HTTPS，并保留管理员凭据及服务配置；原 HTTP 端口不再使用，需调整安全组或反向代理。升级只重启面板服务，不重启 Nginx、PHP、数据库、Redis 或 Docker；已有面板会话需重新登录。替换前的文件保存在 `/var/backups/lnmp-gopanel/`，替换或启动失败时自动恢复旧文件，包含原配置及证书。

升级备份同时保存版本化事务记录：八个受管文件原先是否存在、内容摘要、权限和所有者，以及面板原先的运行和启用状态。备份和待恢复标记持久化后才开始替换，全部安装检查通过后才提交；事务标记位于 `/var/backups/lnmp-gopanel/pending`。如果升级因强杀、断电等原因中断，下次运行安装或更新命令会在联网和检查安装包之前先恢复旧文件及原服务状态，成功后继续本次安装。恢复过程再次中断也可以重复执行。服务器启动本身不会自动恢复；`--no-start` 只控制本次正常安装，恢复仍按中断事务记录中的原服务状态执行。

仅恢复、不联网或继续升级时运行：

```bash
sudo bash install.sh --recover
```

没有待恢复事务时该命令直接成功退出，不依赖安装包、curl 或 sudo。`--recover` 不接受安装参数，也不能与其他操作或 `--yes` 合用。存在待恢复事务时，卸载、密码重置和监控配置会停止并提示先恢复；`--check` 和 `--help` 不执行恢复。恢复仅限原有八个受管文件与面板服务，不回退账号、系统依赖或 `/var/lib/lnmp-gopanel` 中的可变数据。旧安装器留下的、没有新事务记录的历史备份不会被自动识别为待恢复事务。

恢复前先验证记录、备份摘要和路径安全性；备份缺失或损坏时不会把它当作“原文件不存在”而删除目标。文件恢复不完整时不会启动面板。恢复失败会以非零状态退出，分别报告文件、服务和事务清理结果，保留备份及可重试的事务状态，并尽力将诊断保存到对应备份目录的 `recovery-report.txt`。修复磁盘空间、权限或服务配置后再次运行 `--recover`；同时可运行 `systemctl status lnmp-gopanel`、`journalctl -u lnmp-gopanel` 排查。不要删除待恢复标记来绕过失败。若升级已提交但标记清理失败，下次只完成事务清理，不会回滚已提交的新版本。

安装时会以 `lnmp-panel` 账号检查服务状态、Docker 查询、文件扫描、定时任务、日志读取及虚拟主机全部十二项操作的精确 sudo 授权，并实际免密调用只读 `docker snapshot`、`components snapshot` 辅助程序（无需安装或启动 Docker，不触发全盘扫描或服务启停）。虚拟主机只检查 `list/config/format/usage/toggle/options/create/ssl-renew-preview/ssl-renew/basic-auth-get/basic-auth-set/basic-auth-disable` 授权，不读取站点或证书账号、不创建站点或申请证书。启动面板后读取实际进程的 `/proc/<MainPID>/status`，确认 `NoNewPrivs: 0`，避免 systemd 隐式限制使所有辅助功能不可用；检查失败会报错并回滚。`--no-start` 只执行辅助程序检查，进程限制需在自行启动后验证。

</details>

<details>
<summary>完全卸载的清理范围</summary>

完全卸载无需联网，会停止并禁用面板，删除面板程序、辅助程序、systemd 单元及专属覆盖配置、sudoers 规则、`/etc/lnmp-gopanel`（含面板证书副本）、`/var/lib/lnmp-gopanel`、`/var/backups/lnmp-gopanel` 和 `lnmp-panel` 专用账号及组。LNMP、网站、数据库、Docker、借用的原始证书和 Chromium 等系统依赖会保留；自定义到其他目录的面板数据请自行清理。卸载会永久删除面板配置及备份，请先保存需要保留的内容。

</details>

## 系统监控

系统概览展示实时资源状态，支持暂停查看。采集由后端统一执行，多浏览器共享；图表只在当前页面内存中保留最近 5 分钟，刷新、退出登录后清空，不保存历史指标。

“添加状态指标”可选择磁盘读写、负载、Swap、内存构成、进程排行及服务状态卡片。服务指标需要已有的本机状态接口或数据库监控连接，也可通过 `sudo bash install.sh --configure-metrics` 配置；保存后在概览中勾选对应卡片。

<details>
<summary>采样频率、统计口径与可选指标</summary>

- 系统采集每 2 秒一次，服务状态每 5 秒一次，磁盘容量每 30 秒一次。
- CPU、磁盘和网络速率需要两次采样；首次或计数器重置时返回 `null`，界面显示计算中或不可用。
- 网络为所有非回环网卡合计；有虚拟网桥、容器网卡的主机可能重复计算同一流量，不等同于外网带宽。
- Linux 磁盘 I/O 统计 `/sys/block` 中的整盘，排除分区、loop、ram、dm 层；复杂 RAID / 虚拟磁盘可能有重叠，应结合实际设备解释。

系统概览右上角的“添加状态指标”可选择八类可选组件：磁盘读写速率、系统负载趋势、Swap 使用趋势、内存构成、进程资源排行、Nginx 活跃连接数、PHP-FPM 进程与队列、数据库状态。首次安装或升级默认不添加；现有资源使用趋势与网络吞吐图固定显示。在选择弹窗中勾选或取消勾选组件，点击“保存”后生效。组件顺序固定，桌面端两列，移动端单列。

组件选择保存在 `metrics.state_dir/dashboard-widgets.json`，标准安装默认 `/var/lib/lnmp-gopanel`，自定义配置默认配置文件同级 `metrics-state/`。目录应属于面板账号，权限 `0700`；文件权限 `0600`，不能是链接。文件缺失表示未选择任何新组件，损坏时不会覆盖。刷新、重新登录或重启面板后保留选择；其他浏览器在聚焦页面或每 30 秒刷新配置时同步，多端同时保存以最后一次成功保存为准。备份该文件可保留选择，升级不覆盖。

趋势数据仍仅保存在当前页面内存中，最近 5 分钟最多 150 个点，不保存历史指标文件。磁盘速率沿用整盘合计口径；负载图包含 1、5、15 分钟负载和逻辑 CPU 核数参考线。Swap 未启用时显示提示。内存堆叠由“其他占用、缓存、缓冲区、空闲”组成，可用内存单独显示（包含部分可回收缓存，不能再次加入堆叠）。进程排行可切换 CPU／内存前 10 名：CPU 为相邻采样区间占整机总算力的比例，内存为 RSS 及其占物理内存比例；首次 CPU 样本等待下一次采集。无法读取的进程不参与排行，并显示不完整提示。

进程和服务探测仅在对应组件被选中时由服务器统一执行，多浏览器共享，不额外启动探测。权限受限、响应错误或超时时显示提示，真实的零值正常展示，失败不会伪造零值或沿用旧数值。暂停及断线行为与现有图表一致。

</details>

<details>
<summary>手工接入服务状态接口与指标 API</summary>

在面板 `config.yaml` 中添加以下配置并重启面板。手工填写 URL 时，服务状态接口必须已由管理员启用；面板运行时的采集只查询，不修改服务配置或重载服务。也可使用安装脚本快捷配置向导自动启用入口。仅支持一个 Nginx 状态入口、一个 PHP-FPM 池和一个本机 MySQL／MariaDB 实例。

```yaml
metrics:
  state_dir: /var/lib/lnmp-gopanel
  nginx_status_url: http://127.0.0.1/nginx_status
  php_fpm_status_url: http://127.0.0.1/fpm-status
  mysql:
    socket: /tmp/mysql.sock
    # 或使用 address: 127.0.0.1:3306，此时移除 socket
    user: panel_monitor
    password: "替换为监控账号密码"
```

Nginx 地址须返回 `stub_status` 文本，其中活跃连接包含等待连接。PHP-FPM 地址须直达启用了 `pm.status_path` 的池状态接口，面板自动请求 JSON，读取 `active processes`、`idle processes` 和 `listen queue`。HTTP 地址仅允许字面量回环 IP（如 `127.0.0.1`、`[::1]`），不允许重定向。应将状态接口限制为本机访问。没有配置时，卡片直接提示缺少的配置字段。

数据库使用专用监控账号，通过只读 `SHOW GLOBAL STATUS` 查询 `Threads_connected`（当前连接数，含监控查询连接）和 `Slow_queries`（自数据库启动或计数器重置起累计的慢查询数）。不查询慢 SQL 内容或慢日志，不自动创建账号；Unix socket 和本机 TCP 地址二选一。连接凭据只由后端读取，不返回前端或记录日志。面板配置继续由 root 管理，仅向面板账号提供读取权限。

相关接口（均要求登录）：

| 方法 | 路径                        | 内容                                |
| ---- | --------------------------- | ----------------------------------- |
| GET  | `/api/v1/dashboard/widgets` | 读取 `{ "enabled": [...] }`         |
| PUT  | `/api/v1/dashboard/widgets` | 保存同结构数据，需要 `X-CSRF-Token` |

组件标识为 `disk_io`、`load`、`swap`、`memory_breakdown`、`processes`、`nginx`、`php_fpm`、`database`。空数组移除全部可选组件；未知或重复标识返回 400；配置不可读或不可写返回 503。原指标快照及 SSE 兼容增加内存构成字段和可选的 `processes`、`nginx`、`php_fpm`、`database`；未启用或采集失败时不返回对应可选数据，错误放入同名 `errors` 字段。

</details>

<details>
<summary>服务监控配置向导、回滚与只读检查</summary>

下载与当前版本配套的 `install.sh` 和面板程序后，以 root 运行：

```bash
sudo bash install.sh --configure-metrics
```

也可在管理菜单选择“5) 配置服务监控指标”。交互式首次安装成功后会询问是否配置，默认跳过；更新、非交互安装和 `--no-start` 不自动进入。该独立入口不下载或升级软件，不接受安装参数或 `--yes`；旧版二进制没有向导时，会在修改前提示先升级，仅更新脚本无法增加二进制能力。

向导分别询问 Nginx、PHP-FPM 和数据库，已配置项目默认保留并检查。未检测到运行环境时可跳过，也可填写已有本机 HTTP 地址或数据库连接。所有选择完成后会预览文件路径、监控地址、账号名称和重载动作，确认前不写入配置或创建账号；密码不回显，不作为命令行参数，不记录在日志中。跳过某项会保留原配置，不关闭既有采集，不改变概览页的组件选择。

自动启用适用于 `/usr/local/nginx`、`/usr/local/php` 的 LNMP 环境，要求相关服务正在运行：

- Nginx 默认增加 `127.0.0.1:18080` 本机 HTTP 入口，端口冲突可另选。专用文件为 `/usr/local/nginx/conf/panel-metrics.conf`，主配置 `http` 块只增加一次 include，不放入网站目录。仅生成所选状态路由，其他路径返回 404。原有公网状态页及访问规则保持原样，向导会提示检查。
- PHP-FPM 展示从实际配置及绝对路径 include 中发现的池，选择后复用其业务 socket（或已配置的回环 TCP 监听）。已有 `pm.status_path` 时沿用，否则增加 `/fpm-status`。不新增 `pm.status_listen`；业务池满载时，状态请求也可能排队或超时。复杂、重复或不能安全解析的配置会拒绝自动修改，可改用已有 URL。
- 数据库默认使用检测到的 Unix socket，也可填写回环 TCP 地址。“新建专用账号”默认名称 `panel_monitor`，生成随机密码；需要时隐藏输入管理员凭据。只执行创建账号和必要的检查，新账号只有隐含的 `USAGE`，不授业务库、`PROCESS`、`SUPER` 或复制权限。同名账号存在时停止，要求换名或改选“使用已有账号”；后者仅验证连接及两项状态查询，不重置密码或修改权限。socket 账号来源为 `localhost`，TCP 新账号来源限定 `127.0.0.1` 或 `::1`，不创建 `%` 来源账号。慢查询阈值及日志开关保持原样。

应用前会检查可信文件路径和配置版本，生成 root 私有备份到 `/var/backups/lnmp-gopanel/*-metrics-*/`，其中 `manifest.json` 记录原文件路径和权限，按清单顺序对应 `0.before`、`1.before` 等原始内容（新文件没有 `.before`）。原配置备份为 `0600`。向导的受管入口记录保存在面板配置同目录的 `metrics-setup.json`，用于识别文件及端口；受管文件被手工修改时不会覆盖。面板 `config.yaml` 保持 root 所有、`0640`，原服务配置保留原权限和组。

配置检查通过后，仅重载发生变化的 Nginx／PHP-FPM，再以 `lnmp-panel` 账号验证所选采集；原本运行的面板会重启并检查 HTTPS 是否就绪，原本停止的面板保持停止，数据库不重启。重复运行不会重复增加 include、状态路径或账号；如改变受管 HTTP 端口，会一并更新引用该入口的既有监控 URL。

应用失败或收到中断信号时，恢复本次修改的文件和服务配置，只清理本次已确认创建的新账号；已有账号不变。并发修改的文件不会被恢复操作强行覆盖，恢复失败会列出残留项及备份目录。数据库创建请求遇到网络等错误、无法确认是否成功时，会提示核对账号，不自动删除归属未确认的账号。首次安装后的向导是独立事务，取消或失败不撤销已经成功的面板安装。

需要只读复查时，可以面板账号运行：

```bash
sudo -u lnmp-panel /usr/local/bin/lnmp-gopanel metrics check \
  --config /etc/lnmp-gopanel/config.yaml \
  --only nginx,php_fpm,database
```

`--only` 可只列出已配置的服务，名称为 `nginx`、`php_fpm`、`database`。检查复用运行时采集逻辑，不打开认证状态或写入指标数据。卸载面板仍保留 LNMP 配置和数据库账号；如需清理本功能资源，应先删除专用入口及其 include、恢复本次新增的 PHP 状态设置，检查并重载相关服务，再由数据库管理员删除对应监控账号。勿将 `config.yaml` 中的监控密码当作数据库管理员密码。

</details>

## 系统信息与日志开启

“系统信息”页面的“日志设置”提供“打开PHP错误日志”和“打开DB慢日志”。确认后保存配置并重启对应服务；服务需要已启动。页面展示持久配置状态，已有正确配置及日志文件时不会重复修改或重启。

- PHP 从 `/usr/local/php/etc/php-fpm.conf` 及其绝对路径 include 定位默认 `[www]` 池，设置 `php_admin_value[error_log]` 与 `php_admin_flag[log_errors]`，日志写入 `/usr/local/php/var/log/php_errors.log`，所有权为 `www:www`。仅支持使用 `www` 账号的默认池。
- 数据库修改 `/etc/my.cnf` 的 `[mysqld]`，开启慢日志并输出到 `/var/log/mysql/mariadb-slow.log`，与日志读取页一致。保留已有 `long_query_time`，缺省设置为 2 秒；确保文件输出，已有表输出同时保留。多个数据库服务或其他配置组、include、别名中的冲突设置会提示手动处理。
- 开启前备份配置到 `/var/backups/lnmp-gopanel/logging-*`。PHP-FPM 检查配置后重启；数据库直接修改配置并通过现有服务管理方式重启，开启流程不调用 `mariadbd`／`mysqld` 做配置检查，也不要求数据库程序目录归 root 所有。已有日志内容不清空；PHP 配置检查或服务重启失败时恢复原配置及日志权限，重启已尝试时再尝试恢复原服务。期间发现的管理员并发修改会保留，并提示恢复结果。

升级需同步更新面板、辅助程序及 `logging status`、`logging enable-php`、`logging enable-database` 的 sudoers 精确授权。安装器只检查这三项授权，不执行日志开启或重启服务。

## LNMP 服务与编程组件

“LNMP 服务”页面展示服务状态、已安装版本和可用操作。Nginx、PHP-FPM、MySQL / MariaDB 支持启停与重启，Nginx 还支持检查配置后重载；Redis 只读监控。发现 Nginx 进程与管理单元归属不一致时，会阻止操作，并在符合条件时提供“修复管理方式”。

“编程组件”展示 phpMyAdmin、Python 3、Go、Node.js、Codex CLI、Composer 的版本与路径。服务和组件都可后台查询最新正式版本及更新日志；Nginx、PHP、MariaDB、Node.js、Python 3 支持指定大版本或中版本范围。版本检查只提供信息，组件安装和升级需自行处理。

<details>
<summary>服务状态判定、操作限制与 Nginx 管理修复</summary>

- LNMP 的 Nginx/PHP-FPM init 脚本可能不支持 `status`；这两类服务使用 `/proc` 中的完整可执行文件路径检查进程，避免将 usage 输出当作运行状态。systemd 服务使用对应 unit 状态；Nginx 额外核对真实主进程、MainPID 和进程组归属。实际进程运行在所配置 unit 之外时显示“管理状态异常”，页面和辅助程序均阻止启停、重启及重载，避免重复启动。
- Nginx 出现管理状态异常时，可在服务页选择“修复管理方式”，先预览再确认。仅支持标准 LNMP 路径、唯一且身份可验证的 root 主进程、可信 init 脚本，以及没有 MainPID 的 failed/inactive unit；自定义启动参数、其他服务所属进程、异常 PID 文件等情况需要通过 SSH 检查。修复会先检查 Nginx 配置，按需恢复缺失或空的 PID 记录，并将该服务切换为 init 管理，现有进程保持运行。预览绑定配置与进程启动时间，内容变化后必须重新预览。同一 Nginx 操作共用锁，失败时恢复原配置和 PID 文件内容、权限及组；修复前快照以 `0600` 保存到 `/etc/lnmp-gopanel/services.yaml.nginx-repair-<标识>.json`。成功后面板立即刷新管理方式，无需重启面板。升级时应同步更新两个二进制与 sudoers 中的 `nginx repair-preview`、`nginx repair` 精确授权；安装检查只验证授权，不执行修复。
- 修复回退会再次核对配置和进程身份；若期间有外部运维操作修改配置、PID 或替换主进程，会保留这些新状态并提示通过 SSH 检查，避免用旧快照覆盖管理员的新改动。
- 不支持或无法明确解释的状态返回“未知”；systemd 的切换状态单独显示，不误报停止。
- 服务操作最多执行 30 秒，同服务互斥；超时后重新检查实际状态，不自动重试启停。
- systemd 环境下，init／LNMP 脚本的服务操作通过可信的 `/usr/bin/systemd-run --scope` 在独立控制组中执行；普通服务页和日志开启、失败恢复共用此入口。脚本执行结果同步返回，后台守护进程随 scope 独立存活，面板停止或升级重启不会带停它们，空 scope 自动回收。原生 systemd 服务继续由对应单元管理。创建 scope 失败时返回错误，不降级为面板控制组内直接执行。

服务状态不等同于应用健康。可选的 Nginx、PHP-FPM 和数据库指标在单独配置并勾选卡片后采集；普通服务状态检测不会自动开启状态接口或创建数据库连接。

</details>

<details>
<summary>服务版本来源、查询范围与 Redis 配置</summary>

服务卡片展示 Nginx、PHP-FPM、MySQL / MariaDB、Redis 的已安装版本。版本号通过固定的服务可执行文件及 `-v` / `--version` 参数读取，由面板普通用户执行，不连接数据库、不读取凭据；本机版本成功缓存 1 小时，失败后 1 分钟重试，失败不覆盖上次版本或服务运行状态。

进入“LNMP 服务”页面后，后台独立查询每个已配置服务的最新正式版本。Nginx 默认选择官方 Stable 分支（不包括 Mainline），PHP 使用 php.net 发布接口与 php-src 的 NEWS，MariaDB 使用官方发布接口与 changelog，MySQL 使用官方源码仓库的正式标签及官方 Release Notes，Redis 使用官方 GitHub Releases。默认查询可能跨越本机版本分支，不表示当前安装分支的补丁更新建议。

点击页面右上角“检查最新版本”，在弹出的“版本检查与设置”对话框内统一设置 Nginx、PHP、MariaDB、Node.js、Python 3 的“版本范围”：可填写大版本（如 `8`）或大版本＋中版本（如 `8.3`），查询该范围内最新正式版；留空不限。点击“保存并检查”后生效，对话框内“检查全部版本”使用各服务及组件已保存范围，未保存的输入不影响查询。Nginx 留空或仅指定大版本时查询 Stable／Legacy 稳定分支；明确指定中版本（如 `1.29`）时也允许 Mainline。Node.js 可填写 `22` 或 `22.23`，Python 3 可填写 `3` 或 `3.13`；两项始终可设置，不依赖 LNMP 服务配置或本机是否安装。Python 仅查询 Python 3，Node.js 不额外限制 LTS。历史分支可返回官方仍提供的最后正式版本，不代表仍受维护；无匹配版本时显示失败，不自动改查其他范围。

范围由面板普通用户保存在 `metrics.state_dir/service-release-ranges.json`（生产默认 `/var/lib/lnmp-gopanel/service-release-ranges.json`），所有客户端共用，重启后保留；页面轮询同步保存结果，但不覆盖正在编辑的输入。目录和文件要求面板账号所有，权限为 0700／0600，不接受链接；保存采用原子替换，失败时保留原设置。配置文件不存在时默认全部不限；损坏或读取失败时暂停上述五项版本查询并显示错误，修复文件后重启面板，其他服务和组件继续查询。

`GET /api/v1/services/releases` 各项结果包含 `range` 字段（空串表示不限）。`PUT /api/v1/services/releases/:name/range` 支持已配置的 `nginx`、`php-fpm`、`mariadb`，以及固定展示的 `node`、`python3`，JSON 请求示例为 `{"range":"8.3"}`；清空用 `{"range":""}`。成功返回 202 及该服务查询状态，参数错误返回 400，持久化不可用返回 503。范围只接受一段或两段数字，每段最多 9 位，不接受多余前导零、补丁版本、通配符和预发布后缀。

联网查询由普通面板进程执行，单次网络请求最多 12 秒，每个服务的网络与可取消阶段共用 30 秒预算；同步 HTML 解析不提供硬截止时间。网络访问限定为固定官方 HTTPS 域名，并限制响应大小与重定向。服务之间并行，状态 API 不等待联网；多个客户端按“服务＋版本范围”共享内存缓存、正在执行的查询和刷新限流；切换范围不会显示其他范围结果，同范围失败保留上次成功结果。成功缓存 6 小时，失败或 changelog 不可用时 15 分钟后自动重试；手动检查至少间隔 1 分钟。版本查询失败保留上次结果；changelog 失败仍展示最新版本与官方原文链接。上游限流、网络故障或 HTTP 403 会明确显示原因，不生成替代更新日志。更新日志以纯文本显示，长日志截断并提供完整原文链接，不执行上游 HTML。

首次初始化自动识别 `redis.service` / `redis-server.service` 或 `/etc/init.d/redis` / `/etc/init.d/redis-server`。已有安装升级时，需要在 `/etc/lnmp-gopanel/services.yaml` 中增加实际对应的 Redis 配置，例如：

```yaml
- name: redis
  manager: systemd
  program: /usr/bin/systemctl
  target: redis.service
```

Redis 只显示状态和版本，不提供启停按钮，辅助程序也拒绝 Redis 修改操作。升级时同步安装新面板、辅助程序和 `deploy/lnmp-gopanel.sudoers`（新增精确的 `redis status` 权限，先执行 `visudo -cf` 验证），然后重启面板。其余服务无需增加版本查询的 sudo 权限。

</details>

<details>
<summary>编程组件检测路径、phpMyAdmin 入口与缓存</summary>

“LNMP 服务”页面的“编程组件”分组固定展示 phpMyAdmin、Python 3、Go、Node.js、Codex CLI、Composer 的本机版本、安装路径、检查时间、最新正式版本及对应 Changelog。即使未配置任何 LNMP 服务，也可查看组件信息。仅查询版本，不提供安装、升级或启停操作。

本机版本通过固定的 `components snapshot` 辅助命令读取，检查程序及其父目录的 root 所有权和写权限，不加载 shell 配置。Python、Go 检查标准系统安装目录；Node.js、Codex 优先选择 `/root/.nvm/alias/default` 指定的已安装版本，再检查系统安装。支持数字版本前缀、`node`、`stable`、`lts/*` 和 LTS 名称别名；不跟踪临时终端的 `nvm use`，不扫描其他用户目录。Codex 的 npm 启动脚本使用安装目录中的 Node。Composer 检查 `/usr/local/bin/composer`、`/usr/bin/composer`、`/usr/local/php/bin/composer`，通过受信任的 PHP CLI（优先 `/usr/local/php/bin/php`，其次 `/usr/local/bin/php`、`/usr/bin/php`）仅读取版本，并禁用插件、脚本和交互；不扫描网站内的项目级 Composer。未安装和读取失败分别展示，读取失败保留上次成功版本。

phpMyAdmin 静态读取应用版本文件，不执行站点 PHP 代码。安装时生成的随机路径或 Basic Auth 固定路径由 LNMP 保存在 `/usr/local/etc/lnmp/security.conf`；辅助程序只解析该文件的数据，结合安装时填写的 IPv4 生成 `https://IPv4/实际路径/`，在卡片中提供“打开 phpMyAdmin”链接。配置和应用目录校验成功才显示链接，面板重启及升级后仍可恢复。旧安装缺少保护配置时检测 `/home/wwwroot/default/phpmyadmin`，显示版本但不猜测访问地址。组件 API 的 phpMyAdmin 条目增加可选 `access_url` 字段，未安装或路径无效时不返回该字段。

采集异步执行，每个程序最多 8 秒，结果缓存 1 分钟，手动刷新间隔至少 5 秒。最新版本排除预发布，Node.js 默认按最高正式版本查询，可能高于 LTS 分支；Node.js 和 Python 3 可在右上角版本检查对话框设置大版本／中版本范围；Python 使用 python.org 发布列表与 CPython 对应标签的 NEWS，Go 使用 go.dev 下载接口及发布历史，Node.js 使用官方版本索引与对应标签 Changelog，Codex CLI、Composer 和 phpMyAdmin 分别使用各自官方仓库的正式 GitHub Release。联网查询与服务共用缓存、限流和失败处理。

已有安装需要同步更新面板、辅助程序和 sudoers，新增精确授权 `lnmp-gopanel-control components snapshot`；安装脚本会验证该权限，并以面板账号执行只读组件自检；工具未安装不阻止安装，辅助程序执行失败则回滚升级。无需修改 `services.yaml` 或放宽 `/root` 目录权限。

Composer 检测沿用 `components snapshot` 授权，升级时使用同一源码打包的面板与辅助程序即可，无需新增 sudoers 规则。安装脚本不会安装 Composer 或 PHP；未安装 Composer 显示“未安装”，已安装 Composer 但缺少 PHP CLI 显示读取失败，不影响面板安装和其他组件检测。

</details>

## 在线安装 LNMP

无 LNMP 的服务器可在“LNMP 服务”或系统概览中选择“安装 LNMP”，准备源码后配置数据库、PHP、内存分配器、默认站点保护和 Nginx 模块。页面提供安装进度、耗时、实时输出及历史日志下载。

安装在独立 systemd 服务中执行，关闭页面或重启面板后继续；服务器重启后，未完成任务标记为中断，不自动恢复编译。已有 LNMP、冲突组件、目录残留或端口冲突时会阻止安装。

<details>
<summary>安装来源、版本限制、模块依赖与任务持久化</summary>

在“LNMP 服务”或系统概览中点击“安装 LNMP”，进入 `/lnmp/install`。先从 [azpanel/lnmp2.3-offline](https://github.com/azpanel/lnmp2.3-offline) 的 `main` 分支获取最新源码到 `/root/lnmp2.3-offline`，再按实际脚本选择数据库、PHP、内存分配器与默认站点保护方式。安装页标题为“安装配置”，仅提供高于 PHP 8.1.34 的稳定版本（不含 8.1.34）；数据库密码支持安全随机生成、显示和复制。准备完成后固定该提交安装；上游选项结构不兼容时会停止并提示。

Nginx 默认勾选 VTS、GeoIP2、FancyIndex 和 Substitutions Filter，支持逐项取消。所选模块从各自仓库的默认分支拉取并记录提交，使用 `--add-module` 静态编译，FancyIndex 不会重复添加。选择 GeoIP2 时，apt 系统自动安装 `libmaxminddb0 libmaxminddb-dev mmdb-bin`；dnf/yum 系统安装 `libmaxminddb libmaxminddb-devel`。Rocky、AlmaLinux、CentOS 8 在本次命令中启用官方 PowerTools 源，9 及以后启用 CRB 源，不永久更改源配置。软件源缺包或模块编译失败会明确终止，不会跳过。MMDB 数据库、统计访问入口和模块业务配置需自行配置。

安装通过独立 systemd 服务运行，关闭页面或重启面板后继续执行。页面展示阶段、安装耗时及编译输出，支持断线续传、轮询、历史日志和下载。安装耗时从后端接受“开始安装 LNMP”请求时起算，排除源码准备和填写配置时间，刷新页面后继续，安装结束后固定；尚未开始时显示提示，缺少起点的旧记录显示“安装耗时未知”。实时与历史日志自动折行，长路径及编译参数无需横向滚动，下载保留原始格式。任务状态与脱敏日志保存在 root 专属的 `/var/lib/lnmp-gopanel-install`，原始安装日志为 `/root/lnmp-install.log`。服务器重启后未完成的任务标记为中断，不自动恢复编译。成功后自动更新面板服务列表。

在线安装仅用于无 LNMP 和冲突组件的服务器。已有组件、目录残留或端口冲突时会阻止安装；失败保留源码和日志，由管理员处理后重试，不自动删除数据库或网站。面板卸载保留上述安装记录、源码与 LNMP 环境。升级时请同时更新辅助程序及 sudoers：新增的 `lnmp-install inspect/prepare/start/read` 仅在面板安装自检中检查授权，不触发环境安装。

多个客户端共享安装状态和相同游标的日志查询，结果缓存 1 秒；缓存最多 32 条、按 JSON 编码体积合计不超过 8 MiB，辅助程序查询最多并发 4 个。开始准备或安装后使旧缓存失效，任务结束前重新读取日志尾部，保留原有断线续传和下载协议。

</details>

## 网站管理

“虚拟主机”支持卡片和表格视图，可查看域名、监听地址、代理目标、目录占用、SSL 证书和配置原文。支持站点启停、配置格式化、Basic Auth，以及按需生成的网站截图。

“添加站点”支持 PHP 或静态站点、附加域名、伪静态、IPv6、访问日志，可选创建数据库和本机用户。SSL 可申请 Let's Encrypt / ZeroSSL、引用已有证书或选择已发现的通配符证书；表格中可多选站点提前续签。自动申请与续签使用 HTTP-01，需要全部证书域名能通过公网 80 端口访问。

建站和续签进度可在关闭窗口后重新打开；面板重启后不自动恢复任务队列，请刷新站点核对实际状态。网站截图需要额外安装 Chromium / Chrome，支持分辨率、缩放、语言、加载时间、Basic Auth 和截图前 JavaScript 设置。

<details>
<summary>配置解析、目录占用、站点启停与相关接口</summary>

“虚拟主机”页面按 `/usr/local/nginx/conf/vhost` 中的配置文件展示站点：域名、监听地址、反向代理目标、目录占用、SSL 证书签发者、主体、域名、有效期、剩余天数、序列号及 SHA-256 指纹。可查看原始配置，并手动格式化缩进后保存。`.conf` 表示启用，`.conf.disabled` 表示停用；同文件的多个 server 块作为一项配置一起管理。

侧栏“虚拟主机”显示配置总数，包含已启用及已停用的站点，和“LNMP 服务”使用相同数量角标。登录后通过需鉴权的 `GET /api/v1/vhosts/count` 查询，每 30 秒刷新，读取失败时显示“—”或上次成功结果，不误报为零。统计只读取站点元数据、不触发目录占用扫描，多浏览器共享 30 秒缓存；站点列表刷新成功后立即同步角标，搜索筛选不改变总数。

配置解析支持注释、引号、多行指令及静态 include（相对于 Nginx conf 目录，设有深度与读取上限）。动态 root 或证书路径显示无法静态计算，不执行变量或脚本。SSL 信息来自已配置的本地 PEM 叶证书，时间状态不代表证书链信任、域名匹配或在线吊销校验；证书信息解析不读取 `ssl_certificate_key` 指向的私钥文件。无显式 root 的反向代理显示无本地目录。

目录占用在站点元数据加载后异步计算，使用 `du -s -x -B1` 的实际磁盘块大小。root 路径最后一级为 `public` 时统计其上一级目录；相同目录去重计算，符号链接部署目录先解析实际目标。单目录最多 45 秒，总任务最多 5 分钟，结果只保留内存。站点配置和证书可通过“刷新站点”重新读取。

启停时只对普通配置文件原地重命名，保留原文件内容与权限；拒绝目录穿越、符号链接、同名文件冲突及覆盖目标。重命名后执行 `nginx -t`，通过才发出 `nginx -s reload`。检查失败恢复原文件名；重载失败恢复原文件名并尝试重新加载原配置，失败结果明确提示。所有面板触发的 Nginx 修改与站点启停共享进程间锁。

相关接口：`GET /api/v1/vhosts`、`POST /api/v1/vhosts/refresh`（202，异步刷新）、`GET /api/v1/vhosts/config?name=example.conf`、`POST /api/v1/vhosts/toggle`（JSON `{"name":"example.conf","enabled":false}`）。全部需要登录，POST 需要 CSRF。特权操作仅开放固定参数 `vhosts list/config/format/usage/toggle`，文件名通过有界 JSON stdin 传递；辅助程序重新验证文件名与目录权限。升级时同步安装两个二进制和 sudoers 模板。

</details>

<details>
<summary>添加站点、DNS 检查、数据库与证书申请</summary>

“添加站点”支持主域名与附加域名、站点目录、PHP／纯静态、Pathinfo、现有伪静态模板、IPv6 和访问日志，可选创建数据库与同名本机用户，不创建 FTP 账号。默认目录是 `/home/wwwroot/<主域名>`，自定义目录只允许使用 `/home/wwwroot/` 下的规范子目录，不能直接使用 `/home/wwwroot` 或通过符号链接绕过范围限制；该根目录须已存在、由 root 管理且不可被其他用户写入。已有目录不递归修改权限，也不覆盖已有 `.user.ini`。新站点访问日志保存在 `/home/wwwlogs/`，目录与文件沿用 root 所有权、禁止其他用户写入及禁止符号链接的检查，可在日志页面查看；原有日志路径不变。数据库密码可点击“随机生成强密码”生成 24 位、包含大小写字母、数字及符号的密码，并可显示或隐藏；密码仅保留在当前表单，不存入浏览器存储。数据库优先使用 root 配置或 Unix socket 认证，不可用时填写管理员密码；凭据仅用于本次操作。已有数据库、账号、域名绑定和同名配置不会被覆盖。

HTTP 站点创建成功前失败，会收回本次新建目录授予 `www` 的权限，清理未被修改的新增 `.user.ini` 并逐级删除新增空目录；已有目录和用户新增内容不会被删除。遇到路径替换、可被普通用户修改的父目录或其他清理失败，会保留相关资源并报告遗留路径。若 Nginx 回滚结果无法确认，则保留站点目录和建站记录供排查，避免删除仍可能被运行中配置引用的资源。HTTP 创建成功后数据库或 SSL 失败，仍保留 HTTP 站点；既有站点不因新建路径限制而迁移。

SSL 支持申请 Let's Encrypt／ZeroSSL、填写 cert/key 绝对路径，或从 `/root/.acme.sh/`、`/usr/local/acme.sh/` 扫描到的通配符证书中选择。证书会检查有效期、私钥匹配和全部站点域名的覆盖范围；通配符只匹配一级子域名，根域名需另含 SAN。已有证书直接引用原文件，沿用原续期与 Nginx 加载设置，本功能不额外接管。启用 SSL 后默认强制 HTTPS，可关闭；HTTP 证书验证路径始终保留。

Go 辅助程序生成配置并检查、重载 Nginx，配置或重载失败会恢复原配置，不调用版本相关的 `lnmp vhost add`／`lnmp ssl`。自动申请由 Go 依次检测 `/root/.acme.sh/acme.sh`、`/usr/local/acme.sh/acme.sh`，使用首个可执行且由 root 所有、程序及父目录均不可被组或其他用户写入的安装；两个路径同时有效时优先前者。要求安装可信的 `/usr/bin/flock` 和运行中的系统 cron；使用 webroot HTTP-01 验证，所有域名需能从公网 80 端口访问本站。申请不支持通配符或 DNS API。新证书使用所选安装目录下独立的 `gopanel-certs/`，申请、部署和每日续期使用同一安装目录与证书目录，不改写已有证书配置。证书通过 `--install-cert` 部署到 `/usr/local/nginx/conf/ssl/gopanel/`，每日续期任务保存在 `/etc/cron.d/lnmp-gopanel-acme`，续期重载使用与面板共享的 Nginx 锁并先检查配置。续期独立于面板运行，卸载面板仍保留网站证书和续期任务。不会扫描备份目录来执行 acme.sh；缺少客户端或权限不安全与缺少 flock 会分别提示。

“校验解析”与主域名、附加域名输入框同排，逐域名查询 A／AAAA，与本机网卡和公网 IPv4／IPv6 对比。公网探测通过 HTTPS 访问 `api.ipify.org` 和 `api6.ipify.org`，结果缓存五分钟；失败时显示“无法确认”。公网探测警告及解析校验说明收在问号图标的提示中，支持悬停、键盘聚焦和点击。NAT 环境可在配置中添加 `vhosts.public_ips: ["服务器公网 IPv4", "服务器公网 IPv6"]` 补充参考地址。CDN、代理、混合解析或暂未生效只作提示，不阻止建站与证书申请。

创建后可实时查看证书申请输出。关闭窗口不取消任务，刷新页面可打开“查看建站进度”或“最近建站结果”；任务最长 20 分钟，在内存中保留最近 20 个任务，每个任务最多 1000 行日志，敏感输出会隐藏。面板重启不自动重放任务，请刷新站点确认实际状态。数据库创建失败只清理本次新增数据库资源；SSL 失败保留 HTTP 站点，可单独重试 SSL，不重复创建网站或数据库。重试只操作本功能创建且配置未被外部修改的启用站点。

相关认证接口：`GET /api/v1/vhosts/options`、`POST /api/v1/vhosts/check-dns`、`POST /api/v1/vhosts`、`GET /api/v1/vhosts/jobs`、`GET /api/v1/vhosts/jobs/:id`、`GET /api/v1/vhosts/jobs/:id/events` 和 `POST /api/v1/vhosts/ssl/retry`。POST 需要 CSRF，建站相关 JSON 请求上限 32 KiB；创建和重试返回 202 与任务 ID，重复提交相同 ID 不重复执行。升级需同步安装两个二进制与 sudoers，新增固定 `vhosts options` 和 `vhosts create` 授权；安装只检查授权，不创建站点或申请证书。

</details>

<details>
<summary>批量提前续签、回滚与自动续期</summary>

表格视图支持逐行多选、全选当前筛选结果和清空选择。点击“提前续签 SSL”，为每个站点分别选择 ZeroSSL 或 Let's Encrypt 并填写联系邮箱；机构默认匹配当前证书，无法识别时使用 Let's Encrypt，邮箱优先读取已有面板记录。确认列表显示原签发者、到期时间、本次签发域名和无法续签的原因。

支持原有 LNMP 站点及面板创建的已启用 SSL 站点，不要求存在建站记录或配置保持原样。当前使用 HTTP-01 验证，要求全部证书域名绑定在该配置且可从公网 80 端口访问。通配符、多组证书、动态证书引用、关键配置位于共享 include、无法安全调整的服务器级重写，以及不可读取的证书会显示原因并禁止提交；停用站点需先启用。只修改验证入口和证书引用，保留 PHP、代理、重写及访问认证。

队列在后端逐站执行，每站最长 20 分钟，单站失败后继续。强制重新签发，不以旧证书尚未到期作为成功。新证书使用独立版本目录暂存，检查私钥、有效期和完整域名覆盖后切换配置；Nginx 检查、重载或续期登记失败时恢复原配置，遇到外部并发修改则停止覆盖并在输出中说明。原证书文件保留。成功后每站使用独立 `/etc/cron.d/lnmp-gopanel-acme-<站点哈希>` 任务续期，与原有证书续期目录隔离；续期不依赖面板运行。

任务窗口实时显示每站状态、阶段、完成数量及带站点名称的签发输出。SSE 断线时每两秒轮询，关闭窗口或刷新页面后可通过“查看续签进度”重新打开；完成后可只重试失败项并保留其机构和邮箱。与建站任务共用执行互斥、最近 20 个任务及最近 1000 行脱敏日志的内存记录，面板进程重启后不自动恢复队列，应刷新站点确认实际状态。

面板停止导致任务 context 取消时，等待当前站点执行和清理返回后停止调度，未启动站点标记为“已取消”。任务增加 `canceled` 数量，未启动项目不计入 `completed` 或 `failed`；已执行项目仍保留实际结果或状态核对提示。不增加整批总时限。建站和续签 SSE 按任务变更读取增量日志，空闲时不复制完整任务；日志游标超出保留窗口时返回快照。

相关接口：`POST /api/v1/vhosts/ssl/renew/preview` 接收 `{"names":["example.conf"]}`，返回每站可续签状态与默认参数；`POST /api/v1/vhosts/ssl/renew` 接收 `{"request_id":"唯一请求标识","items":[{"name":"example.conf","provider":"zerossl","email":"admin@example.com"}]}`，返回 202 与批量任务。两者均需登录及 CSRF，请求体上限 1 MiB，最多 1000 个不重复站点；辅助程序仅逐站执行。进度复用现有任务查询和 SSE 接口，批量任务的 `kind` 为 `ssl-renew`，增加 `items`、`completed`、`succeeded`、`failed`，日志增加 `name`。升级需同步安装两个二进制和 sudoers，新增 `vhosts ssl-renew-preview`、`vhosts ssl-renew` 精确授权；安装只检查授权，不实际续签。

</details>

<details>
<summary>站点 Basic Auth、凭证存储与权限边界</summary>

虚拟主机卡片和表格的“访问认证”可开启、读取、关闭 Basic Auth 或替换用户名及密码。每个 vhost 一组凭证，同文件全部 server 共用；证书验证路径 `/.well-known/acme-challenge/` 免认证，SSL 重试会保留认证。停用站点只显示保存的配置状态，启用后才能修改。已有手写认证、共享 include 中的认证或 `satisfy any` 显示“外部配置”，不覆盖；动态 include、Lua 执行指令、提前返回成功响应的 `return 2xx`、异常管理标记、凭证缺失或无法确认继承关系时显示“无法确认”，禁止修改。状态来自磁盘配置检查，不代表已对公网地址执行认证探测。

认证接口：`GET /api/v1/vhosts/basic-auth?name=example.conf` 读取状态；`PUT /api/v1/vhosts/basic-auth` 使用 JSON `{"name":"example.conf","username":"visitor","password":"新密码"}` 开启或完整替换凭证；`DELETE /api/v1/vhosts/basic-auth?name=example.conf` 关闭，重复关闭成功。均需登录，写操作需要 CSRF，JSON 上限 4 KiB。状态返回 `name`、`status`（`disabled/enabled/external/unknown`）、`managed`、`site_enabled`，以及可选 `username` 和 `message`，不返回密码及哈希。用户名为 1–64 位字母、数字、点、下划线或连字符；密码为 8–256 字节，不含控制字符。

辅助程序只接受固定 `vhosts basic-auth-get/set/disable`，凭证通过 JSON stdin 传递。需要可信 `/usr/bin/openssl` 生成带随机盐的 SHA-512 crypt 哈希；只将用户名和哈希保存在 `/usr/local/nginx/conf/gopanel-auth/`，目录 `0755`、文件 `0644`，均由 root 管理且不可被其他用户写入，供 Nginx worker 读取。每次更新使用独立凭证文件，原凭证文件保留供平滑退出的旧 worker 完成请求，不复用或改写。配置检查或重载失败会恢复原配置，确认回滚成功后未被引用的新凭证会清理；关闭认证精确移除面板插入的配置片段，保留原配置内容和权限。卸载面板保留网站认证及凭证文件。升级需同步安装面板、辅助程序和 sudoers；安装仅检查新增命令的精确授权，不读取或修改认证。

</details>

<details>
<summary>网站截图设置、浏览器运行与备份</summary>

虚拟主机支持卡片和表格视图，两种视图均展示网站预览。通过“截图设置”填写 HTTP/HTTPS 地址，可启用 Basic Auth，并按站点指定浏览器语言（默认 `zh-CN`，例如 `en-US`）和最长加载时间（默认 30 秒，可设为 5–300 秒）。语言同时用于请求语言、浏览器语言及日期、数字格式。截图分辨率可选 1920×1080、1440×900（默认）、1366×768、768×1024、390×844，并提供 1920×1200、2560×1600、2880×1800、3072×1920、3200×2000、3840×2400 六个 16:10 高分辨率预设；也可自定义宽高，均须为 320–3840 的整数。网页缩放默认 100%，提供常用比例，也可自定义 25%–200% 的整数百分比。缩小比例可显示更多内容，响应式布局随可见范围变化，输出图片尺寸始终保持所选分辨率。手机和平板尺寸不切换移动端 UA 或启用触摸模拟。保存后后台截取网站首屏，之后手动“重新截图”，点击图片可放大；没有定时截图、全页截图或历史图库。支持公网、本机和内网地址，浏览器访问的是服务器可达的地址，不使用管理员浏览器中的登录状态。页面加载后等待资源请求结束并连续稳定 2 秒、字体就绪后再截图；CSS、图片等加载较慢时可增加最长加载时间，超时显示原因并保留上次成功截图。持续轮询的页面可能需要更长时间才能稳定。HTTPS 证书错误、HTTP 错误和认证失败会显示原因，保留上次成功截图。

可选填写“截图前 JavaScript”（最多 16 KiB，按 UTF-8 字节计算），在目标网站主框架中执行，用于关闭弹窗、点击元素或调整页面。代码在页面正文、资源和字体就绪后执行一次，支持直接使用 `await`，执行结果不回传；完成后再次等待资源稳定、字体及渲染就绪再截图。执行也计入该站点的最长加载时间，脚本语法错误、执行异常、Promise 拒绝或超时会显示原因，并保留上次成功截图。分辨率、缩放比例和脚本随站点保存，重启后保留，重新截图时复用；脚本留空不执行，旧设置默认使用 1440×900 和 100% 缩放。例如：

```javascript
document.querySelector(".popup")?.remove();
document.querySelector(".expand-button")?.click();
await new Promise((resolve) => setTimeout(resolve, 500));
```

截图由 Go 直接控制本机 Chromium，无需生产环境安装 Node.js，不通过 sudo 辅助程序启动浏览器。浏览器必须以普通面板账号运行，保留沙箱，每次任务使用独立临时配置，完成或取消后清理。全局一次执行一个任务、最多排队 64 项，单次任务（含启动浏览器和资源等待）受该站点的最长加载时间限制，排队时间不计入。Basic Auth 只发送给配置地址同源的认证挑战，跨源跳转不会携带凭据；如需在跳转后的站点认证，请直接填写最终地址。密码不回传，编辑时留空保留原密码，更换目标来源或用户名时需重新填写；关闭认证会清除凭据，“清除设置”会同时删除保存的图片。

可选配置：

```yaml
screenshots:
  browser: /usr/bin/chromium # 留空时在 PATH 中检测 Chromium/Chrome
  state_dir: /var/lib/lnmp-gopanel
```

标准部署的状态目录为 `/var/lib/lnmp-gopanel`，由安装脚本和 systemd 为 `lnmp-panel` 创建；自定义配置文件默认在其同级 `screenshots-state/` 保存状态，也可以显式指定目录。目录必须由面板账号所有、权限 `0700`，状态文件和图片权限为 `0600`。密码使用 AES-GCM 加密保存，`screenshots.key` 与 `screenshot-settings.json` 必须一起备份、恢复，密钥丢失不会自动覆盖重建。设置与最后成功截图在升级及重启后保留；它们不会写入 Nginx 配置或打包进 Release。缺失浏览器或状态目录不可用时，其他功能继续工作。

截图设置文件 `screenshot-settings.json` 按实际 JSON 编码后的大小统一限制为 16 MiB（包含 JSON 转义），图片独立保存，不计入该上限。超限保存返回 HTTP 400 及容量提示，保留原设置、队列和正在执行的任务；后台截图完成时若设置超限，清理新图片并保留上次成功截图。JPEG 质量参数维持 80。旧版本已产生的超限设置文件会在启动时明确报错，不会自动截断、清空或更换密钥。恢复时先停止面板服务，备份原设置文件和密钥，使用保留 UTF-8 和 JSON 格式的编辑工具删除不再需要的站点记录或缩短 `javascript` 字段，使文件不超过 16 MiB；保留其余记录和加密凭据，维持文件所有者及 `0600` 权限后再启动服务。自定义部署使用实际配置的状态目录。

部署后可用 `sudo -u lnmp-panel /usr/local/bin/lnmp-gopanel check-screenshots` 检查截图组件，仅访问临时本地测试页面，不访问业务站点。源码集成测试使用 `SCREENSHOT_TEST_BROWSER=/path/to/chromium go test -race ./internal/screenshots -run TestChromiumIntegration`，所有目标都是本地测试服务器；仅隔离的 root 测试进程会关闭浏览器沙箱，生产代码不提供该开关。

相关接口：`GET /api/v1/vhosts/screenshots`、`PUT /api/v1/vhosts/screenshot?name=…`、`POST /api/v1/vhosts/screenshot/refresh?name=…`、`DELETE /api/v1/vhosts/screenshot?name=…`、`GET /api/v1/vhosts/screenshot/image?name=…`。均需登录，写操作需 CSRF；设置与刷新返回 202，图片通过面板鉴权后读取且禁止缓存。设置输入及返回数据包含 `viewport_width`、`viewport_height`、`zoom_percent`、`javascript`；宽高同时省略或均为 0 时使用默认尺寸，只提供一项非零尺寸会报错。`zoom_percent` 为 25–200 的整数，省略时默认 100，显式传入 0 或超出范围会报错。截图设置 PUT 请求体上限为 128 KiB，以容纳脚本和 JSON 转义，建站相关 POST 为 32 KiB，批量 SSL 续签及预检查 POST 为 1 MiB，其他 API 为 4 KiB。

</details>

<details>
<summary>站点配置格式化与冲突检查</summary>

在虚拟主机的“查看配置”弹窗中，点击“格式化缩进并保存”可按 4 空格整理当前站点配置；停用站点保持停用。该操作保留配置指令、字符串、注释和面板管理的访问认证，不修改共享 include 文件，也不重载 Nginx。配置文件已被其他操作修改时需点击“重新读取”后再执行；不支持安全解析的配置会保留原文件并显示原因。写入使用原子替换并保留权限，Nginx 检查失败时恢复原文件。

新增 `POST /api/v1/vhosts/config/format`，请求为 `{ "name": "example.com.conf", "revision": "读取配置返回的版本" }`，需要登录和 CSRF。`GET /api/v1/vhosts/config` 额外返回 `revision`；格式化接口返回新的配置内容、版本、`changed` 和提示信息。升级须同步安装面板、辅助程序和 sudoers，安装脚本会检查新增的 `vhosts format` 精确授权，不会在安装时执行格式化。

</details>

## 日志与定时任务

“日志读取”支持内置日志与自定义 `.log` / `.txt` 文件，按需加载、追踪新增内容、暂停和筛选。Nginx 访问日志与登录日志可格式化为表格并展示离线 IP 属地，Nginx 表格还支持 HTTP 状态码筛选。筛选针对已加载范围，不搜索整个文件；页面不提供日志编辑、清空或下载。

“定时任务”只读展示 root crontab，可展开原文、手动刷新和预览之后 5 次预计执行时间。此处不包含其他用户、系统 Cron 目录或 systemd timer，也不提供任务编辑或执行。

<details>
<summary>日志来源、自定义路径、登录审计与读取协议</summary>

左侧“日志读取”页面自动列出 `/home/wwwlogs`、`/usr/local/nginx/logs/gopanel` 直属目录中的 `.log` 普通文件，以及 `/var/log/mysql/mariadb-slow.log`、`/usr/local/php/var/log/php_errors.log`。在“日志内容”卡片顶部选择日志，卡片显示路径、大小和修改时间；不递归发现子目录，不自动列出轮转归档，不跟随符号链接，不提供编辑、清空和下载功能。文件不存在不影响安装。

文件列表还会自动列出“登录日志”，实际文件为 `logs.state_dir/login.log`（标准安装为 `/var/lib/lnmp-gopanel/login.log`），不可从列表移除。每行是一个 JSON 对象，包含带时区的时间 `time`、客户端 IP `ip`、User-Agent `ua`、用户名 `username`、结果 `result`（`success` / `failure`）、阶段 `stage`（`password` / `totp`）及原因 `reason`。成功与失败均记录；仅在密码校验失败时额外保存原始输入的 `password`（包括空密码），用户名错误但密码正确、二步验证失败和成功登录均不保存密码，验证码、恢复码及挑战令牌也不会写入。启用二步验证后，完成验证并创建会话才记录成功，进入待验证状态不记为成功。二步验证事件使用当前唯一管理员用户名；参数无效、限流及认证存储错误仅记录可获得的元信息，无法取得的用户名为空，请求来源校验拒绝不写入此文件。IP 沿用直接连接地址，不信任未经配置的代理请求头。

登录日志由面板账号直接读写，目录权限为 `0700`、文件为 `0600`，拒绝符号链接、硬链接和特殊文件。页面复用原文、筛选、历史加载和实时追踪；文件元数据可包含 `name` 显示名称。仅这一指定文件由专用入口读取，其他状态文件仍禁止读取，不增加 sudo 授权。系统日志辅助程序不可用时仍可读取登录日志，并显示其他日志的读取错误。初始化或写入失败会提示错误，不改变认证结果；重启保留记录，本功能不自动轮转日志。

登录日志默认显示 JSON 原文，可勾选“格式化为表格”，依次展示字节偏移、时间、客户端 IP、IP 属地、用户名、结果、验证阶段、原因、失败密码及 User-Agent，并显示可识别的 UA 图标。时间按浏览器本地时区显示，悬停可查看原始带时区时间；已知结果、阶段及原因使用中文，未知值保留原文。没有记录密码时显示“—”，记录了空密码时显示“（空密码）”。无法解析的行保留原文及说明，截断、未换行提示仍可见。表格支持容器内横向滚动，沿用历史加载、实时追踪和关键词／正则筛选，切换文件后恢复原文模式；HTTP 状态码筛选只用于 Nginx 日志。

登录日志文件元数据包含 `format: "login"`，不依赖文件路径或显示名称识别。读取时从 JSON 的 `ip` 字段提取地址，使用下述纯真数据库离线补充 `ip_location`，同批重复 IP 复用查询结果。IPv6 显示“暂不支持”，无效地址及查询失败显示“未知”；原文件不修改，历史记录同样可展示属地。

点击“添加日志”，输入服务器上已有普通文本日志的绝对路径，例如 `/opt/app/logs/server.log`、`/opt/app/logs/output.txt`；最终扩展名仅允许 `.log` 或 `.txt`，不区分大小写。无扩展名、`.php`、`.log.1`、`.txt.bak` 等文件禁止添加及读取；API 与辅助程序均校验，不能绕过页面限制直接读取。添加前由辅助程序检查文件，成功后自动选中读取。最多保存 64 个自定义路径，刷新、重新登录和面板重启后保留；文件暂时消失或不可读时保留条目并显示错误。旧版本已登记但扩展名不合规的条目仍会显示在列表中并提示不可读取，可通过“移除日志”清理，不影响其他合规条目及内置日志。“移除日志”只移除自定义列表条目，不删除或修改原文件，内置日志不能移除。升级时同步更新面板及日志辅助程序。

系统配置（`/etc`）、`/root`、`/proc`、`/sys`、`/dev`、`/run`、`/boot`、备份和面板状态目录、常见 MySQL／MariaDB／PostgreSQL／Redis 数据目录，以及 Nginx／PHP 等组件的配置目录禁止读取。任意路径中的 `.ssh`、`.aws`、`.kube`、`.docker`、`.gnupg`、`.git` 等凭据或版本库目录，`.env*`、常见密码配置、私钥证书和数据库文件也会拒绝。路径必须规范，不能包含 `..`、重复分隔符、控制字符或反斜杠。添加及每次读取都重新校验，逐段禁止符号链接，并拒绝硬链接、特殊文件、包含 NUL 或私钥头的文件；格式检查仅检查前 4 KiB，不对应用日志中可能记录的敏感业务内容进行脱敏。

自定义路径存于 `logs.state_dir` 下的 `custom-logs.json`，标准安装默认为 `/var/lib/lnmp-gopanel`；自定义配置文件默认使用同级 `logs-state/`。目录必须属于面板账号且权限为 `0700`，列表文件为 `0600`，采用原子替换保存；存储不可用时内置日志继续可用并显示提示。备份该文件即可保留自定义列表，升级安装不会覆盖它。

默认定位到文件末尾，约每秒增量读取，可暂停追踪、自动滚动、回到最新或加载更早日志。首次打开、切换文件和“回到最新”读取末尾 64 KiB，历史与增量读取单批原始范围仍最多 256 KiB；行边界由有界回溯补全，每批最多返回 4000 行。向上滚动会关闭自动滚动；加载历史会暂停追踪。后台标签页暂停轮询，返回后从游标继续读取。页面缓存最多 2 MiB、20000 行和 64 批，超限会释放浏览方向另一端，可重新加载。日志使用虚拟列表显示，避免整文件读取和大量 DOM。文件轮转或截断后提示并重新读取最新文件，不混合不同版本的历史。

日志读取接口对支持 gzip 的客户端压缩至少 1 KiB 且压缩后更小的成功 JSON 响应，带 `Vary: Accept-Encoding`；不支持或显式禁用 gzip 时返回普通 JSON。压缩仅用于日志读取，不影响实时指标 SSE。缩小首批窗口需要面板与辅助程序同步升级。

排查读取等待时，在浏览器 Network 中检查 `/api/v1/logs/read` 的 `Server-Timing`：`read` 包含日志读取（系统日志还包含 sudo、辅助进程启动及返回解码），`geo` 为属地补充，`encode` 为 JSON 编码，`compress` 为压缩，`total` 为响应开始发送前的处理时间，单位均为毫秒。`X-Log-Request-ID` 用于关联服务器记录。`journalctl -u lnmp-gopanel` 会记录每次 `tail`、至少 500 ms 的慢请求及读取错误，包含各阶段耗时、服务器写入时间、行数和压缩前后字节数；普通快速增量请求仅输出到 debug 级别。服务器写入时间不代表客户端已收完整个响应。

浏览器在新日志提交并经过绘制周期后记录 `log-read` 性能条目，可在控制台执行 `performance.getEntriesByName("log-read", "measure").map(entry => entry.detail)` 查看请求等待、下载、JSON 解码及显示耗时、传输与解压后字节数、请求 ID 和服务端耗时。性能条目最多 64 条，满后重置；首次读取和至少 500 ms 的显示等待也输出到 debug 控制台。耗时诊断不记录日志正文、筛选规则、游标或凭据，不上传浏览器测量数据。

点击“日志内容”旁的“筛选规则”，在右侧抽屉中编辑并应用规则；关闭抽屉保留未应用的编辑，应用成功后自动收起。可添加最多 20 条关键词或 Go 正则规则，逐行选择“显示匹配”或“隐藏匹配”，支持启停及忽略大小写，默认区分大小写。存在显示规则时命中任意一条即可，隐藏规则优先；没有显示规则时默认显示全部。点击“应用规则”后，仅重新筛选已加载范围，不搜索整个文件；慢 SQL 和错误堆栈也逐行处理。规则按文件保存在当前浏览器，每条内容最多 256 字节、合计最多 2048 字节，请求 JSON 总长不超过 4 KiB。Go 正则不支持前后查找、回溯引用，无效规则会提示并保留当前有效筛选。超长行只显示和匹配前 64 KiB；无法在有界回溯范围内找到行首时跳过该行并提示。日志按 UTF-8 展示，非法字符显示为替代字符。

读取 Nginx 日志目录（`/home/wwwlogs`、`/usr/local/nginx/logs/gopanel`）中的日志时，可勾选“自动滚动”右侧的“格式化为表格”，依次显示字节偏移、客户端 IP、IP 属地、时间、请求、HTTP 状态码、响应字节数、Referer 和 User-Agent。属地展示数据库返回的地区及运营商信息；旁边的问号图标支持悬停、聚焦或点击查看数据来源、版本与 IPv6 支持说明。表格不提供原文列或原文弹窗。支持 common、combined 及相同字段前缀的常见文本格式；无法解析的行保留文本，自定义 JSON 或不同字段顺序不解析。表格支持在容器内横向滚动，沿用虚拟列表、历史加载和实时追踪。

IP 地址位置数据由[纯真 CZ88](https://www.cz88.net/) 提供支持，使用 [metowolf/qqwry.dat 的 20261021 版本](https://github.com/metowolf/qqwry.dat/releases/tag/20261021)，以 [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) 署名并分发，详见[纯真社区版数据说明](https://www.cz88.net/geo-public)。数据库 SHA-256 为 `15197d3a13d767a3113ca92bde9aa2fa02a99b0620eee07ebbd630262a4f57bf`，内嵌在面板二进制中，随安装包交付并随面板版本更新；运行时离线查询，无需另行安装数据库。DAT 格式只支持 IPv4，IPv6 显示“暂不支持”，未命中或查询失败显示“未知”。初始化失败会记录日志，日志读取继续运行。

表格模式下可多选具体 HTTP 状态码及 1xx～5xx 类别，多个选择取并集，未选择或点击“清除筛选”时显示全部。状态码筛选仅作用于现有关键词、正则规则返回的已加载范围，不扫描整个文件；加载历史和收到新增日志后继续筛选。启用状态码筛选时不展示无法解析的行。取消表格模式会清空状态码选择；切换文件时恢复原文模式并清空选择，表格及状态码设置不保存到浏览器。

需登录接口 `GET /api/v1/logs` 和 `POST /api/v1/logs/read`，POST 需要 CSRF。读取 JSON 包含 `path`、`mode`（`tail`、`before`、`after`、`range`）、不透明的 `start` / `end` 游标和 `rules`；每条规则包含 `kind`（`keyword` / `regex`）、`action`（`show` / `hide`）、`pattern`、`ignore_case`、`enabled`。响应提供匹配行及字节偏移、源文件范围、前后游标和文件状态，Nginx 和登录日志行可包含 `ip_location` 属地显示文本；409 表示文件已变化。属地由普通面板进程补充，不增加辅助程序权限。游标始终按原始范围推进，筛选全部隐藏也不会反复读取同一批。规则在 Go 后端执行，读取有超时和全局并发限制。

升级时同步安装两个二进制和 sudoers 模板，新增固定授权 `/usr/local/libexec/lnmp-gopanel-control logs list`、`/usr/local/libexec/lnmp-gopanel-control logs read`，验证 sudoers 并重启面板。文件、游标和规则通过有界 JSON stdin 传递，辅助程序重新校验敏感路径策略、文件类型和路径，禁止跟随任意路径组件中的符号链接。安装脚本仅检查这两项的 sudo 授权，不实际读取或输出日志内容。

自定义日志沿用上述两项 sudo 授权，需同步升级面板和辅助程序。新增 `POST /api/v1/logs/custom`、`DELETE /api/v1/logs/custom`，请求均为 `{"path":"绝对路径"}`，需要登录、CSRF，JSON 不超过 4 KiB。添加返回文件元数据，移除返回 204；列表中自定义条目带 `custom: true`。现有读取接口仅允许内置路径或已添加的自定义路径，不能通过直接请求绕过添加。辅助程序的读取请求新增 `stat` 模式用于有界格式检查与文件元数据查询，沿用读取超时和并发限制，不提供任意命令或文件修改权限。

</details>

<details>
<summary>Cron 解析、时区与执行时间预览</summary>

左侧“定时任务”页面只读展示 root 用户的 crontab，包含执行周期、执行命令、原始行号、任务数量和最近读取时间，可手动刷新并展开查看原文。支持五字段 Cron 表达式及 `@reboot` 等特殊周期；环境变量、注释和空行不计为任务，未识别内容会显示提示并保留原文。此页面不包含其他用户、`/etc/crontab`、系统 Cron 目录或 systemd timer，也不提供编辑或立即执行功能。

接口 `GET /api/v1/cron` 需要登录，返回 `tasks`、`raw`、`warnings` 和 `updated_at`。读取失败会明确提示，刷新失败时保留上次成功的数据并标记可能过期；没有 root crontab 时显示空列表。辅助程序仅接受固定的 `cron list`，在可信路径下执行 `crontab -u root -l`，有超时及输出大小限制，不接受用户输入的参数。

升级时同步更新面板、辅助程序和 `deploy/lnmp-gopanel.sudoers`，新增精确授权 `/usr/local/libexec/lnmp-gopanel-control cron list`，先通过 `visudo -cf` 验证再安装并重启面板。安装脚本会检查该只读操作的 sudo 授权；未安装 crontab 不影响安装，页面会提示无法读取。

定时任务表达式支持鼠标悬停、键盘聚焦或手机点击预览之后 5 次预计执行时间。预览接口 `GET /api/v1/cron/preview?line=行号` 需要登录，以服务器当前时间重新读取并计算，默认使用服务器时区，任务前的 `CRON_TZ` 声明可覆盖时区；每个时间保留时区偏移。`@reboot`、无效表达式、无效时区和没有匹配日期时会显示说明。预览不检查 Cron 守护进程是否运行，不代表任务已执行或执行成功。原文展开后按需加载 Prism，对周期、环境变量、注释和 Shell 语法高亮，保留原始文本并由 React 安全渲染。此更新沿用 `cron list` 授权，安装脚本无需新增权限。

</details>

## Docker 与硬盘空间

“Docker 管理”只读展示运行中容器和分类空间统计，不提供容器启停、执行命令或清理。容器可写层、虚拟大小和数据卷的统计口径不同，不能直接相加作为实际总占用。

“硬盘空间”展示全部挂载及容量，支持筛选和排序；后台扫描主文件系统 `/` 的最大 30 个普通文件，以列表和矩形树图展示，不读取文件内容。其他挂载点不参与扫描，大文件排行按逻辑大小统计。

<details>
<summary>Docker 查询范围、缓存与权限</summary>

左侧“Docker 管理”页面展示与 `docker ps --size --no-trunc` 对应的运行中容器信息：名称、容器 ID、镜像、启动命令、创建时间、运行状态、端口、可写层大小和虚拟大小；不展示已停止的容器。空间汇总来自 `docker system df`，包含镜像、容器可写层、本地数据卷及构建缓存的数量、使用中数量、占用空间和可回收空间。

容器可写层不包含日志和挂载目录，数据卷在空间汇总中单独列出；虚拟大小包含可能共享的镜像层，不能按容器直接相加，因此本页不把这些值相加作为 Docker 的实际总磁盘占用。

数据在打开页面后后台采集，多个客户端共享 30 秒缓存和正在执行的查询，手动刷新至少间隔 5 秒；每条 Docker 命令最多 12 秒。容器列表或空间统计单独失败时保留对应的旧数据并显示错误。Docker 未安装、服务未启动或权限未配置时会显示状态说明。

辅助程序仅接受固定的 `docker snapshot`，内部只执行上述两条只读命令，并固定连接本机 `/var/run/docker.sock`；面板账号不加入 docker 组，不接受任意 Docker 参数，也不提供容器启停、exec 或清理功能。升级需同步安装面板、辅助程序和 `deploy/lnmp-gopanel.sudoers`（新增精确的 `docker snapshot` 权限，先通过 `visudo -cf` 验证），然后重启面板，无需重启 Docker。

</details>

<details>
<summary>挂载容量、大文件扫描与统计口径</summary>

左侧“硬盘空间”页面列出全部挂载，包括物理磁盘、内存、虚拟文件系统和绑定挂载；不支持容量的挂载仍显示，标记为“不提供容量”。列表每 30 秒刷新，可筛选挂载点、设备和文件系统类型，并可隐藏无容量或容量不可读取的挂载。点击表头可按挂载点、设备、文件系统、已用空间、总容量、可用空间或使用率升降序排序；容量使用原始数值比较，无容量及读取失败项始终排在数值项之后，零使用量仍参与排序。筛选及刷新保留当前排序。

进入页面后异步扫描主文件系统 `/`，每次仅保留最大的 30 个普通文件，按逻辑大小排序（与 `find -type f -printf '%s %p'` 的大小定义一致）。使用 NUL 分隔记录保留含空格、制表符和换行的路径；不读取文件内容、不跟随符号链接，并跳过其他挂载点，包括同设备的绑定挂载。硬链接按路径计数，稀疏文件显示逻辑大小，所以排行总和不代表实际占用的磁盘块数。

矩形树图的面积为文件大小占本次 Top 30 总大小的比例，不是占整盘容量的比例；旁边同时显示路径、文件大小和百分比。扫描以低 CPU 优先级运行，最多 5 分钟；多个浏览器共享同一进行中的任务，切换页面不会停止扫描，可点击“重新扫描”更新。进度和最新结果仅存在后端内存中，不记录历史，面板重启后清空；超时、文件消失或读取失败会标记部分结果。

相关接口：`GET /api/v1/storage/mounts`、`GET /api/v1/storage/scan`、`POST /api/v1/storage/scan`（需要 CSRF，立即返回 202）。所有接口均需登录。扫描通过固定辅助程序参数 `storage scan` 执行，不接受用户输入的路径或命令。升级部署时应同步更新两个二进制和 sudoers 模板，新规则为 `/usr/local/libexec/lnmp-gopanel-control storage scan`。

</details>

## 账户与数据

面板使用单管理员账户，支持修改密码、绑定 TOTP 和生成一次性恢复码。账户安全变更需要重新验证身份；忘记密码可通过安装脚本重置，验证器丢失也可使用离线命令关闭 TOTP。

标准安装的配置位于 `/etc/lnmp-gopanel/`，认证状态、截图、日志列表和界面组件选择等持久数据位于 `/var/lib/lnmp-gopanel/`。备份加密状态时需同时保存对应密钥，具体文件见折叠说明。实时指标、会话和建站／续签任务记录保存在内存中，重启后清空。

<details>
<summary>密码规则、TOTP、认证状态备份与离线恢复</summary>

左下角管理员区域的“账户安全”按钮可修改登录密码、绑定或关闭 TOTP，并重新生成恢复码。新密码至少 15 个 Unicode 字符、最多 72 个 UTF-8 字节，不含控制字符，且必须通过 zxcvbn 强度检测（最低 3 分）；支持空格和长口令，不要求固定字符组合。“随机生成强密码”在浏览器本地生成 24 位密码，可显示、隐藏和复制。修改密码需验证当前密码，启用 TOTP 后还需新的验证码或未使用的恢复码；成功后全部会话退出。已有密码仍可登录，初始化命令的原有 12–72 字节规则保持不变。

每个管理员支持一组 TOTP。输入当前密码后扫描二维码或手动录入密钥，再输入验证码确认启用；未确认的绑定在 5 分钟后失效，退出或取消也会清除。使用 SHA-1、6 位数字和 30 秒周期，允许前后各一个周期的时间偏差；服务器应保持时间同步。验证码成功使用后不能再次使用，需要等待下一组数字。启用后，密码正确只进入第二步，验证码或恢复码验证完成后才获得登录会话；登录挑战有效期 5 分钟，最多尝试 5 次。

密码登录和二步验证分别按直连 IP 每滚动一分钟最多 10 次，两个阶段及不同来源独立计数；每阶段最多保存 4,096 个来源窗口，满额时拒绝新增来源直至旧窗口过期。两个阶段各允许一个认证操作执行，繁忙时立即返回 `429` 和 `Retry-After: 1`，不堆积匿名任务。来源限流的 `Retry-After` 表示下次可尝试的秒数；账户安全写操作保持独立的每分钟 10 次限流。只使用 TCP 直连 IP，不信任 `X-Forwarded-For` 等代理头，因此反向代理后的用户共用代理 IP 的额度；不支持可信代理配置。

认证请求必须是单个 JSON 对象，最多 4 KiB，未知字段、多余内容和不完整 JSON 均被拒绝，超限返回 `413`。请求体读取和响应、审计写入均在认证状态锁外执行，凭据变更、一次性证明消费和会话更新保持原子性。

启用及重新生成时一次展示 10 个恢复码，支持复制、下载，每个只能使用一次，服务端仅保存哈希。重新生成后所有旧码失效。启用、关闭 TOTP 或重新生成恢复码需重新确认身份，操作后撤销其他浏览器的会话并更新当前会话。恢复码也可代替验证码执行敏感操作；刚用于登录的恢复码不能再用于关闭 TOTP，请使用另一个未使用的恢复码。

认证状态由普通面板账号保存在 `auth.state_dir`：标准安装默认 `/var/lib/lnmp-gopanel`，自定义配置默认使用配置文件同级的 `auth-state/`。目录权限必须为 `0700`，`auth-state.json`、`auth.key` 和 `auth.lock` 为 `0600`，由面板账号所有，不能是链接。首次运行从 `config.yaml` 导入管理员及 bcrypt 哈希，此后以认证状态文件为准，修改密码不会回写 root 管理的配置。TOTP 密钥使用 AES-GCM 加密；备份与恢复时必须一起保存 `auth-state.json` 和 `auth.key`，不要分别覆盖或删除。状态或密钥部分缺失、损坏时拒绝启动，不会回退接受配置中的旧密码；写入失败时停止接受新登录，需要修复存储后重启。升级保留认证状态；改密或启用 TOTP 后，不要直接降级到忽略认证状态的旧版本。

验证器和恢复码全部丢失时，可通过服务器关闭 TOTP，登录密码保持不变。标准安装执行：

```bash
sudo systemctl stop lnmp-gopanel
sudo -u lnmp-panel /usr/local/bin/lnmp-gopanel auth disable-totp --config /etc/lnmp-gopanel/config.yaml
sudo systemctl start lnmp-gopanel
```

命令使用与面板相同的独占文件锁，运行中的面板会阻止离线修改。自定义部署以状态目录所属的服务账号执行，指定实际配置文件路径。此命令用于验证器丢失，不重置忘记的登录密码。

忘记面板登录密码时，标准安装运行 `sudo bash install.sh --reset-password`，或直接运行脚本后选择“重置管理员密码”。输入并确认新密码，规则与账户安全设置一致：至少 15 个字符、最多 72 个 UTF-8 字节，且强度评分至少 3。重置会同时清除 TOTP 和全部恢复码，保留管理员用户名与其他配置；脚本自动停止服务并恢复原先的运行状态，旧会话随重启失效。支持 `--reset-password --password-stdin` 从标准输入读取一行新密码，密码不接受命令行参数。不需要联网或下载安装包；已安装二进制尚不支持此命令时需先升级面板。

自定义部署先停止面板，再以认证状态目录所属账号执行 `lnmp-gopanel auth reset-password --config PATH`（可加 `--password-stdin`），完成后自行启动服务。密码及二步验证状态原子保存到认证状态文件，不回写 `config.yaml` 的初始化哈希；写入失败时保留原认证信息。若提示密码已重置但服务启动失败，请修复服务后使用新密码登录。

账户安全接口均位于 `/api/v1`。`GET /auth/security` 只返回 TOTP 状态、剩余恢复码数和密码规则；`POST /auth/password` 接收 `current_password`、`new_password`，启用 TOTP 后再提供 `code` 或 `recovery_code`，成功返回 204 并撤销会话。`POST /auth/totp/setup` 接收 `current_password`，返回 `setup_id`、`secret`、本地二维码 `qr_code` 和 `expires_at`；`POST /auth/totp/confirm` 接收 `setup_id`、`code`，确认启用；`DELETE /auth/totp/setup` 取消未完成的绑定。`DELETE /auth/totp` 和 `POST /auth/totp/recovery-codes` 接收当前密码及二次验证凭据。启用、关闭和重新生成响应包含更新后的 `session`，生成恢复码时额外返回仅此一次的 `recovery_codes`。这些接口需要有效会话，写操作要求 CSRF 和同源校验，参数错误返回 400，重新确认身份失败返回 403，状态过期或冲突返回 409，存储错误返回 503。

`POST /auth/login` 未开启 TOTP 时保留原有响应；开启时返回 `totp_required`、`challenge_token`、`expires_in`，不签发 Cookie。随后向 `POST /auth/login/totp` 发送 `challenge_token` 及 `code` 或 `recovery_code` 完成登录。登录两步无需已有会话及 CSRF，但均校验来源并限流。仅密码校验失败时，原始输入的密码会写入专用登录日志；成功验证的密码、TOTP 密钥、二维码、验证码和恢复码不会写入日志，认证凭据不写入浏览器持久存储。

</details>

<details>
<summary>配置文件、状态文件与浏览器存储</summary>

配置示例见 [`deploy/config.example.yaml`](deploy/config.example.yaml) 和 [`deploy/services.example.yaml`](deploy/services.example.yaml)。前者定义面板 HTTPS、认证、日志、截图与可选指标，后者定义被管理服务。配置由 root 管理，普通面板账号仅有读取权限；手工修改服务配置后重启面板，使两侧读取一致。

| 标准安装位置                                                        | 内容与备份要求                                                                       |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `/etc/lnmp-gopanel/config.yaml`                                     | 面板配置、初始化密码哈希、可选数据库监控凭据；账户初始化后密码以认证状态为准         |
| `/etc/lnmp-gopanel/services.yaml`                                   | 服务管理方式及可信程序路径                                                           |
| `/etc/lnmp-gopanel/tls/`                                            | 面板证书副本和私钥；借用的源证书续期后需重新同步                                     |
| `/var/lib/lnmp-gopanel/auth-state.json`、`auth.key`                 | 账户与加密 TOTP 状态，必须一起备份和恢复                                             |
| `/var/lib/lnmp-gopanel/screenshot-settings.json`、`screenshots.key` | 截图设置与加密认证凭据，必须一起备份和恢复；截图图片独立保存                         |
| `/var/lib/lnmp-gopanel/custom-logs.json`                            | 自定义日志列表，不包含原日志内容                                                     |
| `/var/lib/lnmp-gopanel/login.log`                                   | 登录审计记录，不自动轮转；密码校验失败时可包含输入的原始失败密码，具体规则见日志说明 |
| `/var/lib/lnmp-gopanel/dashboard-widgets.json`                      | 所有客户端共享的可选指标选择                                                         |
| `/var/lib/lnmp-gopanel/service-release-ranges.json`                 | 所有客户端共享的版本查询范围                                                         |
| `/var/backups/lnmp-gopanel/`                                        | 安装／升级及监控配置备份；`pending` 指示待恢复的安装事务                             |
| `/var/lib/lnmp-gopanel-install/`                                    | root 专属的 LNMP 安装任务与脱敏日志，卸载面板时保留                                  |

面板状态目录由面板账号所有，权限 `0700`，私有状态文件为 `0600`；配置、证书及特殊文件的权限见部署和对应功能说明。自定义配置文件不在 `/etc/lnmp-gopanel` 时，未显式指定的状态目录默认放在配置文件同级的 `auth-state/`、`logs-state/`、`screenshots-state/`、`metrics-state/`。

浏览器只持久保存主题、桌面侧栏偏好及每个日志文件的筛选规则；日志正文和指标趋势不写入浏览器持久存储。登录页演示使用本地示例数据，不能作为服务器实际状态。

必要运行和服务操作记录写入 systemd journal，可用 `journalctl -u lnmp-gopanel` 排查。标准运行日志不记录指标样本或服务命令原始输出；登录审计文件按日志章节的独立规则记录认证事件。

</details>

## 部署与开发

推荐使用安装脚本部署，以同步更新面板、特权辅助程序、systemd 单元和 sudoers 规则。Web 进程以普通账号运行，只通过固定辅助程序执行白名单特权操作。

源码支持本地开发、两种架构的安装包构建与自动化验证，具体环境和步骤见下方。

<details>
<summary>手工 Linux 部署、systemd 权限与 HTTPS 反向代理</summary>

先将 `bin/` 和 `deploy/` 复制到目标机器。以下命令由管理员在项目目录执行，需要系统具备 systemd、sudo 和 visudo（通常来自 sudo 软件包）：

```bash
sudo groupadd --system lnmp-panel
sudo useradd --system --gid lnmp-panel --no-create-home --shell /usr/sbin/nologin lnmp-panel
sudo install -d -o root -g root -m 0755 /usr/local/libexec
sudo install -o root -g root -m 0755 bin/lnmp-gopanel /usr/local/bin/lnmp-gopanel
sudo install -o root -g root -m 0755 bin/lnmp-gopanel-control /usr/local/libexec/lnmp-gopanel-control
sudo /usr/local/bin/lnmp-gopanel init
```

**检查 `/etc/lnmp-gopanel/services.yaml`**：每个服务固定一种管理方式，不要同时调用 systemd 与 init 管理同一服务。初始化优先探测已加载的原生 systemd 单元（排除 systemd 自动生成的 SysV 包装单元），其次探测 init 脚本，再对有已知程序的服务探测 LNMP 命令。对于已有 init 脚本、实际进程运行但原生 unit 为 failed/inactive 的 Nginx/PHP-FPM，保留 init 管理方式，避免误报停止或重复启动。路径与服务名使用白名单，暂不支持非标准 PHP 版本命名或自定义 unit。可删除不需要管理的服务；未检测到服务时允许 `services: []`。

确认服务配置后安装权限规则并启动：

```bash
sudo chown root:lnmp-panel /etc/lnmp-gopanel /etc/lnmp-gopanel/config.yaml
sudo chmod 0750 /etc/lnmp-gopanel
sudo chmod 0640 /etc/lnmp-gopanel/config.yaml
sudo chown -R root:lnmp-panel /etc/lnmp-gopanel/tls
sudo chmod 0750 /etc/lnmp-gopanel/tls
sudo chmod 0640 /etc/lnmp-gopanel/tls/panel.crt /etc/lnmp-gopanel/tls/panel.key
sudo chown root:root /etc/lnmp-gopanel/services.yaml
sudo chmod 0644 /etc/lnmp-gopanel/services.yaml
sudo visudo -cf deploy/lnmp-gopanel.sudoers
sudo install -o root -g root -m 0440 deploy/lnmp-gopanel.sudoers /etc/sudoers.d/lnmp-gopanel
sudo install -o root -g root -m 0644 deploy/lnmp-gopanel.service /etc/systemd/system/lnmp-gopanel.service
sudo systemctl daemon-reload
sudo systemctl enable --now lnmp-gopanel
```

Web 进程没有任意命令接口。它只能通过 `/usr/bin/sudo -n` 调用固定辅助程序；辅助程序只读取固定位置的 root 配置，验证配置与可执行文件及其父目录为 root 所有、不可由组或其他用户写入，再执行白名单服务操作。不使用 setuid 文件权限。systemd 配置显式关闭 `NoNewPrivileges`，同时关闭在 systemd 252 等版本中会对普通服务账号隐式启用它的 `ProtectKernelTunables`、`ProtectKernelModules`、`RestrictRealtime` 和 `LockPersonality`；仅设置 `NoNewPrivileges=false` 不足以解除这些隐式限制。保留 `ProtectControlGroups=true`，不启用 `RestrictSUIDSGID`。辅助程序会继承 systemd 的文件系统视图，因此不启用 `ProtectHome` 或将 `/usr/local` 设为只读，避免阻止 LNMP 脚本读写 PID、日志及配置检查所需路径。普通 Web 用户仍受 Unix 文件权限限制。

脚本安装默认对外提供 HTTPS，实际端口以 `/etc/lnmp-gopanel/config.yaml` 的 `listen` 为准，并在重启后保持不变。例如安装结果为 `0.0.0.0:39481`，则访问 `https://服务器公网IP:39481/`，借用证书时访问 `https://证书域名:39481/`。`0.0.0.0` 表示绑定所有 IPv4 网卡，不是浏览器访问地址。需要放行对应 TCP 端口；脚本不自动修改安全组或防火墙。

可在服务器本机检查 HTTPS（自签名证书用安装的证书验证）：

```bash
sudo ss -lntp 'sport = :39481'
curl --cacert /etc/lnmp-gopanel/tls/panel.crt -I https://127.0.0.1:39481/
```

以上 `39481` 是示例，需替换为实际端口；借用证书时使用证书域名和对应的 CA 信任链。明文 HTTP 请求不会进入面板。若本机 HTTPS 正常但公网不通，检查安全组、防火墙及公网 IP / NAT 端口映射。

手工执行 `init` 默认监听 `127.0.0.1:8090` 并生成自签名证书，仅接受本机 HTTPS；可通过 `--listen 0.0.0.0:0` 初始化为随机公网监听端口，或使用 SSH 隧道访问：

```bash
ssh -N -L 8090:127.0.0.1:8090 user@your-server
```

然后访问 `https://127.0.0.1:8090/`。脚本安装使用隧道时，将隧道目标端口替换为实际随机端口。

安装时显示的 `/tmp/lnmp-gopanel-install.*/config` 是暂存目录；实际配置安装在 `/etc/lnmp-gopanel/`。

使用 HTTPS 反向代理时，面板的上游连接也必须使用 HTTPS。参考 `deploy/nginx.conf.example`，将代理端口改为实际监听端口，配置证书信任及匹配的 `proxy_ssl_name`，并将面板配置设置为：

```yaml
public_origin: https://panel.example.com
```

面板始终启用 Secure Cookie。配置改变后重启面板；旧的 HTTP `public_origin` 在迁移时清空。SSE 的代理路径必须关闭缓冲与缓存。若面板依赖被管理的 Nginx 代理，停止它会中断访问，可使用面板自身的 HTTPS 端口或 SSH 隧道保留管理通道。

</details>

<details>
<summary>源码构建与构建信息</summary>

技术栈为 Go / Gin、React / TypeScript / Vite，使用 shadcn/ui、Tailwind CSS 和 Recharts。构建机需要 Go 1.26+、Node.js 22.12+、npm、make。目标机器为 Linux，运行时无需 Node.js。

```bash
make build
```

生成 `bin/lnmp-gopanel` 和 `bin/lnmp-gopanel-control`。生产面板使用 `-tags embed` 将前端嵌入 Go 二进制；服务辅助程序为独立二进制。

鼠标悬停或键盘聚焦侧栏用户头像（手机点击）可在带图标的提示中查看实际版本与构建时间。`make build` 和未指定版本的打包默认使用 `dev-<Git短提交号>`；`bash scripts/package.sh amd64 v0.1.0` 使用指定版本，并与包内 `VERSION` 保持一致。构建时间以 UTC 注入程序，页面固定显示北京时间（UTC+8）；可用 `VERSION`（Makefile）与 `BUILD_TIME`（格式 `YYYY-MM-DDTHH:MM:SSZ`）覆盖构建信息。CI 同一轮的两个架构共用构建时间。普通 `go run` 显示 `dev` 和未知构建时间。`GET /api/v1/build` 需要登录，返回 `version`、`built_at`。

</details>

<details>
<summary>本地后端与前端开发</summary>

从仓库根目录执行，首次初始化会提示输入密码（12–72 字节），检测本机服务，不执行服务启停：

```bash
./bin/lnmp-gopanel init --dir .
./bin/lnmp-gopanel --config ./config.yaml --services ./services.yaml
```

访问 https://127.0.0.1:8090 ，并信任初始化生成的自签名证书。默认用户名为 `admin`，密码为初始化时设置的值。CLI 也支持 `init --username NAME`、`--password-stdin`、`--listen HOST:PORT` 和 `--certificate-dir DIR`；端口设为 `0` 时自动选择空闲端口。请勿将密码直接写入命令历史。

如果开发容器只能以 root 运行，可临时添加 `--allow-root-for-development`；生产环境使用手工 Linux 部署中的普通用户方案。未安装辅助程序时仍可查看系统指标，服务状态显示未知，不会模拟服务数据。

前端开发：先运行后端，再在 `web` 目录执行 `npm ci && npm run dev`，访问 Vite 输出的本地 HTTPS 地址并信任证书；Vite 使用仓库根目录 `tls/` 中初始化生成的证书，`/api` 自动代理到 HTTPS 8090，使用 Secure Cookie 认证。普通 `go run ./cmd/panel` 从 `web/dist` 读取构建产物，不带生产嵌入标签。

</details>

<details>
<summary>单元测试、特权测试、浏览器回归与安装包验证</summary>

在源码仓库根目录执行；安装包冒烟需在对应架构的 Linux 主机运行。

```bash
make test                    # Go 竞态测试与前端单元测试
make check                   # Go vet 与前端生产构建
make test-security           # Go 校验、可达漏洞扫描与 npm audit
sudo env "PATH=$PATH" make test-privileged  # 需 nginx、openssl；指定用例不得跳过
make test-browser            # 构建本轮程序，执行三套桌面／移动端回归
bash scripts/package.sh amd64 # arm64 主机改用 arm64
python3 scripts/test-package.py dist/lnmp-gopanel_linux_amd64.tar.gz
```

浏览器测试需先在 `web` 执行 `npx playwright install --with-deps chromium`。设置 `GOPANEL_E2E_BINARY` 可指定本轮解包的生产程序；默认使用 `bin/lnmp-gopanel`。并发开发验证可通过 `GOPANEL_E2E_PORT_BASE` 指定六个连续端口，并用 Playwright `--output` 分离测试产物。每个浏览器用例使用独立后端、临时配置和认证状态，真实登录额度不跨用例共用；独立实例禁用真实 helper，避免触碰宿主机服务；安装实例契约测试仅由一次性 CI 环境使用真实 helper。`scripts/test-systemd.py` 和 Debian VM 脚本拒绝在普通开发或已有面板安装的服务器上执行，请勿在生产机伪造 CI 环境标记。

</details>

<details>
<summary>前端资源更新、按需加载与浏览器行为</summary>

生产页面由独立脚本检测 CSS／JS 资源更新：打开页面、重新聚焦、返回前台及每分钟检查一次最新入口，发现资源版本变化后自动刷新当前地址；旧脚本加载失败时也可恢复。网络失败或无效响应不触发刷新，同一目标版本每个标签页只自动刷新一次，避免代理缓存导致循环。HTML 不缓存，带哈希文件名的构建资源长期缓存。首次升级到支持此功能的版本时，已打开的旧页面需要先手动刷新一次。

桌面端可通过顶部左侧按钮隐藏或显示导航侧边栏，隐藏后主内容区扩展；当前浏览器会记住这一偏好。手机端继续使用抽屉导航，独立于桌面隐藏状态。

构建预生成有收益的 JS／CSS gzip 文件，直连面板也可协商压缩；HTML 继续禁止缓存，哈希资源长期缓存。HEAD 返回对应表示长度，Range 使用原始字节偏移；API 和 SSE 不进入静态资源压缩路径。登录页的工作区、业务页面、账户设置和非当前演示按需加载；移动端不下载演示组件。构建检查完整静态依赖图，浏览器进一步检查可见演示加载后的传输量：JS gzip 不超过 250 KiB、CSS 不超过 40 KiB。耗时随测试报告保存，用于排查趋势，不使用波动较大的绝对耗时作为唯一门禁。

演示加载失败时保留登录表单；工作区、业务页面和账户设置加载失败时显示局部提示与重新加载入口，业务页面的导航仍可使用。

登录页每 5 秒同步轮换标语和资源趋势、服务状态、磁盘空间、网络吞吐、日志速览五组演示，支持圆点切换和暂停。演示使用本地示例数据，手机端只展示标语；悬停、焦点进入展示区或页面进入后台时临时暂停，系统启用减少动态效果时默认暂停并关闭过渡。

</details>

<details>
<summary>GitHub Actions、发布产物与当前源码打包升级</summary>

[Build and release](https://github.com/azpanel/lnmp-gopanel/actions/workflows/build.yml) 在每个 Pull Request 运行格式检查、Go 竞态与前端单元测试、依赖漏洞扫描、隔离安装脚本测试、相关包的完整 root 测试（关键特权／真实 Nginx 测试必须执行），以及桌面与移动端浏览器回归。默认、账户安全和登录审计三套 Playwright 配置分别执行。浏览器测试使用本轮构建的嵌入式生产程序；缺失或跳过指定关键测试会失败。

每个 PR 还在一次性 Ubuntu 24.04 amd64 托管 runner 上安装真实面板包，检查 sudo 精确授权、systemd、普通账号的 Chromium 沙箱，以及缺失授权、证书权限错误、NoNewPrivileges 限制时的升级回滚。真实 systemd 测试同时用临时服务复现守护进程继承面板控制组的问题，并验证独立 scope 在面板重启、停止后仍保留原守护进程，以及显式重启、停止和空 scope 回收。主分支、标签和手动触发另外验证原生 arm64 包，以及 Debian 12／systemd 252 虚拟机；Debian 官方云镜像使用其发布的 SHA-512 清单校验。标准发布需要等待基础检查和扩展矩阵全部通过；手动选择快速模式时，仅以基础检查、双架构打包和安装包冒烟验证作为发布条件。上述安装测试只安装面板，不安装 LNMP；已有 chroot mock 测试继续用于事务故障注入。故障时保留浏览器 trace、截图和服务日志。

前端被嵌入面板程序，两个 Go 程序均禁用 CGO；实际安装包在源码目录外验证 HTTPS、登录、资源、版本、缓存、压缩和 Range 行为。Actions 的 `lnmp-gopanel-linux` Artifact 保留 30 天。

推送实际版本的 `v*` 标签会在构建通过后自动创建 GitHub Release（以下 `vX.Y.Z` 需替换为版本号），包含两种架构的 `.tar.gz`、各自的 `.sha256`、汇总的 `SHA256SUMS` 和 `install.sh`：

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

### 手动快速发布

在 GitHub **Actions → Build and release → Run workflow** 中选择要发布的分支，设置 `mode=fast`，填写尚未使用的 `release_tag`（例如 `v0.1.20`）。**不要提前创建或推送这个标签**：工作流在检查通过后，将标签创建在本轮构建的确切提交上，并上传全部附件后发布正式 Release。

也可使用 GitHub CLI：

```bash
gh workflow run build.yml --ref main -f mode=fast -f release_tag=v0.1.20
```

快速模式保留前端格式、单元测试和漏洞扫描，Go 格式、模块校验、静态分析、竞态测试和漏洞扫描，安装／打包脚本检查及特权／真实 Nginx 测试，以及 amd64／arm64 打包、安装包 HTTPS 冒烟验证和 SHA-256 校验。按目前基础构建耗时，通常约需 5 分钟，另加 runner 排队时间。

快速模式跳过完整浏览器、amd64／arm64 systemd 真实安装和 Debian 12 虚拟机回归，Release 说明会标注这一检查范围。修改安装器、sudoers、systemd 或跨平台行为时，选择 `mode=full` 并填写新版本号，执行完整矩阵后发布。`full` 为默认模式；不填版本号时仅验证，手动运行已有 `v*` 标签时沿用标准标签发布。直接推送 `v*` 标签仍执行完整发布流程。

新版本号若已存在标签或 Release，会在构建前拒绝；手动发布不会覆盖已有正式附件。中断后可在同一工作流中重跑失败的发布任务，已上传的草稿可继续完成。工作流使用 `GITHUB_TOKEN` 创建标签，不会再触发一轮标签推送 CI。

本地生成相同布局的包：

```bash
cd web && npm ci && npm run build && cd ..
bash scripts/package.sh amd64 vX.Y.Z
bash scripts/package.sh arm64 vX.Y.Z
```

包内包含 `bin/lnmp-gopanel`、`bin/lnmp-gopanel-control`、`deploy/`、`install.sh`、`README.md` 和 `VERSION`；本机配置、账号密码、依赖目录、测试截图和构建缓存不会提交或打包。

从当前源码升级本机时，先构建前端，再运行上述打包命令，最后使用项目中的安装脚本执行 `sudo bash install.sh --archive ./dist/lnmp-gopanel_linux_amd64.tar.gz`（arm64 使用对应文件名）。这样两个二进制、systemd 单元及 sudoers 会一起更新，并使用相同的备份、授权检查及失败回滚流程。定时任务、日志与虚拟主机仅检查 `cron list`、`logs list/read`、`vhosts list/config/format/usage/toggle/options/create/ssl-renew-preview/ssl-renew/basic-auth-get/basic-auth-set/basic-auth-disable` 的精确授权，不读取业务内容、不创建站点或申请证书；缺少可选组件或业务文件不会阻止安装。旧版 HTTP 安装非交互升级时需追加 `--tls-mode self-signed`，或指定现有证书。

</details>

## API

面板提供经过身份验证的 HTTP API，用于查询监控数据和执行已支持的管理操作。路由与调用约定见下方。

<details>
<summary>路由索引、数据格式与连接限制</summary>

接口位于 `/api/v1`。除两步登录外均需 Cookie 会话，写操作要求 `X-CSRF-Token` 和同源校验；实时指标与任务日志提供 SSE。

路由实现见 [`internal/server/server.go`](internal/server/server.go) 及同目录功能文件，参数和错误处理详见各功能折叠说明。以下路径均省略 `/api/v1` 前缀。

| 功能           | 方法与路径                                                                                                                                                                                                                 |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 登录与会话     | `POST /auth/login`、`POST /auth/login/totp`、`GET /auth/session`、`POST /auth/logout`                                                                                                                                      |
| 账户安全       | `GET /auth/security`、`POST /auth/password`、`POST /auth/totp/setup`、`DELETE /auth/totp/setup`、`POST /auth/totp/confirm`、`DELETE /auth/totp`、`POST /auth/totp/recovery-codes`                                          |
| 构建与主机     | `GET /build`、`GET /host`                                                                                                                                                                                                  |
| 日志设置       | `GET /system/logging`、`POST /system/logging/:kind/enable`（`kind` 为 `php` 或 `database`，POST 无请求体） |
| 指标           | `GET /metrics`、`GET /metrics/stream`、`GET /dashboard/widgets`、`PUT /dashboard/widgets`                                                                                                                                  |
| 服务与组件     | `GET /services`、`GET /components`、`POST /components/refresh`、`POST /services/:name/actions/:action`                                                                                                                     |
| 服务管理修复   | `GET /services/:name/management-repair`、`POST /services/:name/management-repair`                                                                                                                                          |
| 版本查询       | `GET /services/releases`、`POST /services/releases/refresh`、`PUT /services/releases/:name/range`                                                                                                                          |
| LNMP 安装      | `GET /lnmp-installation`、`POST /lnmp-installation/prepare`、`POST /lnmp-installation/jobs/:id/start`、`GET /lnmp-installation/jobs/:id`、`GET /lnmp-installation/jobs/:id/events`、`GET /lnmp-installation/jobs/:id/logs` |
| 网站列表与配置 | `GET /vhosts`、`GET /vhosts/count`、`POST /vhosts/refresh`、`GET /vhosts/config`、`POST /vhosts/config/format`、`POST /vhosts/toggle`                                                                                      |
| 建站与 SSL     | `GET /vhosts/options`、`POST /vhosts/check-dns`、`POST /vhosts`、`POST /vhosts/ssl/retry`、`POST /vhosts/ssl/renew/preview`、`POST /vhosts/ssl/renew`                                                                      |
| 建站／续签任务 | `GET /vhosts/jobs`、`GET /vhosts/jobs/:id`、`GET /vhosts/jobs/:id/events`                                                                                                                                                  |
| 站点认证       | `GET /vhosts/basic-auth`、`PUT /vhosts/basic-auth`、`DELETE /vhosts/basic-auth`                                                                                                                                            |
| 截图           | `GET /vhosts/screenshots`、`PUT /vhosts/screenshot`、`POST /vhosts/screenshot/refresh`、`DELETE /vhosts/screenshot`、`GET /vhosts/screenshot/image`                                                                        |
| 日志           | `GET /logs`、`POST /logs/read`、`POST /logs/custom`、`DELETE /logs/custom`                                                                                                                                                 |
| 定时任务       | `GET /cron`、`GET /cron/preview`                                                                                                                                                                                           |
| Docker         | `GET /docker`、`POST /docker/refresh`                                                                                                                                                                                      |
| 存储           | `GET /storage/mounts`、`GET /storage/scan`、`POST /storage/scan`                                                                                                                                                           |

响应禁止缓存，跨源 Origin 被拒绝，不开启 CORS。指标快照包含 `timestamp`、`cpu`、`memory`、`swap`、`load`、`uptime`、`network`、`disk_io`、`disks` 和 `errors`，并按选择增加可选指标。容量单位为字节、速率为字节/秒、使用率为 0–100，时间为 RFC 3339。无法采集的字段为 `null` 或省略可选数据，原因位于 `errors`；接口错误统一为 `{ "error": "说明" }`。

会话最长 8 小时，面板重启后失效。密码登录与二步验证分别按 TCP 直连 IP 每滚动一分钟最多 10 次；不信任代理来源头，反向代理后的用户共享代理 IP 的额度。全局 SSE 连接数上限为 32，慢连接限时写入，指标订阅只缓存最新快照。

</details>
