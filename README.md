# LNMP GoPanel

面向 lnmp.org 安装环境的单机实时监控与服务管理面板。Go + Gin 后端，React + TypeScript + Vite 前端，使用 shadcn/ui、Tailwind CSS 和 Recharts。

- CPU、内存、Swap、负载、运行时间、磁盘容量、磁盘 I/O 和网络速率。
- SSE 每 2 秒推送；统一采集，多浏览器共享。无数据库、Redis 和指标历史文件。
- 浏览器仅在内存中保留最近 5 分钟、最多 150 个采样点；刷新或退出登录即清空。重连只取最新数据，断档不补线。
- Nginx、PHP-FPM、MySQL / MariaDB 状态与启停、重启；Nginx 配置检查通过后才允许重载。Redis 提供只读状态监控。
- 显示服务已安装版本，异步查询官方最新稳定版本及 changelog。
- Docker 只读概览：运行中容器的基础信息、可写层与虚拟大小，以及镜像、容器、数据卷和构建缓存的空间占用。
- 定时任务只读查看 root crontab，展示执行周期、命令和原文，支持刷新。
- 日志读取支持网站、MariaDB 慢查询及 PHP 错误日志，按需加载大文件、实时追踪和多条关键词／正则筛选。
- 单管理员、bcrypt 密码、内存 Session、CSRF 校验；独立特权辅助程序和精确 sudoers 规则。
- 中文、默认亮色、深浅主题切换、移动端导航。登录页、工作区及通知使用一致的主题。主题偏好及每个日志文件的筛选规则写入 localStorage，日志内容不保存到浏览器持久存储。

## 安装、更新与卸载

在 Linux amd64 / arm64 机器上，以 root 或具备 sudo 权限的账号执行。需要运行中的 systemd、`curl`、`tar`、`gzip`、`sudo`（含 `visudo`）和常见系统工具；Debian / Ubuntu 可先执行 `sudo apt-get install -y curl ca-certificates tar gzip sudo`。

```bash
curl -fsSL https://github.com/azpanel/lnmp-gopanel/releases/latest/download/install.sh -o install.sh
sudo bash install.sh
```

直接运行会显示中文管理菜单，可选择安装、从 GitHub Release 更新、完全卸载或退出。也可以显式使用 `--install`、`--update`、`--uninstall`；已有带安装参数的调用方式继续有效。

```bash
sudo bash install.sh --update                    # 从 GitHub Release 更新到最新版本
sudo bash install.sh --update --version v0.1.0   # 更新到指定 Release
sudo bash install.sh --uninstall                 # 列出清理范围，输入 yes 确认
sudo bash install.sh --uninstall --yes           # 非交互完全卸载
```

更新要求已有完整安装，下载对应架构的 Release 安装包并验证 SHA-256，沿用备份和失败回滚流程。完全卸载无需联网，会停止并禁用面板，删除面板程序、辅助程序、systemd 单元及专属覆盖配置、sudoers 规则、`/etc/lnmp-gopanel`（含面板证书副本）、`/var/lib/lnmp-gopanel`、`/var/backups/lnmp-gopanel` 和 `lnmp-panel` 专用账号及组。LNMP、网站、数据库、Docker、借用的原始证书和 Chromium 等系统依赖会保留；自定义到其他目录的面板数据请自行清理。卸载会永久删除面板配置及备份，请先保存需要保留的内容。

如需网站截图，可在 Debian 12/13 使用 `sudo bash install.sh --with-screenshots`，额外安装 Chromium、沙箱组件和中文字体，并以面板普通账号运行截图冒烟检查。已有 Chromium/Chrome 时优先复用；其他 Linux 发行版请自行安装浏览器，通过 `screenshots.browser` 指定其绝对路径。普通安装不安装浏览器，缺少浏览器只影响截图功能。`--check` 不安装系统依赖；可选安装产生的系统软件包不会在面板文件回滚时卸载。

脚本下载最新 Release 中对应架构的预编译安装包，验证 SHA-256，创建普通服务账号并安装面板、特权辅助程序、精确的 sudoers 规则及 systemd 服务。首次安装交互选择 SSL 证书并设置管理员密码（12–72 字节），检测本机 LNMP / Redis 服务，并启动面板。面板仅提供 HTTPS，监听 `0.0.0.0` 上由系统随机分配的空闲端口；安装完成后显示监听地址和端口，需在安全组及防火墙中放行该 TCP 端口。运行时无需 Go、Node.js 或 OpenSSL。

证书有两种来源：

