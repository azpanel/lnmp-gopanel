package metricssetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/metrics"
)

type Console struct {
	ctx    context.Context
	Input  io.Reader
	Output io.Writer
	Secret func(string) ([]byte, error)
}

// Read one byte at a time so ordinary prompts cannot prefetch a password that
// term.ReadPassword subsequently needs to read from the same terminal.
func (c *Console) line(prompt, fallback string) (string, error) {
	if fallback != "" {
		fmt.Fprintf(c.Output, "%s [%s]：", prompt, fallback)
	} else {
		fmt.Fprintf(c.Output, "%s：", prompt)
	}
	type answer struct {
		value string
		err   error
	}
	result := make(chan answer, 1)
	go func() {
		var out strings.Builder
		var data [1]byte
		for {
			n, err := c.Input.Read(data[:])
			if n > 0 {
				if data[0] == '\n' {
					value := strings.TrimSpace(out.String())
					if value == "" {
						value = fallback
					}
					result <- answer{value: value}
					return
				}
				if out.Len() > 4096 {
					result <- answer{err: errors.New("输入过长")}
					return
				}
				if data[0] != '\r' {
					out.WriteByte(data[0])
				}
			}
			if err != nil {
				result <- answer{err: err}
				return
			}
		}
	}()
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case value := <-result:
		return value.value, value.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *Console) yes(prompt string, fallback bool) (bool, error) {
	hint := "y/N"
	if fallback {
		hint = "Y/n"
	}
	for {
		value, err := c.line(prompt+"（"+hint+"）", "")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "":
			return fallback, nil
		case "y", "yes", "是":
			return true, nil
		case "n", "no", "否":
			return false, nil
		}
		fmt.Fprintln(c.Output, "请输入 y 或 n。")
	}
}
func (c *Console) choice(prompt string, fallback int, valid ...int) (int, error) {
	for {
		value, err := c.line(prompt, strconv.Itoa(fallback))
		if err != nil {
			return 0, err
		}
		number, e := strconv.Atoi(value)
		if e == nil {
			for _, candidate := range valid {
				if number == candidate {
					return number, nil
				}
			}
		}
		fmt.Fprintln(c.Output, "请输入列表中的编号。")
	}
}
func (c *Console) password(prompt string) (string, error) {
	if c.Secret == nil {
		return "", errors.New("未配置隐藏密码输入")
	}
	data, err := c.Secret(prompt)
	defer clear(data)
	if err != nil {
		return "", err
	}
	if len(data) > 256 || bytes.ContainsAny(data, "\x00\r\n") {
		return "", errors.New("密码过长或包含控制字符")
	}
	return string(data), nil
}

type proposal struct {
	configPath                                string
	options                                   config.MetricsConfig
	selected                                  map[string]bool
	ids                                       []string
	changes                                   []change
	watch                                     []fileSnapshot
	nginxReload, phpReload                    bool
	account                                   accountAdmin
	accountUser, accountHost, accountPassword string
	accountCreated                            bool
	panelActive                               bool
}

