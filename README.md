# LNMP GoPanel

面向 lnmp.org 安装环境的单机实时监控与服务管理面板。Go + Gin 后端，React + TypeScript + Vite 前端，使用 shadcn/ui、Tailwind CSS 和 Recharts。

- CPU、内存、Swap、负载、运行时间、磁盘容量、磁盘 I/O 和网络速率。
- SSE 每 2 秒推送；统一采集，多浏览器共享。无数据库、Redis 和指标历史文件。
- 浏览器仅在内存中保留最近 5 分钟、最多 150 个采样点；刷新或退出登录即清空。重连只取最新数据，断档不补线。
- Nginx、PHP-FPM、MySQL / MariaDB 状态与启停、重启；Nginx 配置检查通过后才允许重载。Redis 提供只读状态监控。
- 显示服务已安装版本，异步查询官方最新稳定版本及 changelog。
- 未安装 LNMP 时提供在线安装，支持可选 Nginx 模块、持久化安装任务和实时编译输出。
- Docker 只读概览：运行中容器的基础信息、可写层与虚拟大小，以及镜像、容器、数据卷和构建缓存的空间占用。
- 定时任务只读查看 root crontab，展示执行周期、命令和原文，支持刷新。
- 日志读取支持登录日志、网站、MariaDB 慢查询及 PHP 错误日志，按需加载大文件、实时追踪和多条关键词／正则筛选。
- 单管理员、bcrypt 密码、TOTP 双重验证、内存 Session、CSRF 校验；独立特权辅助程序和精确 sudoers 规则。
- 中文、默认亮色、深浅主题切换、移动端导航。登录页、工作区及通知使用一致的主题。主题偏好及每个日志文件的筛选规则写入 localStorage，日志内容不保存到浏览器持久存储。
- 登录页每 5 秒同步轮换标语和资源趋势、服务状态、磁盘空间、网络吞吐、日志速览五组演示，支持圆点切换和暂停。演示使用本地示例数据，手机端只展示标语；悬停、焦点进入展示区或页面进入后台时临时暂停，系统启用减少动态效果时默认暂停并关闭过渡。

## 安装、更新与卸载

在 Linux amd64 / arm64 机器上，以 root 或具备 sudo 权限的账号执行。需要运行中的 systemd、`curl`、`tar`、`gzip`、`sudo`（含 `visudo`）和常见系统工具；Debian / Ubuntu 可先执行 `sudo apt-get install -y curl ca-certificates tar gzip sudo`。

```bash
curl -fsSL https://raw.githubusercontent.com/azpanel/lnmp-gopanel/main/install.sh -o install.sh
sudo bash install.sh
```

直接运行会显示中文管理菜单，可选择安装、从 GitHub Release 更新、完全卸载或退出。也可以显式使用 `--install`、`--update`、`--uninstall`；已有带安装参数的调用方式继续有效。

管理脚本从主分支下载，面板安装包仍从 GitHub Release 下载并校验 SHA-256。

管理脚本与安装包需要兼容。旧版 Release（如 `v0.1.1`）尚不支持 HTTPS 初始化，仅替换脚本后安装会在选择证书后出现 `run as a dedicated unprivileged user`。当前脚本会在询问证书和密码前检查兼容性（`--check` 也会检查），并明确拒绝旧包。遇到该错误需使用包含 HTTPS 功能的新 Release，或按下文从当前源码打包后使用 `--archive` 安装。

```bash
sudo bash install.sh --update                    # 从 GitHub Release 更新到最新版本
sudo bash install.sh --update --version v0.1.0   # 更新到指定 Release
sudo bash install.sh --uninstall                 # 列出清理范围，输入 yes 确认
sudo bash install.sh --uninstall --yes           # 非交互完全卸载
```

如果出现 `Error: Unknown argument: --uninstall`，说明当前执行的 `install.sh` 是尚未支持卸载的旧版。主分支更新不会自动替换已发布的 Release 附件；可单独下载主分支的管理脚本后卸载，无需先升级面板：

```bash
curl -fsSL https://raw.githubusercontent.com/azpanel/lnmp-gopanel/main/install.sh -o install-latest.sh &&
  sudo bash install-latest.sh --uninstall
```

更新要求已有完整安装，下载对应架构的 Release 安装包并验证 SHA-256，沿用备份和失败回滚流程。完全卸载无需联网，会停止并禁用面板，删除面板程序、辅助程序、systemd 单元及专属覆盖配置、sudoers 规则、`/etc/lnmp-gopanel`（含面板证书副本）、`/var/lib/lnmp-gopanel`、`/var/backups/lnmp-gopanel` 和 `lnmp-panel` 专用账号及组。LNMP、网站、数据库、Docker、借用的原始证书和 Chromium 等系统依赖会保留；自定义到其他目录的面板数据请自行清理。卸载会永久删除面板配置及备份，请先保存需要保留的内容。

如需网站截图，可在 Debian 12/13 或 Ubuntu 22.04/24.04/26.04（amd64 / arm64）使用 `sudo bash install.sh --with-screenshots`，额外安装 Chromium、沙箱组件和中文字体，并以面板普通账号运行截图冒烟检查。已有原生 Chromium/Chrome 时优先复用；其他 Linux 发行版请自行安装浏览器，通过 `screenshots.browser` 指定其绝对路径。普通安装不安装浏览器，缺少浏览器只影响截图功能。`--check` 不安装系统依赖；可选安装产生的系统软件包和 APT 源配置不会在面板文件回滚或卸载时删除。