- 自动生成自签名证书：无需填写域名、组织等表单，自动使用 ECDSA P-256 密钥、随机序列号、一年有效期，以及 localhost、本机主机名和网卡 IP 的 SAN。TLS 最低版本为 1.2，浏览器需要信任该证书；服务器公网 IP 经 NAT 映射时可能不在自动发现的 SAN 中。
- 借用现有证书：扫描 `/usr/local/nginx/conf/ssl` 下的目录，仅列出存在有效证书及匹配的未加密私钥的目录，支持 `.pem`、`.crt`、`.cer`、`.key` 及 ACME 符号链接，优先使用 fullchain。选择后复制到面板专用目录，不修改源证书或私钥权限。使用证书覆盖的域名访问。

证书和私钥安装到 `/etc/lnmp-gopanel/tls/`，目录权限 `0750`、文件权限 `0640`、所有者 `root:lnmp-panel`。借用的证书不会自动跟随原证书续期；续期后重新执行 `sudo bash install.sh --certificate-dir /usr/local/nginx/conf/ssl/域名`，同步证书并重启面板，已有 HTTPS 端口保持不变。自签名证书可通过 `--tls-mode self-signed` 重新生成。

安装时会以 `lnmp-panel` 账号检查服务状态、Docker 查询、文件扫描、定时任务与日志读取的精确 sudo 授权，并实际免密调用一次只读 `docker snapshot` 辅助程序（无需安装或启动 Docker，不触发全盘扫描或服务启停）。启动面板后读取实际进程的 `/proc/<MainPID>/status`，确认 `NoNewPrivs: 0`，避免 systemd 隐式限制使所有辅助功能不可用；检查失败会报错并回滚。`--no-start` 只执行辅助程序检查，进程限制需在自行启动后验证。

在菜单选择更新或执行 `sudo bash install.sh --update` 即可从 GitHub Release 升级；带安装参数再次执行也可覆盖升级。已有 HTTPS 配置、端口、证书、管理员密码和 `services.yaml` 会保留。旧版 HTTP 安装升级时会询问证书来源、迁移为随机端口的 HTTPS，并保留管理员凭据及服务配置；原 HTTP 端口不再使用，需调整安全组或反向代理。升级只重启面板服务，不重启 Nginx、PHP、数据库、Redis 或 Docker；已有面板会话需重新登录。替换前的文件保存在 `/var/backups/lnmp-gopanel/`，替换或启动失败时自动恢复旧文件，包含原配置及证书。

指定版本、自定义首次安装用户名、只验证包及离线安装：

```bash
sudo bash install.sh --version v0.1.0 --username admin
bash install.sh --version v0.1.0 --check
# 从 Release 下载两个同名配套文件后执行；arm64 使用对应的 arm64 文件名
sudo bash install.sh --archive ./lnmp-gopanel_linux_amd64.tar.gz
```

离线安装同时需要 `lnmp-gopanel_linux_amd64.tar.gz.sha256`（或通过 `--checksum-file` 指定校验文件）。`--no-start` 用于安装后自行检查配置，脚本不会启用或重启面板；检查后执行 `sudo systemctl enable --now lnmp-gopanel`，已有运行实例升级后需执行 `sudo systemctl restart lnmp-gopanel`。非交互首次安装使用 `--password-stdin --tls-mode self-signed`，或 `--password-stdin --tls-mode existing --certificate-dir /usr/local/nginx/conf/ssl/域名`；密码从标准输入读取，勿把密码放入命令行参数。非交互旧版 HTTP 升级也需显式选择证书模式。

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

从当前源码升级本机时，先构建前端，再运行上述打包命令，最后使用项目中的安装脚本执行 `sudo bash install.sh --archive ./dist/lnmp-gopanel_linux_amd64.tar.gz`（arm64 使用对应文件名）。这样两个二进制、systemd 单元及 sudoers 会一起更新，并使用相同的备份、授权检查及失败回滚流程。定时任务与日志仅检查 `cron list`、`logs list`、`logs read` 的精确授权，不读取或记录任务及日志内容；未安装 crontab 或日志文件不存在不会阻止安装。旧版 HTTP 安装非交互升级时需追加 `--tls-mode self-signed`，或指定现有证书。

## 构建

构建机需要 Go 1.26+、Node.js 22.12+、npm、make。目标机器为 Linux，运行时无需 Node.js。

```bash
make build
```