func (p *proposal) add(s fileSnapshot, data []byte, mode os.FileMode, gid int) {
	p.watch = append(p.watch, s)
	if s.Exists && bytes.Equal(s.Data, data) && s.Mode == mode && s.GID == gid {
		return
	}
	p.changes = append(p.changes, change{before: s, after: data, mode: mode, gid: gid})
}
func Configure(ctx context.Context, path string, console *Console) error {
	e := systemEnvironment()
	e.probe = metrics.CheckServices
	return e.configure(ctx, path, console)
}
func (e *environment) configure(ctx context.Context, path string, c *Console) error {
	c.ctx = ctx
	p, err := e.collect(ctx, path, c)
	if p != nil && p.account != nil {
		defer p.account.Close()
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(c.Output, "\n已取消，未修改配置。")
			return nil
		}
		return err
	}
	if len(p.ids) == 0 {
		fmt.Fprintln(c.Output, "未选择服务指标，配置保持不变。")
		return nil
	}
	fmt.Fprintln(c.Output, "\n待应用的监控配置：")
	if p.selected["nginx"] {
		fmt.Fprintln(c.Output, "  Nginx：", p.options.NginxStatusURL)
	}
	if p.selected["php_fpm"] {
		fmt.Fprintln(c.Output, "  PHP-FPM：", p.options.PHPFPMStatusURL)
	}
	if p.selected["database"] {
		fmt.Fprintf(c.Output, "  数据库：账号 %s，连接 %s%s（密码隐藏）\n", p.options.MySQL.User, p.options.MySQL.Socket, p.options.MySQL.Address)
	}
	for _, change := range p.changes {
		fmt.Fprintln(c.Output, "  修改文件：", change.before.Path)
	}
	if p.account != nil {
		fmt.Fprintf(c.Output, "  新建数据库账号：%s@%s（仅基础登录权限，不授业务库权限）\n", p.accountUser, p.accountHost)
	}
	if p.nginxReload {
		fmt.Fprintln(c.Output, "  将检查并平滑重载 Nginx。")
	}
	if p.phpReload {
		fmt.Fprintln(c.Output, "  将检查并平滑重载 PHP-FPM；状态请求复用业务池，满载时也可能排队。")
	}
	if p.panelActive && len(p.changes) > 0 {
		fmt.Fprintln(c.Output, "  将在检查通过后重启面板。")
	}
	if !p.panelActive {
		fmt.Fprintln(c.Output, "  面板当前未运行，将保持停止状态。")
	}
	fmt.Fprintln(c.Output, "  不修改已有公网状态页、慢查询日志设置或组件显示选择。")
	confirmed, err := c.yes("确认应用以上变更", false)
	if errors.Is(err, io.EOF) || err == nil && !confirmed {
		fmt.Fprintln(c.Output, "已取消，未修改配置。")
		return nil
	}
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	backup, err := e.apply(ctx, p)
	if err != nil {
		return err
	}
	if backup != "" {
		fmt.Fprintln(c.Output, "监控配置完成。备份：", backup)
	} else {
		fmt.Fprintln(c.Output, "现有监控配置检查通过，无需修改或重载。")
	}
	return nil
}
func (e *environment) collect(ctx context.Context, path string, c *Console) (p *proposal, err error) {
	snapshot, err := e.read(path, false)
	if err != nil {
		return nil, err
	}
	var loaded config.Config
	if err = config.Read(e.path(path), &loaded); err != nil {
		return nil, errors.New("面板配置无法解析")
	}
	// Resolve defaults only for validation. patchPanel never persists them.
	validated, err := config.Load(e.path(path))
	if err != nil {
		return nil, errors.New("面板配置无效，请先修复原配置")
	}
	p = &proposal{configPath: path, options: validated.Metrics, selected: map[string]bool{}, panelActive: e.active(ctx, "panel")}
	p.watch = append(p.watch, snapshot)
	state, stateFile, err := e.readState(path)
	if err != nil {
		return p, err
	}
	nginxAuto := false
	phpAuto := false
	nginxAvailable := e.active(ctx, "nginx")
	phpAvailable := nginxAvailable && e.active(ctx, "php-fpm")
	socket := e.databaseSocket()
	fmt.Fprintln(c.Output, "服务监控快捷配置：逐项选择；跳过不会清除原配置；按 Ctrl-D 可取消。")
	if nginxAvailable {
		fmt.Fprintln(c.Output, "检测到运行中的 LNMP Nginx。已有公网状态页仅提示保留，不会自动收紧访问规则。")
	}
	for _, id := range []string{"nginx", "php_fpm", "database"} {
		title := map[string]string{"nginx": "Nginx", "php_fpm": "PHP-FPM", "database": "数据库"}[id]
		configured := id == "nginx" && loaded.Metrics.NginxStatusURL != "" || id == "php_fpm" && loaded.Metrics.PHPFPMStatusURL != "" || id == "database" && loaded.Metrics.MySQL.User != ""
		available := id == "nginx" && nginxAvailable || id == "php_fpm" && phpAvailable || id == "database" && socket != ""
		if !available && !configured {
			fmt.Fprintln(c.Output, title+"：未检测到可自动配置的运行环境，可跳过或填写已有本机连接。")
		}
		selected, e2 := c.yes("配置或检查 "+title+" 指标", available || configured)
		if e2 != nil {
			return p, e2
		}
		if !selected {
			continue
		}
		p.ids = append(p.ids, id)
		if configured {
			if id == "database" {
				fmt.Fprintf(c.Output, "当前账号：%s；连接：%s%s；密码隐藏。\n", loaded.Metrics.MySQL.User, loaded.Metrics.MySQL.Socket, loaded.Metrics.MySQL.Address)
			} else if id == "nginx" {
				fmt.Fprintln(c.Output, "当前地址：", loaded.Metrics.NginxStatusURL)
			} else {
				fmt.Fprintln(c.Output, "当前地址：", loaded.Metrics.PHPFPMStatusURL)
			}
			keep, e2 := c.yes("保留当前配置并检查", true)
			if e2 != nil {
				return p, e2
			}
			if keep {
				continue
			}
		}
		p.selected[id] = true
		switch id {
		case "nginx", "php_fpm":
			automatic := false
			if available {
				mode, e2 := c.choice("1) 自动启用本机状态入口  2) 填写已有本机 HTTP 地址", 1, 1, 2)
				if e2 != nil {
					return p, e2
				}
				automatic = mode == 1
			}
			if automatic {
				if id == "nginx" {
					nginxAuto = true
				} else {
					phpAuto = true
				}
			} else {
				fallback := loaded.Metrics.NginxStatusURL
				if id == "php_fpm" {
					fallback = loaded.Metrics.PHPFPMStatusURL
				}
				for {
					value, e2 := c.line(title+" 状态 URL（仅支持回环 IP 的 HTTP 地址，不跟随跳转）", fallback)
					if e2 != nil {
						return p, e2
					}
					if !config.LoopbackURL(value) {
						fmt.Fprintln(c.Output, "地址无效，例如 http://127.0.0.1:18080/nginx_status。")
						continue
					}
					if id == "nginx" {
						p.options.NginxStatusURL = value
					} else {
						p.options.PHPFPMStatusURL = value
					}
					break
				}
			}
		case "database":
			if e2 = e.collectDatabase(ctx, p, c, socket); e2 != nil {
				return p, e2
			}
		}
	}
	if nginxAuto || phpAuto {
		if err = e.prepareNginx(ctx, p, c, &state, stateFile, nginxAuto, phpAuto); err != nil {
			return p, err
		}
	}
	if p.options != validated.Metrics {
		data, e2 := patchPanel(snapshot.Data, p.options, p.selected)
		if e2 != nil {
			return p, e2
		}
		// The installed panel account must be able to read credentials, while all
		// root-owned configuration remains immutable to that account.
		p.add(snapshot, data, 0640, snapshot.GID)
	}
	if err = p.options.Validate(); err != nil {
		return p, errors.New("监控连接配置无效")
	}
	return p, nil
}
func (e *environment) databaseSocket() string {
	for _, path := range []string{"/tmp/mysql.sock", "/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/usr/local/mysql/mysql.sock", "/usr/local/mariadb/mysql.sock"} {
		if info, err := os.Stat(e.path(path)); err == nil && info.Mode()&os.ModeSocket != 0 {
			return path
		}
	}
	return ""
}
func (e *environment) collectDatabase(ctx context.Context, p *proposal, c *Console, detected string) error {
	current := p.options.MySQL
	fallback := 1
	if current.Address != "" {
		fallback = 2
	}
	mode, err := c.choice("数据库连接：1) Unix socket  2) 本机 TCP", fallback, 1, 2)
	if err != nil {
		return err
	}
	db := config.MetricsMySQLConfig{}
	if mode == 1 {
		defaultSocket := current.Socket
		if defaultSocket == "" {
			defaultSocket = detected
		}
		if defaultSocket == "" {
			defaultSocket = "/tmp/mysql.sock"
		}
		db.Socket, err = c.line("数据库 socket", defaultSocket)
	} else {
		address := current.Address
		if address == "" {
			address = "127.0.0.1:3306"
		}
		db.Address, err = c.line("数据库地址", address)
	}
	if err != nil {
		return err
	}
	validation := p.options
	validation.MySQL = db
	validation.MySQL.User = "validation"
	if validation.Validate() != nil {
		return errors.New("数据库地址无效，请使用绝对 socket 路径或本机回环 TCP 地址")
	}
	mode, err = c.choice("监控账号：1) 新建专用账号  2) 使用已有账号", 1, 1, 2)
	if err != nil {
		return err
	}
	if mode == 2 {
		db.User, err = c.line("已有监控账号", current.User)
		if err != nil {
			return err
		}
		if db.User == "" {
			return errors.New("监控账号不能为空")
		}
		prompt := "监控账号密码（输入隐藏）"
		if db.User == current.User && current.Password != "" {
			prompt += "，留空保留原密码"
		}
		db.Password, err = c.password(prompt + "：")
		if err != nil {
			return err
		}
		if db.Password == "" && db.User == current.User {
			db.Password = current.Password
		}
		if err = e.probe(ctx, config.MetricsConfig{MySQL: db}, []string{"database"}); err != nil {
			return errors.New("已有监控账号无法执行状态查询，请检查连接和凭据")
		}
	} else {
		db.User, err = c.line("新监控账号名称", "panel_monitor")
		if err != nil {
			return err
		}
		if !accountName.MatchString(db.User) || db.User == "root" {
			return errors.New("新监控账号需为字母开头的 1–32 位字母、数字或下划线，且不能为 root")
		}
		db.Password, err = randomPassword()
		if err != nil {
			return err
		}
		adminUser, e2 := c.line("数据库管理员用户名", "root")
		if e2 != nil {
			return e2
		}
		adminOptions := db
		adminOptions.User = adminUser
		adminOptions.Password = ""
		administrator, e2 := e.admin(ctx, adminOptions)
		for attempt := 0; e2 != nil && attempt < 3; attempt++ {
			fmt.Fprintln(c.Output, "管理员本机认证未通过，请输入密码；按 Ctrl-D 取消。")
			adminOptions.Password, err = c.password("管理员密码（输入隐藏）：")
			if err != nil {
				return err
			}
			administrator, e2 = e.admin(ctx, adminOptions)
		}
		if e2 != nil {
			return errors.New("管理员认证失败，未创建账号")
		}
		p.account = administrator
		p.accountUser = db.User
		p.accountHost = accountHost(db)
		p.accountPassword = db.Password
		if !validAccountHost(p.accountHost) {
			return errors.New("新建账号仅支持 localhost socket、127.0.0.1 或 ::1 TCP")
		}
		exists, e2 := administrator.Exists(ctx, db.User, p.accountHost)
		if e2 != nil {
			return e2
		}
		if exists {
			return errors.New("同名监控账号已存在，请重新运行并换名或选择使用已有账号；未修改其密码")
		}
	}
	p.options.MySQL = db
	return nil
}
func (e *environment) prepareNginx(ctx context.Context, p *proposal, c *Console, state *managedState, stateFile fileSnapshot, nginx, php bool) error {
	old, err := e.read(managedConfig, true)
	if err != nil {
		return err
	}
	if old.Exists && (!stateFile.Exists || !strings.HasPrefix(string(old.Data), managedHeader) || hash(old.Data) != state.Hash) {
		return errors.New("监控配置已被手工修改或不属于本向导，请使用已有状态 URL；未覆盖文件")
	}
	if stateFile.Exists && !old.Exists {
		return errors.New("受管监控配置文件缺失，请先恢复备份或手动配置")
	}
	originalPort := state.Port
	port := state.Port
	for {
		value, e2 := c.line("本机 HTTP 监控端口", strconv.Itoa(port))
		if e2 != nil {
			return e2
		}
		number, e2 := strconv.Atoi(value)
		if e2 == nil && number >= 1024 && number <= 65535 && (old.Exists && number == state.Port || e.portFree(number)) {
			port = number
			break
		}
		fmt.Fprintln(c.Output, "端口无效或已占用，请输入其他 1024–65535 端口。")
	}
	state.Port = port
	if nginx {
		state.Nginx = true
		p.options.NginxStatusURL = fmt.Sprintf("http://127.0.0.1:%d/nginx_status", port)
	}
	if php {
		pools, e2 := e.pools()
		if e2 != nil {
			return e2
		}
		if len(pools) == 0 {
			return errors.New("未找到可配置的 PHP-FPM 池")
		}
		for i, pool := range pools {
			fmt.Fprintf(c.Output, "  %d) %s（%s；%s）\n", i+1, pool.Name, pool.Path, pool.Listen)
		}
		indices := make([]int, len(pools))
		for i := range indices {
			indices[i] = i + 1
		}
		index, e2 := c.choice("选择 PHP-FPM 池", 1, indices...)
		if e2 != nil {
			return e2
		}
		pool := pools[index-1]
		p.watch = append(p.watch, pool.sources...)
		path := pool.Status
		if path == "" {
			path = "/fpm-status"
		}
		if _, e2 = renderNginx(port, state.Nginx, path, pool.Listen); e2 != nil {
			return e2
		}
		before, e2 := e.read(pool.Path, false)
		if e2 != nil {
			return e2
		}
		if !bytes.Equal(before.Data, pool.data) {
			return errors.New("PHP-FPM 配置在检测后发生变化，请重试")
		}
		data := addFPMStatus(pool, path)
		p.add(before, data, before.Mode, before.GID)
		p.phpReload = !bytes.Equal(before.Data, data)
		state.PHPPath, state.PHPListen = path, pool.Listen
		p.options.PHPFPMStatusURL = fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	}
	// Port changes also update existing URLs using this managed server, even if
	// their route was not selected for a new setup, so they cannot be broken.
	for id, raw := range map[string]*string{"nginx": &p.options.NginxStatusURL, "php_fpm": &p.options.PHPFPMStatusURL} {
		if old.Exists {
			replacement, changed := changeManagedPort(*raw, originalPort, port, state.PHPPath)
			if changed {
				*raw = replacement
				p.selected[id] = true
				if !contains(p.ids, id) {
					p.ids = append(p.ids, id)
				}
			}
		}
	}
	data, err := renderNginx(port, state.Nginx, state.PHPPath, state.PHPListen)
	if err != nil {
		return err
	}
	p.add(old, data, 0644, e.fileGroup)
	main, err := e.read(nginxConfig, false)
	if err != nil {
		return err
	}
	included, err := addMetricsInclude(main.Data)
	if err != nil {
		return err
	}
	p.add(main, included, main.Mode, main.GID)
	state.Hash = hash(data)
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	p.add(stateFile, append(encoded, '\n'), 0600, e.fileGroup)
	p.nginxReload = !old.Exists || !bytes.Equal(old.Data, data) || !bytes.Equal(main.Data, included)
	if err = e.command(ctx, nginxBinary, "-t"); err != nil {
		return errors.New("原 Nginx 配置检查失败，未应用修改")
	}
	if php {
		if err = e.command(ctx, fpmBinary, "-t", "-y", e.path(fpmConfig)); err != nil {
			return errors.New("原 PHP-FPM 配置检查失败，未应用修改")
		}
	}
	return nil
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func changeManagedPort(raw string, from, to int, phpPath string) (string, bool) {
	if from == to {
		return raw, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Hostname() != "127.0.0.1" || u.Port() != strconv.Itoa(from) || (u.Path != "/nginx_status" && u.Path != phpPath) {
		return raw, false
	}
	u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(to))
	return u.String(), true
}