Ubuntu 的官方 `chromium-browser` 是转到 Snap 的过渡包，Snap 的账号主目录和文件访问限制与面板账号及截图临时目录不兼容。因此 Ubuntu 自动安装使用第三方 [XtraDeb PPA](https://launchpad.net/~xtradeb/+archive/ubuntu/apps) 提供的原生 deb 包，跳过已有的 Snap 启动脚本，不关闭浏览器沙箱。脚本通过独立密钥文件及完整指纹校验限定签名来源，并将该源限定为仅提供 `chromium*` 软件包；后续可通过 APT 更新。相关配置位于 `/etc/apt/sources.list.d/lnmp-gopanel-chromium.sources`、`/etc/apt/preferences.d/lnmp-gopanel-chromium` 和 `/etc/apt/keyrings/lnmp-gopanel-chromium.asc`。不希望添加第三方源时，请先自行安装原生 Chromium/Chrome。

Ubuntu 24.04 及更新版本若启用了 AppArmor 用户命名空间限制，脚本会加载系统已有的 `/etc/apparmor.d/chromium`；没有该配置时创建 `/etc/apparmor.d/lnmp-gopanel-chromium`，仅允许 `/usr/lib/chromium/chromium` 使用用户命名空间，并在缺少解析器时安装 `apparmor`。不会修改全局内核限制。该浏览器配置与 Chromium 一同保留，面板回滚或卸载时不删除。

脚本下载最新 Release 中对应架构的预编译安装包，验证 SHA-256，创建普通服务账号并安装面板、特权辅助程序、精确的 sudoers 规则及 systemd 服务。首次安装交互选择 SSL 证书并设置管理员密码（12–72 字节），检测本机 LNMP / Redis 服务，并启动面板。面板仅提供 HTTPS，监听 `0.0.0.0` 上由系统随机分配的空闲端口；安装完成后显示监听地址和端口，需在安全组及防火墙中放行该 TCP 端口。运行时无需 Go、Node.js 或 OpenSSL。

证书有两种来源：

- 自动生成自签名证书：无需填写域名、组织等表单，自动使用 ECDSA P-256 密钥、随机序列号、一年有效期，以及 localhost、本机主机名和网卡 IP 的 SAN。TLS 最低版本为 1.2，浏览器需要信任该证书；服务器公网 IP 经 NAT 映射时可能不在自动发现的 SAN 中。
- 借用现有证书：扫描 `/usr/local/nginx/conf/ssl` 下的目录，仅列出存在有效证书及匹配的未加密私钥的目录，支持 `.pem`、`.crt`、`.cer`、`.key` 及 ACME 符号链接，优先使用 fullchain。选择后复制到面板专用目录，不修改源证书或私钥权限。使用证书覆盖的域名访问。

证书和私钥安装到 `/etc/lnmp-gopanel/tls/`，目录权限 `0750`、文件权限 `0640`、所有者 `root:lnmp-panel`。借用的证书不会自动跟随原证书续期；续期后重新执行 `sudo bash install.sh --certificate-dir /usr/local/nginx/conf/ssl/域名`，同步证书并重启面板，已有 HTTPS 端口保持不变。自签名证书可通过 `--tls-mode self-signed` 重新生成。

安装时会以 `lnmp-panel` 账号检查服务状态、Docker 查询、文件扫描、定时任务、日志读取及虚拟主机全部十二项操作的精确 sudo 授权，并实际免密调用只读 `docker snapshot`、`components snapshot` 辅助程序（无需安装或启动 Docker，不触发全盘扫描或服务启停）。虚拟主机只检查 `list/config/format/usage/toggle/options/create/ssl-renew-preview/ssl-renew/basic-auth-get/basic-auth-set/basic-auth-disable` 授权，不读取站点或证书账号、不创建站点或申请证书。启动面板后读取实际进程的 `/proc/<MainPID>/status`，确认 `NoNewPrivs: 0`，避免 systemd 隐式限制使所有辅助功能不可用；检查失败会报错并回滚。`--no-start` 只执行辅助程序检查，进程限制需在自行启动后验证。

在菜单选择更新或执行 `sudo bash install.sh --update` 即可从 GitHub Release 升级；带安装参数再次执行也可覆盖升级。已有 HTTPS 配置、端口、证书、管理员密码和 `services.yaml` 会保留。旧版 HTTP 安装升级时会询问证书来源、迁移为随机端口的 HTTPS，并保留管理员凭据及服务配置；原 HTTP 端口不再使用，需调整安全组或反向代理。升级只重启面板服务，不重启 Nginx、PHP、数据库、Redis 或 Docker；已有面板会话需重新登录。替换前的文件保存在 `/var/backups/lnmp-gopanel/`，替换或启动失败时自动恢复旧文件，包含原配置及证书。

指定版本、自定义首次安装用户名、只验证包及离线安装：

```bash
sudo bash install.sh --version v0.1.0 --username admin
bash install.sh --version v0.1.0 --check
# 从 Release 下载两个同名配套文件后执行；arm64 使用对应的 arm64 文件名
sudo bash install.sh --archive ./lnmp-gopanel_linux_amd64.tar.gz
```

离线安装同时需要 `lnmp-gopanel_linux_amd64.tar.gz.sha256`（或通过 `--checksum-file` 指定校验文件）。`--no-start` 用于安装后自行检查配置，脚本不会启用或重启面板；检查后执行 `sudo systemctl enable --now lnmp-gopanel`，已有运行实例升级后需执行 `sudo systemctl restart lnmp-gopanel`。非交互首次安装使用 `--password-stdin --tls-mode self-signed`，或 `--password-stdin --tls-mode existing --certificate-dir /usr/local/nginx/conf/ssl/域名`；密码从标准输入读取，勿把密码放入命令行参数。非交互旧版 HTTP 升级也需显式选择证书模式。

## 在线安装 LNMP

在“LNMP 服务”或系统概览中点击“安装 LNMP”，进入 `/lnmp/install`。先从 [azpanel/lnmp2.3-offline](https://github.com/azpanel/lnmp2.3-offline) 的 `main` 分支获取最新源码到 `/root/lnmp2.3-offline`，再按实际脚本选择数据库、PHP、内存分配器与默认站点保护方式。准备完成后固定该提交安装；上游选项结构不兼容时会停止并提示。

Nginx 默认勾选 VTS、GeoIP2、FancyIndex 和 Substitutions Filter，支持逐项取消。所选模块从各自仓库的默认分支拉取并记录提交，使用 `--add-module` 静态编译，FancyIndex 不会重复添加。选择 GeoIP2 时，apt 系统自动安装 `libmaxminddb0 libmaxminddb-dev mmdb-bin`；dnf/yum 系统安装 `libmaxminddb libmaxminddb-devel`。Rocky、AlmaLinux、CentOS 8 在本次命令中启用官方 PowerTools 源，9 及以后启用 CRB 源，不永久更改源配置。软件源缺包或模块编译失败会明确终止，不会跳过。MMDB 数据库、统计访问入口和模块业务配置需自行配置。

安装通过独立 systemd 服务运行，关闭页面或重启面板后继续执行。页面展示阶段、运行时间及编译输出，支持断线续传、轮询、历史日志和下载。任务状态与脱敏日志保存在 root 专属的 `/var/lib/lnmp-gopanel-install`，原始安装日志为 `/root/lnmp-install.log`。服务器重启后未完成的任务标记为中断，不自动恢复编译。成功后自动更新面板服务列表。

在线安装仅用于无 LNMP 和冲突组件的服务器。已有组件、目录残留或端口冲突时会阻止安装；失败保留源码和日志，由管理员处理后重试，不自动删除数据库或网站。面板卸载保留上述安装记录、源码与 LNMP 环境。升级时请同时更新辅助程序及 sudoers：新增的 `lnmp-install inspect/prepare/start/read` 仅在面板安装自检中检查授权，不触发环境安装。

## GitHub Actions 产物

[Build and release](https://github.com/azpanel/lnmp-gopanel/actions/workflows/build.yml) 在主分支推送、Pull Request 和手动触发时检查格式、运行 Go 竞态测试与前端测试、执行隔离的安装脚本测试，然后构建 Linux amd64 / arm64 安装包。前端被嵌入面板程序，两个 Go 程序均禁用 CGO。Actions 的 `lnmp-gopanel-linux` Artifact 保留 30 天。

推送 `v*` 标签会在构建通过后自动创建 GitHub Release，包含两种架构的 `.tar.gz`、各自的 `.sha256`、汇总的 `SHA256SUMS` 和 `install.sh`：

```bash
git tag v0.1.0
git push origin v0.1.0
```

本地生成相同布局的包：

```bash
cd web && npm ci && npm run build && cd ..
bash scripts/package.sh amd64 v0.1.0
bash scripts/package.sh arm64 v0.1.0
```

包内包含 `bin/lnmp-gopanel`、`bin/lnmp-gopanel-control`、`deploy/`、`install.sh`、`README.md` 和 `VERSION`；本机配置、账号密码、依赖目录、测试截图和构建缓存不会提交或打包。

从当前源码升级本机时，先构建前端，再运行上述打包命令，最后使用项目中的安装脚本执行 `sudo bash install.sh --archive ./dist/lnmp-gopanel_linux_amd64.tar.gz`（arm64 使用对应文件名）。这样两个二进制、systemd 单元及 sudoers 会一起更新，并使用相同的备份、授权检查及失败回滚流程。定时任务、日志与虚拟主机仅检查 `cron list`、`logs list/read`、`vhosts list/config/format/usage/toggle/options/create/ssl-renew-preview/ssl-renew/basic-auth-get/basic-auth-set/basic-auth-disable` 的精确授权，不读取业务内容、不创建站点或申请证书；缺少可选组件或业务文件不会阻止安装。旧版 HTTP 安装非交互升级时需追加 `--tls-mode self-signed`，或指定现有证书。

## 构建

构建机需要 Go 1.26+、Node.js 22.12+、npm、make。目标机器为 Linux，运行时无需 Node.js。

```bash
make build
```

生成 `bin/lnmp-gopanel` 和 `bin/lnmp-gopanel-control`。生产面板使用 `-tags embed` 将前端嵌入 Go 二进制；服务辅助程序为独立二进制。

鼠标悬停或键盘聚焦侧栏用户头像（手机点击）可在带图标的提示中查看实际版本与构建时间。`make build` 和未指定版本的打包默认使用 `dev-<Git短提交号>`；`bash scripts/package.sh amd64 v0.1.0` 使用指定版本，并与包内 `VERSION` 保持一致。构建时间以 UTC 注入程序，页面固定显示北京时间（UTC+8）；可用 `VERSION`（Makefile）与 `BUILD_TIME`（格式 `YYYY-MM-DDTHH:MM:SSZ`）覆盖构建信息。CI 同一轮的两个架构共用构建时间。普通 `go run` 显示 `dev` 和未知构建时间。`GET /api/v1/build` 需要登录，返回 `version`、`built_at`。

生产页面由独立脚本检测 CSS／JS 资源更新：打开页面、重新聚焦、返回前台及每分钟检查一次最新入口，发现资源版本变化后自动刷新当前地址；旧脚本加载失败时也可恢复。网络失败或无效响应不触发刷新，同一目标版本每个标签页只自动刷新一次，避免代理缓存导致循环。HTML 不缓存，带哈希文件名的构建资源长期缓存。首次升级到支持此功能的版本时，已打开的旧页面需要先手动刷新一次。

桌面端可通过顶部左侧按钮隐藏或显示导航侧边栏，隐藏后主内容区扩展；当前浏览器会记住这一偏好。手机端继续使用抽屉导航，独立于桌面隐藏状态。

## 本地运行

从仓库根目录执行，首次初始化会提示输入密码（12–72 字节），检测本机服务，不执行服务启停：

```bash
./bin/lnmp-gopanel init --dir .
./bin/lnmp-gopanel --config ./config.yaml --services ./services.yaml
```

访问 https://127.0.0.1:8090 ，并信任初始化生成的自签名证书。默认用户名为 `admin`，密码为初始化时设置的值。CLI 也支持 `init --username NAME`、`--password-stdin`、`--listen HOST:PORT` 和 `--certificate-dir DIR`；端口设为 `0` 时自动选择空闲端口。请勿将密码直接写入命令历史。

如果开发容器只能以 root 运行，可临时添加 `--allow-root-for-development`；生产环境使用下面的普通用户部署。未安装辅助程序时仍可查看系统指标，服务状态显示未知，不会模拟服务数据。

前端开发：先运行后端，再在 `web` 目录执行 `npm ci && npm run dev`，访问 Vite 输出的本地 HTTPS 地址并信任证书；Vite 使用仓库根目录 `tls/` 中初始化生成的证书，`/api` 自动代理到 HTTPS 8090，使用 Secure Cookie 认证。普通 `go run ./cmd/panel` 从 `web/dist` 读取构建产物，不带生产嵌入标签。

## Linux 部署

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

## 配置与状态说明

`deploy/config.example.yaml`、`deploy/services.example.yaml` 展示所有配置字段。面板配置和辅助程序配置都应由 root 管理，普通 Web 用户仅有读取权限。服务配置修改后重启面板，使两侧读取一致。

- 系统采集每 2 秒一次，服务状态每 5 秒一次，磁盘容量每 30 秒一次。
- CPU、磁盘和网络速率需要两次采样；首次或计数器重置时返回 `null`，界面显示计算中或不可用。
- 网络为所有非回环网卡合计；有虚拟网桥、容器网卡的主机可能重复计算同一流量，不等同于外网带宽。
- Linux 磁盘 I/O 统计 `/sys/block` 中的整盘，排除分区、loop、ram、dm 层；复杂 RAID / 虚拟磁盘可能有重叠，应结合实际设备解释。
- 不开启 Nginx stub_status、PHP-FPM status 或额外数据库连接；第一版仅管理服务运行状态，进程存在不代表应用健康。
- LNMP 的 Nginx/PHP-FPM init 脚本可能不支持 `status`；这两类服务使用 `/proc` 中的完整可执行文件路径检查进程，避免将 usage 输出当作运行状态。systemd 服务使用对应 unit 状态；Nginx 额外核对真实主进程、MainPID 和进程组归属。实际进程运行在所配置 unit 之外时显示“管理状态异常”，页面和辅助程序均阻止启停、重启及重载，避免重复启动。
- Nginx 出现管理状态异常时，可在服务页选择“修复管理方式”，先预览再确认。仅支持标准 LNMP 路径、唯一且身份可验证的 root 主进程、可信 init 脚本，以及没有 MainPID 的 failed/inactive unit；自定义启动参数、其他服务所属进程、异常 PID 文件等情况需要通过 SSH 检查。修复会先检查 Nginx 配置，按需恢复缺失或空的 PID 记录，并将该服务切换为 init 管理，现有进程保持运行。预览绑定配置与进程启动时间，内容变化后必须重新预览。同一 Nginx 操作共用锁，失败时恢复原配置和 PID 文件内容、权限及组；修复前快照以 `0600` 保存到 `/etc/lnmp-gopanel/services.yaml.nginx-repair-<标识>.json`。成功后面板立即刷新管理方式，无需重启面板。升级时应同步更新两个二进制与 sudoers 中的 `nginx repair-preview`、`nginx repair` 精确授权；安装检查只验证授权，不执行修复。
- 修复回退会再次核对配置和进程身份；若期间有外部运维操作修改配置、PID 或替换主进程，会保留这些新状态并提示通过 SSH 检查，避免用旧快照覆盖管理员的新改动。
- 不支持或无法明确解释的状态返回“未知”；systemd 的切换状态单独显示，不误报停止。
- 服务操作最多执行 30 秒，同服务互斥；超时后重新检查实际状态，不自动重试启停。
- 会话最长 8 小时，重启面板会清空会话。登录每分钟最多 10 次，系统最多 32 条 SSE 连接；慢连接写入限时，单个订阅仅缓存最新一份快照。
- 必要运行和服务操作记录写入标准日志（systemd journal），不记录密码、令牌、指标样本或服务命令原始输出。`journalctl -u lnmp-gopanel` 可查看部署错误。

## 账户安全

左下角管理员区域的“账户安全”按钮可修改登录密码、绑定或关闭 TOTP，并重新生成恢复码。新密码至少 15 个 Unicode 字符、最多 72 个 UTF-8 字节，不含控制字符，且必须通过 zxcvbn 强度检测（最低 3 分）；支持空格和长口令，不要求固定字符组合。“随机生成强密码”在浏览器本地生成 24 位密码，可显示、隐藏和复制。修改密码需验证当前密码，启用 TOTP 后还需新的验证码或未使用的恢复码；成功后全部会话退出。已有密码仍可登录，初始化命令的原有 12–72 字节规则保持不变。

每个管理员支持一组 TOTP。输入当前密码后扫描二维码或手动录入密钥，再输入验证码确认启用；未确认的绑定在 5 分钟后失效，退出或取消也会清除。使用 SHA-1、6 位数字和 30 秒周期，允许前后各一个周期的时间偏差；服务器应保持时间同步。验证码成功使用后不能再次使用，需要等待下一组数字。启用后，密码正确只进入第二步，验证码或恢复码验证完成后才获得登录会话；登录挑战有效期 5 分钟，最多尝试 5 次。登录两个步骤合计每分钟最多 10 次；账户安全写操作有独立的每分钟 10 次限流。

启用及重新生成时一次展示 10 个恢复码，支持复制、下载，每个只能使用一次，服务端仅保存哈希。重新生成后所有旧码失效。启用、关闭 TOTP 或重新生成恢复码需重新确认身份，操作后撤销其他浏览器的会话并更新当前会话。恢复码也可代替验证码执行敏感操作；刚用于登录的恢复码不能再用于关闭 TOTP，请使用另一个未使用的恢复码。

认证状态由普通面板账号保存在 `auth.state_dir`：标准安装默认 `/var/lib/lnmp-gopanel`，自定义配置默认使用配置文件同级的 `auth-state/`。目录权限必须为 `0700`，`auth-state.json`、`auth.key` 和 `auth.lock` 为 `0600`，由面板账号所有，不能是链接。首次运行从 `config.yaml` 导入管理员及 bcrypt 哈希，此后以认证状态文件为准，修改密码不会回写 root 管理的配置。TOTP 密钥使用 AES-GCM 加密；备份与恢复时必须一起保存 `auth-state.json` 和 `auth.key`，不要分别覆盖或删除。状态或密钥部分缺失、损坏时拒绝启动，不会回退接受配置中的旧密码；写入失败时停止接受新登录，需要修复存储后重启。升级保留认证状态；改密或启用 TOTP 后，不要直接降级到忽略认证状态的旧版本。

验证器和恢复码全部丢失时，可通过服务器关闭 TOTP，登录密码保持不变。标准安装执行：

```bash
sudo systemctl stop lnmp-gopanel
sudo -u lnmp-panel /usr/local/bin/lnmp-gopanel auth disable-totp --config /etc/lnmp-gopanel/config.yaml
sudo systemctl start lnmp-gopanel
```

命令使用与面板相同的独占文件锁，运行中的面板会阻止离线修改。自定义部署以状态目录所属的服务账号执行，指定实际配置文件路径。此命令用于验证器丢失，不重置忘记的登录密码。

账户安全和登录日志端到端测试使用独立实例，避免修改凭据及真实登录限流影响其他测试。构建面板后，在 `web/` 分别执行 `npm run test:e2e:account` 和 `npm run test:e2e:login-logs`；原有浏览器测试继续使用 `npm run test:e2e`。

账户安全接口均位于 `/api/v1`。`GET /auth/security` 只返回 TOTP 状态、剩余恢复码数和密码规则；`POST /auth/password` 接收 `current_password`、`new_password`，启用 TOTP 后再提供 `code` 或 `recovery_code`，成功返回 204 并撤销会话。`POST /auth/totp/setup` 接收 `current_password`，返回 `setup_id`、`secret`、本地二维码 `qr_code` 和 `expires_at`；`POST /auth/totp/confirm` 接收 `setup_id`、`code`，确认启用；`DELETE /auth/totp/setup` 取消未完成的绑定。`DELETE /auth/totp` 和 `POST /auth/totp/recovery-codes` 接收当前密码及二次验证凭据。启用、关闭和重新生成响应包含更新后的 `session`，生成恢复码时额外返回仅此一次的 `recovery_codes`。这些接口需要有效会话，写操作要求 CSRF 和同源校验，参数错误返回 400，重新确认身份失败返回 403，状态过期或冲突返回 409，存储错误返回 503。

`POST /auth/login` 未开启 TOTP 时保留原有响应；开启时返回 `totp_required`、`challenge_token`、`expires_in`，不签发 Cookie。随后向 `POST /auth/login/totp` 发送 `challenge_token` 及 `code` 或 `recovery_code` 完成登录。登录两步无需已有会话及 CSRF，但均校验来源并限流。仅密码校验失败时，原始输入的密码会写入专用登录日志；成功验证的密码、TOTP 密钥、二维码、验证码和恢复码不会写入日志，认证凭据不写入浏览器持久存储。

## API

除登录两步外，所有数据接口要求 Cookie 会话。响应禁止缓存；写操作要求 `X-CSRF-Token`，其值从登录响应或当前会话获得。跨源 Origin 被拒绝，不开启 CORS。

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/v1/auth/login` | JSON `{ "username": "admin", "password": "…" }` |
| GET | `/api/v1/auth/session` | 用户名、CSRF 令牌、到期时间 |
| POST | `/api/v1/auth/logout` | 清除当前会话 |
| GET | `/api/v1/host` | 主机静态信息 |
| GET | `/api/v1/metrics` | 最新指标快照，首次采集前返回 503 |
| GET | `/api/v1/metrics/stream` | SSE：`connected`、`metrics`、`heartbeat`、`auth_expired` |
| GET | `/api/v1/services` | 服务状态、可用动作、检查时间与忙碌状态 |
| GET | `/api/v1/services/releases` | 立即返回服务及编程组件的版本查询缓存，首次或过期时后台联网查询 |
| GET | `/api/v1/components` | 异步读取本机 Python 3、Go、Node.js、Codex CLI、Composer 版本 |
| POST | `/api/v1/components/refresh` | 手动刷新本机编程组件版本（需要 CSRF，返回 202） |
| POST | `/api/v1/services/releases/refresh` | 手动异步检查最新稳定版本（需要 CSRF，返回 202） |
| GET | `/api/v1/docker` | Docker 只读快照，首次及过期时后台刷新 |
| POST | `/api/v1/docker/refresh` | 手动刷新 Docker 快照（需要 CSRF，返回 202） |
| POST | `/api/v1/services/:name/actions/:action` | 服务操作，无请求体 |

快照包含 `timestamp`、`cpu`、`memory`、`swap`、`load`、`uptime`、`network`、`disk_io`、`disks` 和 `errors`。容量单位为字节、速率为字节/秒、使用率为 0–100，时间为 RFC 3339。无法采集的字段为 `null`，详细信息在 `errors` 中；接口错误统一为 `{ "error": "说明" }`。

## 验证

```bash
make test
make check
# 可选浏览器测试：先 make build，再安装 Chromium 与所需系统库
cd web
npx playwright install --with-deps chromium
npm run test:e2e
```

Go 测试覆盖指标差分、采集与推送、认证、CSRF、SSE、服务白名单与互斥、操作超时和 Nginx 重载前检查。前端测试覆盖曲线内存上限、窗口裁剪、断档与重复数据。Go 服务操作测试使用替身执行器；浏览器测试在临时目录创建独立账号配置，禁用真实服务清单，服务弹窗测试拦截 API，不会启停真实服务。生产服务脚本仍应在对应 LNMP 测试机上验证。

## Docker 只读概览

左侧“Docker 管理”页面展示与 `docker ps --size --no-trunc` 对应的运行中容器信息：名称、容器 ID、镜像、启动命令、创建时间、运行状态、端口、可写层大小和虚拟大小；不展示已停止的容器。空间汇总来自 `docker system df`，包含镜像、容器可写层、本地数据卷及构建缓存的数量、使用中数量、占用空间和可回收空间。

容器可写层不包含日志和挂载目录，数据卷在空间汇总中单独列出；虚拟大小包含可能共享的镜像层，不能按容器直接相加，因此本页不把这些值相加作为 Docker 的实际总磁盘占用。

数据在打开页面后后台采集，多个客户端共享 30 秒缓存和正在执行的查询，手动刷新至少间隔 5 秒；每条 Docker 命令最多 12 秒。容器列表或空间统计单独失败时保留对应的旧数据并显示错误。Docker 未安装、服务未启动或权限未配置时会显示状态说明。

辅助程序仅接受固定的 `docker snapshot`，内部只执行上述两条只读命令，并固定连接本机 `/var/run/docker.sock`；面板账号不加入 docker 组，不接受任意 Docker 参数，也不提供容器启停、exec 或清理功能。升级需同步安装面板、辅助程序和 `deploy/lnmp-gopanel.sudoers`（新增精确的 `docker snapshot` 权限，先通过 `visudo -cf` 验证），然后重启面板，无需重启 Docker。

## 编程组件

“LNMP 服务”页面的“编程组件”分组固定展示 Python 3、Go、Node.js、Codex CLI、Composer 的本机版本、可执行路径、检查时间、最新正式版本及对应 Changelog。即使未配置任何 LNMP 服务，也可查看组件信息。仅查询版本，不提供安装、升级或启停操作。

本机版本通过固定的 `components snapshot` 辅助命令读取，检查程序及其父目录的 root 所有权和写权限，不加载 shell 配置。Python、Go 检查标准系统安装目录；Node.js、Codex 优先选择 `/root/.nvm/alias/default` 指定的已安装版本，再检查系统安装。支持数字版本前缀、`node`、`stable`、`lts/*` 和 LTS 名称别名；不跟踪临时终端的 `nvm use`，不扫描其他用户目录。Codex 的 npm 启动脚本使用安装目录中的 Node。Composer 检查 `/usr/local/bin/composer`、`/usr/bin/composer`、`/usr/local/php/bin/composer`，通过受信任的 PHP CLI（优先 `/usr/local/php/bin/php`，其次 `/usr/local/bin/php`、`/usr/bin/php`）仅读取版本，并禁用插件、脚本和交互；不扫描网站内的项目级 Composer。未安装和读取失败分别展示，读取失败保留上次成功版本。

采集异步执行，每个程序最多 8 秒，结果缓存 1 分钟，手动刷新间隔至少 5 秒。最新版本排除预发布，Node.js 按最高正式版本查询，可能高于 LTS 分支；Python 使用 python.org 发布列表与 CPython 对应标签的 NEWS，Go 使用 go.dev 下载接口及发布历史，Node.js 使用官方版本索引与对应标签 Changelog，Codex CLI 和 Composer 分别使用各自官方仓库的正式 GitHub Release。联网查询与服务共用缓存、限流和失败处理。

已有安装需要同步更新面板、辅助程序和 sudoers，新增精确授权 `lnmp-gopanel-control components snapshot`；安装脚本会验证该权限，并以面板账号执行只读组件自检；工具未安装不阻止安装，辅助程序执行失败则回滚升级。无需修改 `services.yaml` 或放宽 `/root` 目录权限。

Composer 检测沿用 `components snapshot` 授权，升级时使用同一源码打包的面板与辅助程序即可，无需新增 sudoers 规则。安装脚本不会安装 Composer 或 PHP；未安装 Composer 显示“未安装”，已安装 Composer 但缺少 PHP CLI 显示读取失败，不影响面板安装和其他组件检测。

## 服务版本与 Redis 监控

服务卡片展示 Nginx、PHP-FPM、MySQL / MariaDB、Redis 的已安装版本。版本号通过固定的服务可执行文件及 `-v` / `--version` 参数读取，由面板普通用户执行，不连接数据库、不读取凭据；本机版本成功缓存 1 小时，失败后 1 分钟重试，失败不覆盖上次版本或服务运行状态。

进入“LNMP 服务”页面后，后台独立查询每个已配置服务的最新正式版本。Nginx 选择官方 Stable 分支（不包括 Mainline），PHP 使用 php.net 发布接口与 php-src 的 NEWS，MariaDB 使用官方发布接口与 changelog，MySQL 使用官方源码仓库的正式标签及官方 Release Notes，Redis 使用官方 GitHub Releases。这里的“最新稳定版”可能跨越本机版本分支，不表示当前安装分支的补丁更新建议。

联网查询由普通面板进程执行，单次请求最多 12 秒，每个服务整体最多 30 秒，只访问固定官方 HTTPS 域名，并限制响应大小与重定向。服务之间并行，状态 API 不等待联网；多个客户端共享内存缓存和正在执行的查询，成功缓存 6 小时，失败或 changelog 不可用时 15 分钟后自动重试；手动检查至少间隔 1 分钟。版本查询失败保留上次结果；changelog 失败仍展示最新版本与官方原文链接。上游限流、网络故障或 HTTP 403 会明确显示原因，不生成替代更新日志。更新日志以纯文本显示，长日志截断并提供完整原文链接，不执行上游 HTML。

首次初始化自动识别 `redis.service` / `redis-server.service` 或 `/etc/init.d/redis` / `/etc/init.d/redis-server`。已有安装升级时，需要在 `/etc/lnmp-gopanel/services.yaml` 中增加实际对应的 Redis 配置，例如：

```yaml
  - name: redis
    manager: systemd
    program: /usr/bin/systemctl
    target: redis.service
```

Redis 只显示状态和版本，不提供启停按钮，辅助程序也拒绝 Redis 修改操作。升级时同步安装新面板、辅助程序和 `deploy/lnmp-gopanel.sudoers`（新增精确的 `redis status` 权限，先执行 `visudo -cf` 验证），然后重启面板。其余服务无需增加版本查询的 sudo 权限。

## 硬盘空间与大文件排行

左侧“硬盘空间”页面列出全部挂载，包括物理磁盘、内存、虚拟文件系统和绑定挂载；不支持容量的挂载仍显示，标记为“不提供容量”。列表每 30 秒刷新，可筛选挂载点、设备和文件系统类型，并可隐藏无容量或容量不可读取的挂载。点击表头可按挂载点、设备、文件系统、已用空间、总容量、可用空间或使用率升降序排序；容量使用原始数值比较，无容量及读取失败项始终排在数值项之后，零使用量仍参与排序。筛选及刷新保留当前排序。

进入页面后异步扫描主文件系统 `/`，每次仅保留最大的 30 个普通文件，按逻辑大小排序（与 `find -type f -printf '%s %p'` 的大小定义一致）。使用 NUL 分隔记录保留含空格、制表符和换行的路径；不读取文件内容、不跟随符号链接，并跳过其他挂载点，包括同设备的绑定挂载。硬链接按路径计数，稀疏文件显示逻辑大小，所以排行总和不代表实际占用的磁盘块数。

矩形树图的面积为文件大小占本次 Top 30 总大小的比例，不是占整盘容量的比例；旁边同时显示路径、文件大小和百分比。扫描以低 CPU 优先级运行，最多 5 分钟；多个浏览器共享同一进行中的任务，切换页面不会停止扫描，可点击“重新扫描”更新。进度和最新结果仅存在后端内存中，不记录历史，面板重启后清空；超时、文件消失或读取失败会标记部分结果。

新增接口：`GET /api/v1/storage/mounts`、`GET /api/v1/storage/scan`、`POST /api/v1/storage/scan`（需要 CSRF，立即返回 202）。所有接口均需登录。扫描通过固定辅助程序参数 `storage scan` 执行，不接受用户输入的路径或命令。升级部署时应同步更新两个二进制和 sudoers 模板，新规则为 `/usr/local/libexec/lnmp-gopanel-control storage scan`。

## 虚拟主机

“虚拟主机”页面按 `/usr/local/nginx/conf/vhost` 中的配置文件展示站点：域名、监听地址、反向代理目标、目录占用、SSL 证书签发者、主体、域名、有效期、剩余天数、序列号及 SHA-256 指纹。可查看原始配置，并手动格式化缩进后保存。`.conf` 表示启用，`.conf.disabled` 表示停用；同文件的多个 server 块作为一项配置一起管理。

侧栏“虚拟主机”显示配置总数，包含已启用及已停用的站点，和“LNMP 服务”使用相同数量角标。登录后通过需鉴权的 `GET /api/v1/vhosts/count` 查询，每 30 秒刷新，读取失败时显示“—”或上次成功结果，不误报为零。统计只读取站点元数据、不触发目录占用扫描，多浏览器共享 30 秒缓存；站点列表刷新成功后立即同步角标，搜索筛选不改变总数。

配置解析支持注释、引号、多行指令及静态 include（相对于 Nginx conf 目录，设有深度与读取上限）。动态 root 或证书路径显示无法静态计算，不执行变量或脚本。SSL 信息来自已配置的本地 PEM 叶证书，时间状态不代表证书链信任、域名匹配或在线吊销校验；证书信息解析不读取 `ssl_certificate_key` 指向的私钥文件。无显式 root 的反向代理显示无本地目录。

目录占用在站点元数据加载后异步计算，使用 `du -s -x -B1` 的实际磁盘块大小。root 路径最后一级为 `public` 时统计其上一级目录；相同目录去重计算，符号链接部署目录先解析实际目标。单目录最多 45 秒，总任务最多 5 分钟，结果只保留内存。站点配置和证书可通过“刷新站点”重新读取。

启停时只对普通配置文件原地重命名，保留原文件内容与权限；拒绝目录穿越、符号链接、同名文件冲突及覆盖目标。重命名后执行 `nginx -t`，通过才发出 `nginx -s reload`。检查失败恢复原文件名；重载失败恢复原文件名并尝试重新加载原配置，失败结果明确提示。所有面板触发的 Nginx 修改与站点启停共享进程间锁。

新增接口：`GET /api/v1/vhosts`、`POST /api/v1/vhosts/refresh`（202，异步刷新）、`GET /api/v1/vhosts/config?name=example.conf`、`POST /api/v1/vhosts/toggle`（JSON `{"name":"example.conf","enabled":false}`）。全部需要登录，POST 需要 CSRF。特权操作仅开放固定参数 `vhosts list/config/format/usage/toggle`，文件名通过有界 JSON stdin 传递；辅助程序重新验证文件名与目录权限。升级时同步安装两个二进制和 sudoers 模板。

虚拟主机卡片和表格的“访问认证”可开启、读取、关闭 Basic Auth 或替换用户名及密码。每个 vhost 一组凭证，同文件全部 server 共用；证书验证路径 `/.well-known/acme-challenge/` 免认证，SSL 重试会保留认证。停用站点只显示保存的配置状态，启用后才能修改。已有手写认证、共享 include 中的认证或 `satisfy any` 显示“外部配置”，不覆盖；动态 include、Lua 执行指令、提前返回成功响应的 `return 2xx`、异常管理标记、凭证缺失或无法确认继承关系时显示“无法确认”，禁止修改。状态来自磁盘配置检查，不代表已对公网地址执行认证探测。

认证接口：`GET /api/v1/vhosts/basic-auth?name=example.conf` 读取状态；`PUT /api/v1/vhosts/basic-auth` 使用 JSON `{"name":"example.conf","username":"visitor","password":"新密码"}` 开启或完整替换凭证；`DELETE /api/v1/vhosts/basic-auth?name=example.conf` 关闭，重复关闭成功。均需登录，写操作需要 CSRF，JSON 上限 4 KiB。状态返回 `name`、`status`（`disabled/enabled/external/unknown`）、`managed`、`site_enabled`，以及可选 `username` 和 `message`，不返回密码及哈希。用户名为 1–64 位字母、数字、点、下划线或连字符；密码为 8–256 字节，不含控制字符。

辅助程序只接受固定 `vhosts basic-auth-get/set/disable`，凭证通过 JSON stdin 传递。需要可信 `/usr/bin/openssl` 生成带随机盐的 SHA-512 crypt 哈希；只将用户名和哈希保存在 `/usr/local/nginx/conf/gopanel-auth/`，目录 `0755`、文件 `0644`，均由 root 管理且不可被其他用户写入，供 Nginx worker 读取。每次更新使用独立凭证文件，原凭证文件保留供平滑退出的旧 worker 完成请求，不复用或改写。配置检查或重载失败会恢复原配置，确认回滚成功后未被引用的新凭证会清理；关闭认证精确移除面板插入的配置片段，保留原配置内容和权限。卸载面板保留网站认证及凭证文件。升级需同步安装面板、辅助程序和 sudoers；安装仅检查新增命令的精确授权，不读取或修改认证。

### 添加站点

“添加站点”支持主域名与附加域名、站点目录、PHP／纯静态、Pathinfo、现有伪静态模板、IPv6 和访问日志，可选创建数据库与同名本机用户，不创建 FTP 账号。默认目录是 `/home/wwwroot/<主域名>`；已有目录不递归修改权限，也不覆盖已有 `.user.ini`。新站点访问日志保存在 `/home/wwwlogs/`，目录与文件沿用 root 所有权、禁止其他用户写入及禁止符号链接的检查，可在日志页面查看；原有日志路径不变。数据库密码可点击“随机生成强密码”生成 24 位、包含大小写字母、数字及符号的密码，并可显示或隐藏；密码仅保留在当前表单，不存入浏览器存储。数据库优先使用 root 配置或 Unix socket 认证，不可用时填写管理员密码；凭据仅用于本次操作。已有数据库、账号、域名绑定和同名配置不会被覆盖。

SSL 支持申请 Let's Encrypt／ZeroSSL、填写 cert/key 绝对路径，或从 `/root/.acme.sh/`、`/usr/local/acme.sh/` 扫描到的通配符证书中选择。证书会检查有效期、私钥匹配和全部站点域名的覆盖范围；通配符只匹配一级子域名，根域名需另含 SAN。已有证书直接引用原文件，沿用原续期与 Nginx 加载设置，本功能不额外接管。启用 SSL 后默认强制 HTTPS，可关闭；HTTP 证书验证路径始终保留。

Go 辅助程序生成配置并检查、重载 Nginx，配置或重载失败会恢复原配置，不调用版本相关的 `lnmp vhost add`／`lnmp ssl`。自动申请由 Go 依次检测 `/root/.acme.sh/acme.sh`、`/usr/local/acme.sh/acme.sh`，使用首个可执行且由 root 所有、程序及父目录均不可被组或其他用户写入的安装；两个路径同时有效时优先前者。要求安装可信的 `/usr/bin/flock` 和运行中的系统 cron；使用 webroot HTTP-01 验证，所有域名需能从公网 80 端口访问本站。申请不支持通配符或 DNS API。新证书使用所选安装目录下独立的 `gopanel-certs/`，申请、部署和每日续期使用同一安装目录与证书目录，不改写已有证书配置。证书通过 `--install-cert` 部署到 `/usr/local/nginx/conf/ssl/gopanel/`，每日续期任务保存在 `/etc/cron.d/lnmp-gopanel-acme`，续期重载使用与面板共享的 Nginx 锁并先检查配置。续期独立于面板运行，卸载面板仍保留网站证书和续期任务。不会扫描备份目录来执行 acme.sh；缺少客户端或权限不安全与缺少 flock 会分别提示。

“校验解析”与主域名、附加域名输入框同排，逐域名查询 A／AAAA，与本机网卡和公网 IPv4／IPv6 对比。公网探测通过 HTTPS 访问 `api.ipify.org` 和 `api6.ipify.org`，结果缓存五分钟；失败时显示“无法确认”。公网探测警告及解析校验说明收在问号图标的提示中，支持悬停、键盘聚焦和点击。NAT 环境可在配置中添加 `vhosts.public_ips: ["服务器公网 IPv4", "服务器公网 IPv6"]` 补充参考地址。CDN、代理、混合解析或暂未生效只作提示，不阻止建站与证书申请。

创建后可实时查看证书申请输出。关闭窗口不取消任务，刷新页面可打开“查看建站进度”或“最近建站结果”；任务最长 20 分钟，在内存中保留最近 20 个任务，每个任务最多 1000 行日志，敏感输出会隐藏。面板重启不自动重放任务，请刷新站点确认实际状态。数据库创建失败只清理本次新增数据库资源；SSL 失败保留 HTTP 站点，可单独重试 SSL，不重复创建网站或数据库。重试只操作本功能创建且配置未被外部修改的启用站点。

新增认证接口：`GET /api/v1/vhosts/options`、`POST /api/v1/vhosts/check-dns`、`POST /api/v1/vhosts`、`GET /api/v1/vhosts/jobs`、`GET /api/v1/vhosts/jobs/:id`、`GET /api/v1/vhosts/jobs/:id/events` 和 `POST /api/v1/vhosts/ssl/retry`。POST 需要 CSRF，建站相关 JSON 请求上限 32 KiB；创建和重试返回 202 与任务 ID，重复提交相同 ID 不重复执行。升级需同步安装两个二进制与 sudoers，新增固定 `vhosts options` 和 `vhosts create` 授权；安装只检查授权，不创建站点或申请证书。

### 批量提前续签 SSL

表格视图支持逐行多选、全选当前筛选结果和清空选择。点击“提前续签 SSL”，为每个站点分别选择 ZeroSSL 或 Let's Encrypt 并填写联系邮箱；机构默认匹配当前证书，无法识别时使用 Let's Encrypt，邮箱优先读取已有面板记录。确认列表显示原签发者、到期时间、本次签发域名和无法续签的原因。

支持原有 LNMP 站点及面板创建的已启用 SSL 站点，不要求存在建站记录或配置保持原样。首版使用 HTTP-01 验证，要求全部证书域名绑定在该配置且可从公网 80 端口访问。通配符、多组证书、动态证书引用、关键配置位于共享 include、无法安全调整的服务器级重写，以及不可读取的证书会显示原因并禁止提交；停用站点需先启用。只修改验证入口和证书引用，保留 PHP、代理、重写及访问认证。

队列在后端逐站执行，每站最长 20 分钟，单站失败后继续。强制重新签发，不以旧证书尚未到期作为成功。新证书使用独立版本目录暂存，检查私钥、有效期和完整域名覆盖后切换配置；Nginx 检查、重载或续期登记失败时恢复原配置，遇到外部并发修改则停止覆盖并在输出中说明。原证书文件保留。成功后每站使用独立 `/etc/cron.d/lnmp-gopanel-acme-<站点哈希>` 任务续期，与原有证书续期目录隔离；续期不依赖面板运行。

任务窗口实时显示每站状态、阶段、完成数量及带站点名称的签发输出。SSE 断线时每两秒轮询，关闭窗口或刷新页面后可通过“查看续签进度”重新打开；完成后可只重试失败项并保留其机构和邮箱。与建站任务共用执行互斥、最近 20 个任务及最近 1000 行脱敏日志的内存记录，面板进程重启后不自动恢复队列，应刷新站点确认实际状态。

新增接口：`POST /api/v1/vhosts/ssl/renew/preview` 接收 `{"names":["example.conf"]}`，返回每站可续签状态与默认参数；`POST /api/v1/vhosts/ssl/renew` 接收 `{"request_id":"唯一请求标识","items":[{"name":"example.conf","provider":"zerossl","email":"admin@example.com"}]}`，返回 202 与批量任务。两者均需登录及 CSRF，请求体上限 1 MiB，最多 1000 个不重复站点；辅助程序仅逐站执行。进度复用现有任务查询和 SSE 接口，批量任务的 `kind` 为 `ssl-renew`，增加 `items`、`completed`、`succeeded`、`failed`，日志增加 `name`。升级需同步安装两个二进制和 sudoers，新增 `vhosts ssl-renew-preview`、`vhosts ssl-renew` 精确授权；安装只检查授权，不实际续签。

### 网站截图

虚拟主机支持卡片和表格视图，两种视图均展示网站预览。通过“截图设置”填写 HTTP/HTTPS 地址，可启用 Basic Auth，并按站点指定浏览器语言（默认 `zh-CN`，例如 `en-US`）和最长加载时间（默认 30 秒，可设为 5–300 秒）。语言同时用于请求语言、浏览器语言及日期、数字格式。截图分辨率可选 1920×1080、1440×900（默认）、1366×768、768×1024、390×844，并提供 1920×1200、2560×1600、2880×1800、3072×1920、3200×2000、3840×2400 六个 16:10 高分辨率预设；也可自定义宽高，均须为 320–3840 的整数。网页缩放默认 100%，提供常用比例，也可自定义 25%–200% 的整数百分比。缩小比例可显示更多内容，响应式布局随可见范围变化，输出图片尺寸始终保持所选分辨率。手机和平板尺寸不切换移动端 UA 或启用触摸模拟。保存后后台截取网站首屏，之后手动“重新截图”，点击图片可放大；没有定时截图、全页截图或历史图库。支持公网、本机和内网地址，浏览器访问的是服务器可达的地址，不使用管理员浏览器中的登录状态。页面加载后等待资源请求结束并连续稳定 2 秒、字体就绪后再截图；CSS、图片等加载较慢时可增加最长加载时间，超时显示原因并保留上次成功截图。持续轮询的页面可能需要更长时间才能稳定。HTTPS 证书错误、HTTP 错误和认证失败会显示原因，保留上次成功截图。

可选填写“截图前 JavaScript”（最多 16 KiB，按 UTF-8 字节计算），在目标网站主框架中执行，用于关闭弹窗、点击元素或调整页面。代码在页面正文、资源和字体就绪后执行一次，支持直接使用 `await`，执行结果不回传；完成后再次等待资源稳定、字体及渲染就绪再截图。执行也计入该站点的最长加载时间，脚本语法错误、执行异常、Promise 拒绝或超时会显示原因，并保留上次成功截图。分辨率、缩放比例和脚本随站点保存，重启后保留，重新截图时复用；脚本留空不执行，旧设置默认使用 1440×900 和 100% 缩放。例如：

```javascript
document.querySelector(".popup")?.remove();
document.querySelector(".expand-button")?.click();
await new Promise(resolve => setTimeout(resolve, 500));
```

截图由 Go 直接控制本机 Chromium，无需生产环境安装 Node.js，不通过 sudo 辅助程序启动浏览器。浏览器必须以普通面板账号运行，保留沙箱，每次任务使用独立临时配置，完成或取消后清理。全局一次执行一个任务、最多排队 64 项，单次任务（含启动浏览器和资源等待）受该站点的最长加载时间限制，排队时间不计入。Basic Auth 只发送给配置地址同源的认证挑战，跨源跳转不会携带凭据；如需在跳转后的站点认证，请直接填写最终地址。密码不回传，编辑时留空保留原密码，更换目标来源或用户名时需重新填写；关闭认证会清除凭据，“清除设置”会同时删除保存的图片。

可选配置：

```yaml
screenshots:
  browser: /usr/bin/chromium # 留空时在 PATH 中检测 Chromium/Chrome
  state_dir: /var/lib/lnmp-gopanel
```

标准部署的状态目录为 `/var/lib/lnmp-gopanel`，由安装脚本和 systemd 为 `lnmp-panel` 创建；自定义配置文件默认在其同级 `screenshots-state/` 保存状态，也可以显式指定目录。目录必须由面板账号所有、权限 `0700`，状态文件和图片权限为 `0600`。密码使用 AES-GCM 加密保存，`screenshots.key` 与 `screenshot-settings.json` 必须一起备份、恢复，密钥丢失不会自动覆盖重建。设置与最后成功截图在升级及重启后保留；它们不会写入 Nginx 配置或打包进 Release。缺失浏览器或状态目录不可用时，其他功能继续工作。

部署后可用 `sudo -u lnmp-panel /usr/local/bin/lnmp-gopanel check-screenshots` 检查截图组件，仅访问临时本地测试页面，不访问业务站点。源码集成测试使用 `SCREENSHOT_TEST_BROWSER=/path/to/chromium go test -race ./internal/screenshots -run TestChromiumIntegration`，所有目标都是本地测试服务器；仅隔离的 root 测试进程会关闭浏览器沙箱，生产代码不提供该开关。

新增接口：`GET /api/v1/vhosts/screenshots`、`PUT /api/v1/vhosts/screenshot?name=…`、`POST /api/v1/vhosts/screenshot/refresh?name=…`、`DELETE /api/v1/vhosts/screenshot?name=…`、`GET /api/v1/vhosts/screenshot/image?name=…`。均需登录，写操作需 CSRF；设置与刷新返回 202，图片通过面板鉴权后读取且禁止缓存。设置输入及返回数据包含 `viewport_width`、`viewport_height`、`zoom_percent`、`javascript`；宽高同时省略或均为 0 时使用默认尺寸，只提供一项非零尺寸会报错。`zoom_percent` 为 25–200 的整数，省略时默认 100，显式传入 0 或超出范围会报错。截图设置 PUT 请求体上限为 128 KiB，以容纳脚本和 JSON 转义，建站相关 POST 为 32 KiB，批量 SSL 续签及预检查 POST 为 1 MiB，其他 API 为 4 KiB。

### 定时任务

左侧“定时任务”页面只读展示 root 用户的 crontab，包含执行周期、执行命令、原始行号、任务数量和最近读取时间，可手动刷新并展开查看原文。支持五字段 Cron 表达式及 `@reboot` 等特殊周期；环境变量、注释和空行不计为任务，未识别内容会显示提示并保留原文。此页面不包含其他用户、`/etc/crontab`、系统 Cron 目录或 systemd timer，也不提供编辑或立即执行功能。

接口 `GET /api/v1/cron` 需要登录，返回 `tasks`、`raw`、`warnings` 和 `updated_at`。读取失败会明确提示，刷新失败时保留上次成功的数据并标记可能过期；没有 root crontab 时显示空列表。辅助程序仅接受固定的 `cron list`，在可信路径下执行 `crontab -u root -l`，有超时及输出大小限制，不接受用户输入的参数。

升级时同步更新面板、辅助程序和 `deploy/lnmp-gopanel.sudoers`，新增精确授权 `/usr/local/libexec/lnmp-gopanel-control cron list`，先通过 `visudo -cf` 验证再安装并重启面板。安装脚本会检查该只读操作的 sudo 授权；未安装 crontab 不影响安装，页面会提示无法读取。

定时任务表达式支持鼠标悬停、键盘聚焦或手机点击预览之后 5 次预计执行时间。预览接口 `GET /api/v1/cron/preview?line=行号` 需要登录，以服务器当前时间重新读取并计算，默认使用服务器时区，任务前的 `CRON_TZ` 声明可覆盖时区；每个时间保留时区偏移。`@reboot`、无效表达式、无效时区和没有匹配日期时会显示说明。预览不检查 Cron 守护进程是否运行，不代表任务已执行或执行成功。原文展开后按需加载 Prism，对周期、环境变量、注释和 Shell 语法高亮，保留原始文本并由 React 安全渲染。此更新沿用 `cron list` 授权，安装脚本无需新增权限。

### 日志读取

左侧“日志读取”页面自动列出 `/home/wwwlogs`、`/usr/local/nginx/logs/gopanel` 直属目录中的 `.log` 普通文件，以及 `/var/log/mysql/mariadb-slow.log`、`/usr/local/php/var/log/php_errors.log`。在“日志内容”卡片顶部选择日志，卡片显示路径、大小和修改时间；不递归发现子目录，不自动列出轮转归档，不跟随符号链接，不提供编辑、清空和下载功能。文件不存在不影响安装。

文件列表还会自动列出“登录日志”，实际文件为 `logs.state_dir/login.log`（标准安装为 `/var/lib/lnmp-gopanel/login.log`），不可从列表移除。每行是一个 JSON 对象，包含带时区的时间 `time`、客户端 IP `ip`、User-Agent `ua`、用户名 `username`、结果 `result`（`success` / `failure`）、阶段 `stage`（`password` / `totp`）及原因 `reason`。成功与失败均记录；仅在密码校验失败时额外保存原始输入的 `password`（包括空密码），用户名错误但密码正确、二步验证失败和成功登录均不保存密码，验证码、恢复码及挑战令牌也不会写入。启用二步验证后，完成验证并创建会话才记录成功，进入待验证状态不记为成功。二步验证事件使用当前唯一管理员用户名；参数无效、限流及认证存储错误仅记录可获得的元信息，无法取得的用户名为空，请求来源校验拒绝不写入此文件。IP 沿用直接连接地址，不信任未经配置的代理请求头。

登录日志由面板账号直接读写，目录权限为 `0700`、文件为 `0600`，拒绝符号链接、硬链接和特殊文件。页面复用原文、筛选、历史加载和实时追踪；文件元数据可包含 `name` 显示名称。仅这一指定文件由专用入口读取，其他状态文件仍禁止读取，不增加 sudo 授权。系统日志辅助程序不可用时仍可读取登录日志，并显示其他日志的读取错误。初始化或写入失败会提示错误，不改变认证结果；重启保留记录，本功能不自动轮转日志。

登录日志默认显示 JSON 原文，可勾选“格式化为表格”，依次展示字节偏移、时间、客户端 IP、IP 属地、用户名、结果、验证阶段、原因、失败密码及 User-Agent，并显示可识别的 UA 图标。时间按浏览器本地时区显示，悬停可查看原始带时区时间；已知结果、阶段及原因使用中文，未知值保留原文。没有记录密码时显示“—”，记录了空密码时显示“（空密码）”。无法解析的行保留原文及说明，截断、未换行提示仍可见。表格支持容器内横向滚动，沿用历史加载、实时追踪和关键词／正则筛选，切换文件后恢复原文模式；HTTP 状态码筛选只用于 Nginx 日志。

登录日志文件元数据包含 `format: "login"`，不依赖文件路径或显示名称识别。读取时从 JSON 的 `ip` 字段提取地址，使用下述纯真数据库离线补充 `ip_location`，同批重复 IP 复用查询结果。IPv6 显示“暂不支持”，无效地址及查询失败显示“未知”；原文件不修改，历史记录同样可展示属地。

点击“添加日志”，输入服务器上已有普通文本日志的绝对路径，例如 `/var/log/syslog`、`/opt/app/logs/server.log`；不要求 `.log` 扩展名，也可显式添加轮转后的普通文本文件。添加前由辅助程序检查文件，成功后自动选中读取。最多保存 64 个自定义路径，刷新、重新登录和面板重启后保留；文件暂时消失或不可读时保留条目并显示错误。“移除日志”只移除自定义列表条目，不删除或修改原文件，内置日志不能移除。

系统配置（`/etc`）、`/root`、`/proc`、`/sys`、`/dev`、`/run`、`/boot`、备份和面板状态目录、常见 MySQL／MariaDB／PostgreSQL／Redis 数据目录，以及 Nginx／PHP 等组件的配置目录禁止读取。任意路径中的 `.ssh`、`.aws`、`.kube`、`.docker`、`.gnupg`、`.git` 等凭据或版本库目录，`.env*`、常见密码配置、私钥证书和数据库文件也会拒绝。路径必须规范，不能包含 `..`、重复分隔符、控制字符或反斜杠。添加及每次读取都重新校验，逐段禁止符号链接，并拒绝硬链接、特殊文件、包含 NUL 或私钥头的文件；格式检查仅检查前 4 KiB，不对应用日志中可能记录的敏感业务内容进行脱敏。

自定义路径存于 `logs.state_dir` 下的 `custom-logs.json`，标准安装默认为 `/var/lib/lnmp-gopanel`；自定义配置文件默认使用同级 `logs-state/`。目录必须属于面板账号且权限为 `0700`，列表文件为 `0600`，采用原子替换保存；存储不可用时内置日志继续可用并显示提示。备份该文件即可保留自定义列表，升级安装不会覆盖它。

默认定位到文件末尾，约每秒增量读取，可暂停追踪、自动滚动、回到最新或加载更早日志。向上滚动会关闭自动滚动；加载历史会暂停追踪。后台标签页暂停轮询，返回后从游标继续读取。单批原始范围最多 256 KiB、最多返回 4000 行；页面缓存最多 2 MiB、20000 行和 64 批，超限会释放浏览方向另一端，可重新加载。日志使用虚拟列表显示，避免整文件读取和大量 DOM。文件轮转或截断后提示并重新读取最新文件，不混合不同版本的历史。

点击“日志内容”旁的“筛选规则”，在右侧抽屉中编辑并应用规则；关闭抽屉保留未应用的编辑，应用成功后自动收起。可添加最多 20 条关键词或 Go 正则规则，逐行选择“显示匹配”或“隐藏匹配”，支持启停及忽略大小写，默认区分大小写。存在显示规则时命中任意一条即可，隐藏规则优先；没有显示规则时默认显示全部。点击“应用规则”后，仅重新筛选已加载范围，不搜索整个文件；慢 SQL 和错误堆栈也逐行处理。规则按文件保存在当前浏览器，每条内容最多 256 字节、合计最多 2048 字节，请求 JSON 总长不超过 4 KiB。Go 正则不支持前后查找、回溯引用，无效规则会提示并保留当前有效筛选。超长行只显示和匹配前 64 KiB；无法在有界回溯范围内找到行首时跳过该行并提示。日志按 UTF-8 展示，非法字符显示为替代字符。

读取 Nginx 日志目录（`/home/wwwlogs`、`/usr/local/nginx/logs/gopanel`）中的日志时，可勾选“自动滚动”右侧的“格式化为表格”，依次显示字节偏移、客户端 IP、IP 属地、时间、请求、HTTP 状态码、响应字节数、Referer 和 User-Agent。属地展示数据库返回的地区及运营商信息；旁边的问号图标支持悬停、聚焦或点击查看数据来源、版本与 IPv6 支持说明。表格不提供原文列或原文弹窗。支持 common、combined 及相同字段前缀的常见文本格式；无法解析的行保留文本，自定义 JSON 或不同字段顺序不解析。表格支持在容器内横向滚动，沿用虚拟列表、历史加载和实时追踪。

IP 地址位置数据由[纯真 CZ88](https://www.cz88.net/) 提供支持，使用 [metowolf/qqwry.dat 的 20261021 版本](https://github.com/metowolf/qqwry.dat/releases/tag/20261021)，以 [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) 署名并分发，详见[纯真社区版数据说明](https://www.cz88.net/geo-public)。数据库 SHA-256 为 `15197d3a13d767a3113ca92bde9aa2fa02a99b0620eee07ebbd630262a4f57bf`，内嵌在面板二进制中，随安装包交付并随面板版本更新；运行时离线查询，无需另行安装数据库。DAT 格式只支持 IPv4，IPv6 显示“暂不支持”，未命中或查询失败显示“未知”。初始化失败会记录日志，日志读取继续运行。

表格模式下可多选具体 HTTP 状态码及 1xx～5xx 类别，多个选择取并集，未选择或点击“清除筛选”时显示全部。状态码筛选仅作用于现有关键词、正则规则返回的已加载范围，不扫描整个文件；加载历史和收到新增日志后继续筛选。启用状态码筛选时不展示无法解析的行。取消表格模式会清空状态码选择；切换文件时恢复原文模式并清空选择，表格及状态码设置不保存到浏览器。

新增需登录接口 `GET /api/v1/logs` 和 `POST /api/v1/logs/read`，POST 需要 CSRF。读取 JSON 包含 `path`、`mode`（`tail`、`before`、`after`、`range`）、不透明的 `start` / `end` 游标和 `rules`；每条规则包含 `kind`（`keyword` / `regex`）、`action`（`show` / `hide`）、`pattern`、`ignore_case`、`enabled`。响应提供匹配行及字节偏移、源文件范围、前后游标和文件状态，Nginx 和登录日志行可包含 `ip_location` 属地显示文本；409 表示文件已变化。属地由普通面板进程补充，不增加辅助程序权限。游标始终按原始范围推进，筛选全部隐藏也不会反复读取同一批。规则在 Go 后端执行，读取有超时和全局并发限制。

升级时同步安装两个二进制和 sudoers 模板，新增固定授权 `/usr/local/libexec/lnmp-gopanel-control logs list`、`/usr/local/libexec/lnmp-gopanel-control logs read`，验证 sudoers 并重启面板。文件、游标和规则通过有界 JSON stdin 传递，辅助程序重新校验敏感路径策略、文件类型和路径，禁止跟随任意路径组件中的符号链接。安装脚本仅检查这两项的 sudo 授权，不实际读取或输出日志内容。

自定义日志沿用上述两项 sudo 授权，需同步升级面板和辅助程序。新增 `POST /api/v1/logs/custom`、`DELETE /api/v1/logs/custom`，请求均为 `{"path":"绝对路径"}`，需要登录、CSRF，JSON 不超过 4 KiB。添加返回文件元数据，移除返回 204；列表中自定义条目带 `custom: true`。现有读取接口仅允许内置路径或已添加的自定义路径，不能通过直接请求绕过添加。辅助程序的读取请求新增 `stat` 模式用于有界格式检查与文件元数据查询，沿用读取超时和并发限制，不提供任意命令或文件修改权限。

## 站点配置格式化

在虚拟主机的“查看配置”弹窗中，点击“格式化缩进并保存”可按 4 空格整理当前站点配置；停用站点保持停用。该操作保留配置指令、字符串、注释和面板管理的访问认证，不修改共享 include 文件，也不重载 Nginx。配置文件已被其他操作修改时需点击“重新读取”后再执行；不支持安全解析的配置会保留原文件并显示原因。写入使用原子替换并保留权限，Nginx 检查失败时恢复原文件。

新增 `POST /api/v1/vhosts/config/format`，请求为 `{ "name": "example.com.conf", "revision": "读取配置返回的版本" }`，需要登录和 CSRF。`GET /api/v1/vhosts/config` 额外返回 `revision`；格式化接口返回新的配置内容、版本、`changed` 和提示信息。升级须同步安装面板、辅助程序和 sudoers，安装脚本会检查新增的 `vhosts format` 精确授权，不会在安装时执行格式化。