生成 `bin/lnmp-gopanel` 和 `bin/lnmp-gopanel-control`。生产面板使用 `-tags embed` 将前端嵌入 Go 二进制；服务辅助程序为独立二进制。

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
- LNMP 的 Nginx/PHP-FPM init 脚本可能不支持 `status`；这两类服务使用 `/proc` 中的完整可执行文件路径检查进程，避免将 usage 输出当作运行状态。systemd 服务仍使用对应 unit 状态。
- 不支持或无法明确解释的状态返回“未知”；systemd 的切换状态单独显示，不误报停止。
- 服务操作最多执行 30 秒，同服务互斥；超时后重新检查实际状态，不自动重试启停。
- 会话最长 8 小时，重启面板会清空会话。登录每分钟最多 10 次，系统最多 32 条 SSE 连接；慢连接写入限时，单个订阅仅缓存最新一份快照。
- 必要运行和服务操作记录写入标准日志（systemd journal），不记录密码、令牌、指标样本或服务命令原始输出。`journalctl -u lnmp-gopanel` 可查看部署错误。

## API

所有数据接口要求 Cookie 会话。响应禁止缓存；写操作要求 `X-CSRF-Token`，其值从登录响应或当前会话获得。跨源 Origin 被拒绝，不开启 CORS。

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
| GET | `/api/v1/components` | 异步读取本机 Python 3、Go、Node.js、Codex CLI 版本 |
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

“LNMP 服务”页面的“编程组件”分组固定展示 Python 3、Go、Node.js、Codex CLI 的本机版本、可执行路径、检查时间、最新正式版本及对应 Changelog。即使未配置任何 LNMP 服务，也可查看组件信息。仅查询版本，不提供安装、升级或启停操作。

本机版本通过固定的 `components snapshot` 辅助命令读取，检查程序及其父目录的 root 所有权和写权限，不加载 shell 配置。Python、Go 检查标准系统安装目录；Node.js、Codex 优先选择 `/root/.nvm/alias/default` 指定的已安装版本，再检查系统安装。支持数字版本前缀、`node`、`stable`、`lts/*` 和 LTS 名称别名；不跟踪临时终端的 `nvm use`，不扫描其他用户目录。Codex 的 npm 启动脚本使用安装目录中的 Node。未安装和读取失败分别展示，读取失败保留上次成功版本。

采集异步执行，每个程序最多 8 秒，结果缓存 1 分钟，手动刷新间隔至少 5 秒。最新版本排除预发布，Node.js 按最高正式版本查询，可能高于 LTS 分支；Python 使用 python.org 发布列表与 CPython 对应标签的 NEWS，Go 使用 go.dev 下载接口及发布历史，Node.js 使用官方版本索引与对应标签 Changelog，Codex CLI 使用 OpenAI 官方仓库的正式 GitHub Release。联网查询与服务共用缓存、限流和失败处理。

已有安装需要同步更新面板、辅助程序和 sudoers，新增精确授权 `lnmp-gopanel-control components snapshot`；安装脚本会验证该权限。无需修改 `services.yaml` 或放宽 `/root` 目录权限。

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

“虚拟主机”页面按 `/usr/local/nginx/conf/vhost` 中的配置文件展示站点：域名、监听地址、反向代理目标、目录占用、SSL 证书签发者、主体、域名、有效期、剩余天数、序列号及 SHA-256 指纹。可只读查看原始配置。`.conf` 表示启用，`.conf.disabled` 表示停用；同文件的多个 server 块作为一项配置一起管理。

侧栏“虚拟主机”显示配置总数，包含已启用及已停用的站点，和“LNMP 服务”使用相同数量角标。登录后通过需鉴权的 `GET /api/v1/vhosts/count` 查询，每 30 秒刷新，读取失败时显示“—”或上次成功结果，不误报为零。统计只读取站点元数据、不触发目录占用扫描，多浏览器共享 30 秒缓存；站点列表刷新成功后立即同步角标，搜索筛选不改变总数。

配置解析支持注释、引号、多行指令及静态 include（相对于 Nginx conf 目录，设有深度与读取上限）。动态 root 或证书路径显示无法静态计算，不执行变量或脚本。SSL 信息来自已配置的本地 PEM 叶证书，时间状态不代表证书链信任、域名匹配或在线吊销校验；证书信息解析不读取 `ssl_certificate_key` 指向的私钥文件。无显式 root 的反向代理显示无本地目录。

目录占用在站点元数据加载后异步计算，使用 `du -s -x -B1` 的实际磁盘块大小。root 路径最后一级为 `public` 时统计其上一级目录；相同目录去重计算，符号链接部署目录先解析实际目标。单目录最多 45 秒，总任务最多 5 分钟，结果只保留内存。站点配置和证书可通过“刷新站点”重新读取。

启停时只对普通配置文件原地重命名，保留原文件内容与权限；拒绝目录穿越、符号链接、同名文件冲突及覆盖目标。重命名后执行 `nginx -t`，通过才发出 `nginx -s reload`。检查失败恢复原文件名；重载失败恢复原文件名并尝试重新加载原配置，失败结果明确提示。所有面板触发的 Nginx 修改与站点启停共享进程间锁。

新增接口：`GET /api/v1/vhosts`、`POST /api/v1/vhosts/refresh`（202，异步刷新）、`GET /api/v1/vhosts/config?name=example.conf`、`POST /api/v1/vhosts/toggle`（JSON `{"name":"example.conf","enabled":false}`）。全部需要登录，POST 需要 CSRF。特权操作仅开放固定参数 `vhosts list/config/usage/toggle`，文件名通过有界 JSON stdin 传递；辅助程序重新验证文件名与目录权限。升级时同步安装两个二进制和 sudoers 模板。

### 网站截图

虚拟主机支持卡片和表格视图，两种视图均展示网站预览。通过“截图设置”填写 HTTP/HTTPS 地址，可启用 Basic Auth。保存后后台截取 1440×900 桌面首屏，之后手动“重新截图”，点击图片可放大；没有定时截图、全页截图或历史图库。支持公网、本机和内网地址，浏览器访问的是服务器可达的地址，不使用管理员浏览器中的登录状态。HTTPS 证书错误、HTTP 错误和认证失败会显示原因，保留上次成功截图。

截图由 Go 直接控制本机 Chromium，无需生产环境安装 Node.js，不通过 sudo 辅助程序启动浏览器。浏览器必须以普通面板账号运行，保留沙箱，每次任务使用独立临时配置，完成或取消后清理。全局一次执行一个任务、最多排队 64 项、单次最多 30 秒。Basic Auth 只发送给配置地址同源的认证挑战，跨源跳转不会携带凭据；如需在跳转后的站点认证，请直接填写最终地址。密码不回传，编辑时留空保留原密码，更换目标来源或用户名时需重新填写；关闭认证会清除凭据，“清除设置”会同时删除保存的图片。

可选配置：

```yaml
screenshots:
  browser: /usr/bin/chromium # 留空时在 PATH 中检测 Chromium/Chrome
  state_dir: /var/lib/lnmp-gopanel
```

标准部署的状态目录为 `/var/lib/lnmp-gopanel`，由安装脚本和 systemd 为 `lnmp-panel` 创建；自定义配置文件默认在其同级 `screenshots-state/` 保存状态，也可以显式指定目录。目录必须由面板账号所有、权限 `0700`，状态文件和图片权限为 `0600`。密码使用 AES-GCM 加密保存，`screenshots.key` 与 `screenshot-settings.json` 必须一起备份、恢复，密钥丢失不会自动覆盖重建。设置与最后成功截图在升级及重启后保留；它们不会写入 Nginx 配置或打包进 Release。缺失浏览器或状态目录不可用时，其他功能继续工作。

部署后可用 `sudo -u lnmp-panel /usr/local/bin/lnmp-gopanel check-screenshots` 检查截图组件，仅访问临时本地测试页面，不访问业务站点。源码集成测试使用 `SCREENSHOT_TEST_BROWSER=/path/to/chromium go test -race ./internal/screenshots -run TestChromiumIntegration`，所有目标都是本地测试服务器；仅隔离的 root 测试进程会关闭浏览器沙箱，生产代码不提供该开关。

新增接口：`GET /api/v1/vhosts/screenshots`、`PUT /api/v1/vhosts/screenshot?name=…`、`POST /api/v1/vhosts/screenshot/refresh?name=…`、`DELETE /api/v1/vhosts/screenshot?name=…`、`GET /api/v1/vhosts/screenshot/image?name=…`。均需登录，写操作需 CSRF；设置与刷新返回 202，图片通过面板鉴权后读取且禁止缓存。

### 定时任务

左侧“定时任务”页面只读展示 root 用户的 crontab，包含执行周期、执行命令、原始行号、任务数量和最近读取时间，可手动刷新并展开查看原文。支持五字段 Cron 表达式及 `@reboot` 等特殊周期；环境变量、注释和空行不计为任务，未识别内容会显示提示并保留原文。此页面不包含其他用户、`/etc/crontab`、系统 Cron 目录或 systemd timer，也不提供编辑或立即执行功能。

接口 `GET /api/v1/cron` 需要登录，返回 `tasks`、`raw`、`warnings` 和 `updated_at`。读取失败会明确提示，刷新失败时保留上次成功的数据并标记可能过期；没有 root crontab 时显示空列表。辅助程序仅接受固定的 `cron list`，在可信路径下执行 `crontab -u root -l`，有超时及输出大小限制，不接受用户输入的参数。

升级时同步更新面板、辅助程序和 `deploy/lnmp-gopanel.sudoers`，新增精确授权 `/usr/local/libexec/lnmp-gopanel-control cron list`，先通过 `visudo -cf` 验证再安装并重启面板。安装脚本会检查该只读操作的 sudo 授权；未安装 crontab 不影响安装，页面会提示无法读取。

定时任务表达式支持鼠标悬停、键盘聚焦或手机点击预览之后 5 次预计执行时间。预览接口 `GET /api/v1/cron/preview?line=行号` 需要登录，以服务器当前时间重新读取并计算，默认使用服务器时区，任务前的 `CRON_TZ` 声明可覆盖时区；每个时间保留时区偏移。`@reboot`、无效表达式、无效时区和没有匹配日期时会显示说明。预览不检查 Cron 守护进程是否运行，不代表任务已执行或执行成功。原文展开后按需加载 Prism，对周期、环境变量、注释和 Shell 语法高亮，保留原始文本并由 React 安全渲染。此更新沿用 `cron list` 授权，安装脚本无需新增权限。

### 日志读取

左侧“日志读取”页面支持 `/home/wwwlogs` 直属目录中的 `.log` 普通文件，以及 `/var/log/mysql/mariadb-slow.log`、`/usr/local/php/var/log/php_errors.log`。在“日志内容”卡片顶部选择日志，卡片显示路径、大小和修改时间；不递归读取子目录，不读取轮转归档或符号链接，不提供编辑、清空和下载功能。文件不存在不影响安装。

默认定位到文件末尾，约每秒增量读取，可暂停追踪、自动滚动、回到最新或加载更早日志。向上滚动会关闭自动滚动；加载历史会暂停追踪。后台标签页暂停轮询，返回后从游标继续读取。单批原始范围最多 256 KiB、最多返回 4000 行；页面缓存最多 2 MiB、20000 行和 64 批，超限会释放浏览方向另一端，可重新加载。日志使用虚拟列表显示，避免整文件读取和大量 DOM。文件轮转或截断后提示并重新读取最新文件，不混合不同版本的历史。

点击“日志内容”旁的“筛选规则”，在右侧抽屉中编辑并应用规则；关闭抽屉保留未应用的编辑，应用成功后自动收起。可添加最多 20 条关键词或 Go 正则规则，逐行选择“显示匹配”或“隐藏匹配”，支持启停及忽略大小写，默认区分大小写。存在显示规则时命中任意一条即可，隐藏规则优先；没有显示规则时默认显示全部。点击“应用规则”后，仅重新筛选已加载范围，不搜索整个文件；慢 SQL 和错误堆栈也逐行处理。规则按文件保存在当前浏览器，每条内容最多 256 字节、合计最多 2048 字节，请求 JSON 总长不超过 4 KiB。Go 正则不支持前后查找、回溯引用，无效规则会提示并保留当前有效筛选。超长行只显示和匹配前 64 KiB；无法在有界回溯范围内找到行首时跳过该行并提示。日志按 UTF-8 展示，非法字符显示为替代字符。

新增需登录接口 `GET /api/v1/logs` 和 `POST /api/v1/logs/read`，POST 需要 CSRF。读取 JSON 包含 `path`、`mode`（`tail`、`before`、`after`、`range`）、不透明的 `start` / `end` 游标和 `rules`；每条规则包含 `kind`（`keyword` / `regex`）、`action`（`show` / `hide`）、`pattern`、`ignore_case`、`enabled`。响应提供匹配行及字节偏移、源文件范围、前后游标和文件状态；409 表示文件已变化。游标始终按原始范围推进，筛选全部隐藏也不会反复读取同一批。规则在 Go 后端执行，读取有超时和全局并发限制。

升级时同步安装两个二进制和 sudoers 模板，新增固定授权 `/usr/local/libexec/lnmp-gopanel-control logs list`、`/usr/local/libexec/lnmp-gopanel-control logs read`，验证 sudoers 并重启面板。文件、游标和规则通过有界 JSON stdin 传递，辅助程序重新校验白名单、文件类型和路径，禁止跟随任意路径组件中的符号链接。安装脚本仅检查这两项的 sudo 授权，不实际读取或输出日志内容。
