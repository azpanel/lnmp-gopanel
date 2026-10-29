//go:build linux

package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const lnmpRepository = "https://github.com/azpanel/lnmp2.3-offline.git"

type rootInstaller struct{ state, repo, services, helper string }

func defaultInstaller() rootInstaller {
	return rootInstaller{"/var/lib/lnmp-gopanel-install", "/root/lnmp2.3-offline", "/etc/lnmp-gopanel/services.yaml", "/usr/local/libexec/lnmp-gopanel-control"}
}
func (p rootInstaller) path(id, name string) string { return filepath.Join(p.state, id, name) }
func atomicInstallFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".install-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func (p rootInstaller) lock() (func(), error) {
	if err := os.MkdirAll(p.state, 0700); err != nil {
		return nil, err
	}
	if err := TrustedPath(p.state); err != nil {
		return nil, errors.New("安装状态目录权限不安全")
	}
	f, err := os.OpenFile(filepath.Join(p.state, "lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = TrustedPath(f.Name()); err != nil {
		f.Close()
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, lnmpinstall.ErrConflict
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
func (p rootInstaller) load(id string) (*lnmpinstall.Job, error) {
	if !lnmpinstall.ValidID(id) {
		return nil, errors.New("安装任务标识无效")
	}
	path := p.path(id, "job.json")
	if err := TrustedPath(path); err != nil {
		return nil, errors.New("安装任务不存在或权限不安全")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var job lnmpinstall.Job
	if json.Unmarshal(data, &job) != nil || job.ID != id {
		return nil, errors.New("安装任务记录损坏")
	}
	return &job, nil
}
func (p rootInstaller) current() (*lnmpinstall.Job, error) {
	path := filepath.Join(p.state, "current")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, nil
	}
	if err := TrustedPath(path); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return p.load(string(b))
}
func (p rootInstaller) save(j *lnmpinstall.Job) error {
	j.UpdatedAt = time.Now().UTC()
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicInstallFile(p.path(j.ID, "job.json"), b, 0600)
}
func installUnit(id, mode string) string { return "lnmp-gopanel-lnmp-" + mode + "-" + id + ".service" }
func (p rootInstaller) reconcile(ctx context.Context, j *lnmpinstall.Job) error {
	if j == nil || !lnmpinstall.Active(j.Status) || time.Since(j.UpdatedAt) < 30*time.Second {
		return nil
	}
	mode := "prepare"
	if j.Status == "running" {
		mode = "install"
	}
	out, err := RunCommand(ctx, "/usr/bin/systemctl", "show", "--property=ActiveState,LoadState", installUnit(j.ID, mode))
	if err != nil {
		return errors.New("无法查询安装系统任务状态")
	}
	if strings.Contains(out, "ActiveState=active\n") || strings.HasSuffix(out, "ActiveState=active") || strings.Contains(out, "ActiveState=activating") {
		return nil
	}
	j.Status, j.Error = "interrupted", "安装系统任务已结束但未记录完成结果，请检查日志与服务器实际状态"
	_ = os.Remove(p.path(j.ID, "request.json"))
	return p.save(j)
}
func installedLNMP() bool {
	return firstExecutable("/bin/lnmp", "/usr/bin/lnmp", "/usr/local/bin/lnmp") != ""
}
func installBlockers() []string {
	out := []string{}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		out = append(out, "在线安装需要正在运行的 systemd")
	}
	for _, path := range []string{"/usr/local/nginx", "/usr/local/php", "/usr/local/mysql", "/usr/local/mariadb", "/home/wwwroot/default", "/etc/my.cnf", "/etc/mysql/my.cnf", "/usr/sbin/nginx", "/usr/sbin/mysqld", "/usr/sbin/mariadbd", "/usr/sbin/php-fpm", "/etc/init.d/nginx", "/etc/init.d/php-fpm", "/etc/init.d/mysql", "/etc/init.d/mariadb"} {
		if _, err := os.Lstat(path); err == nil {
			out = append(out, "存在组件或安装残留："+path)
		} else if !os.IsNotExist(err) {
			out = append(out, "无法检查："+path)
		}
	}
	if paths, _ := filepath.Glob("/usr/sbin/php-fpm[0-9]*"); len(paths) > 0 {
		out = append(out, "存在系统 PHP-FPM："+strings.Join(paths, ", "))
	}
	return out
}
func checkInstallPorts(database bool) error {
	ports := []string{"80", "443"}
	if database {
		ports = append(ports, "3306")
	}
	for _, port := range ports {
		l, err := net.Listen("tcp", ":"+port)
		if err != nil {
			return fmt.Errorf("安装端口 %s 不可用：%w", port, err)
		}
		_ = l.Close()
	}
	return nil
}
func (p rootInstaller) inspect(ctx context.Context) (lnmpinstall.Snapshot, error) {
	j, err := p.current()
	if err != nil {
		return lnmpinstall.Snapshot{}, err
	}
	if j != nil && lnmpinstall.Active(j.Status) && time.Since(j.UpdatedAt) >= 30*time.Second {
		release, err := p.lock()
		if err != nil {
			return lnmpinstall.Snapshot{}, err
		}
		defer release()
		j, err = p.current()
		if err != nil {
			return lnmpinstall.Snapshot{}, err
		}
		if err = p.reconcile(ctx, j); err != nil {
			return lnmpinstall.Snapshot{}, err
		}
	}
	return lnmpinstall.Snapshot{Installed: installedLNMP(), Blocked: installBlockers(), Job: j}, nil
}
func (p rootInstaller) launch(ctx context.Context, j *lnmpinstall.Job, mode string) (launchErr error) {
	defer func() {
		if launchErr != nil {
			j.Status, j.Error = "failed", launchErr.Error()
			_ = os.Remove(p.path(j.ID, "request.json"))
			_ = p.save(j)
		}
	}()
	for _, path := range []string{p.helper, "/usr/bin/systemd-run", "/usr/bin/systemctl", "/bin/bash"} {
		if err := TrustedPath(path); err != nil {
			return fmt.Errorf("安装程序不可用或权限不安全：%s", path)
		}
	}
	_, err := RunCommand(ctx, "/usr/bin/systemd-run", "--quiet", "--collect", "--unit="+installUnit(j.ID, mode), "--property=Type=exec", "--property=UMask=0077", "--property=StandardInput=null", "--property=StandardOutput=null", "--property=StandardError=journal", "--property=TimeoutStopSec=15s", p.helper, "lnmp-install-worker", mode, j.ID)
	if err != nil {
		return errors.New("无法启动独立安装服务，请检查 systemd")
	}
	return nil
}
func (p rootInstaller) prepare(ctx context.Context, id string) (*lnmpinstall.Job, error) {
	if !lnmpinstall.ValidID(id) {
		return nil, errors.New("安装任务标识无效")
	}
	release, err := p.lock()
	if err != nil {
		return nil, err
	}
	defer release()
	j, err := p.current()
	if err != nil {
		return nil, err
	}
	if err = p.reconcile(ctx, j); err != nil {
		return nil, err
	}
	if j != nil && j.ID == id {
		return j, nil
	}
	if j != nil && lnmpinstall.Active(j.Status) {
		return nil, lnmpinstall.ErrConflict
	}
	if installedLNMP() {
		return nil, fmt.Errorf("%w：LNMP 已安装", lnmpinstall.ErrConflict)
	}
	if blockers := installBlockers(); len(blockers) > 0 {
		return nil, fmt.Errorf("%w：%s", lnmpinstall.ErrConflict, strings.Join(blockers, "；"))
	}
	if _, err = os.Lstat(p.path(id, "job.json")); !os.IsNotExist(err) {
		return nil, errors.New("该任务标识已使用，请重新发起")
	}
	if err = os.Mkdir(p.path(id, ""), 0700); err != nil {
		return nil, err
	}
	j = &lnmpinstall.Job{ID: id, Status: "preparing", Phase: "源码准备", CreatedAt: time.Now().UTC()}
	if err = p.save(j); err != nil {
		return nil, err
	}
	if err = atomicInstallFile(filepath.Join(p.state, "current"), []byte(id), 0600); err != nil {
		return nil, err
	}
	if err = p.launch(ctx, j, "prepare"); err != nil {
		return nil, errors.New(j.Error)
	}
	return j, nil
}
func (p rootInstaller) start(ctx context.Context, r lnmpinstall.Request) (*lnmpinstall.Job, error) {
	release, err := p.lock()
	if err != nil {
		return nil, err
	}
	defer release()
	j, err := p.current()
	if err != nil {
		return nil, err
	}
	if j == nil || j.ID != r.ID || j.Options == nil {
		return nil, errors.New("安装任务不存在，请先准备源码")
	}
	if err = r.Validate(*j.Options); err != nil {
		return nil, err
	}
	if j.Status == "running" || j.Status == "succeeded" {
		return j, nil
	}
	if j.Status != "ready" {
		return nil, lnmpinstall.ErrConflict
	}
	if installedLNMP() {
		return nil, fmt.Errorf("%w：LNMP 已安装", lnmpinstall.ErrConflict)
	}
	if b := installBlockers(); len(b) != 0 {
		return nil, fmt.Errorf("%w：%s", lnmpinstall.ErrConflict, strings.Join(b, "；"))
	}
	if err = checkInstallPorts(r.Database != 0); err != nil {
		return nil, fmt.Errorf("%w：%s", lnmpinstall.ErrConflict, err)
	}
	if err = p.verifySource(ctx, r.Revision); err != nil {
		return nil, err
	}
	if r.Database == 0 {
		r.DatabasePassword = ""
	}
	if r.Protection != "basic" {
		r.AuthUser, r.AuthPassword = "", ""
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if err = atomicInstallFile(p.path(j.ID, "request.json"), b, 0600); err != nil {
		return nil, err
	}
	j.Status, j.Phase, j.Modules = "running", "模块准备", r.Modules
	if err = p.save(j); err != nil {
		_ = os.Remove(p.path(j.ID, "request.json"))
		return nil, err
	}
	if err = p.launch(ctx, j, "install"); err != nil {
		return nil, errors.New(j.Error)
	}
	return j, nil
}
func (p rootInstaller) read(r lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error) {
	page := lnmpinstall.LogPage{Lines: []lnmpinstall.Line{}, Cursor: r.After}
	if !lnmpinstall.ValidID(r.ID) || r.After < 0 {
		return page, errors.New("日志游标或任务标识无效")
	}
	if _, err := p.load(r.ID); err != nil {
		return page, err
	}
	path := p.path(r.ID, "output.jsonl")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		if r.After != 0 {
			return page, errors.New("日志游标无效")
		}
		return page, nil
	}
	if err := TrustedPath(path); err != nil {
		return page, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return page, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return page, err
	}
	if r.After > info.Size() {
		return page, errors.New("日志游标超出文件大小")
	}
	if r.After > 0 {
		if _, err = f.Seek(r.After-1, 0); err != nil {
			return page, err
		}
		var b [1]byte
		if _, err = f.Read(b[:]); err != nil || b[0] != '\n' {
			return page, errors.New("日志游标不在记录边界")
		}
	}
	if _, err = f.Seek(r.After, 0); err != nil {
		return page, err
	}
	reader := bufio.NewReaderSize(f, 128*1024)
	partial := false
	for len(page.Lines) < 200 && page.Cursor-r.After < 128*1024 {
		b, e := reader.ReadBytes('\n')
		if e == io.EOF {
			partial = len(b) > 0
			break
		}
		if e != nil {
			return page, e
		}
		var line lnmpinstall.Line
		if err = json.Unmarshal(b, &line); err != nil {
			return page, errors.New("安装日志记录损坏")
		}
		page.Cursor += int64(len(b))
		line.ID = page.Cursor
		page.Lines = append(page.Lines, line)
	}
	page.More = !partial && page.Cursor < info.Size()
	return page, nil
}

// HandleLNMPInstall is the only sudo-authorized installation entry point.
func HandleLNMPInstall(ctx context.Context, action string, input io.Reader, output io.Writer) error {
	p := defaultInstaller()
	var result any
	var err error
	decode := func(v any) error {
		d := json.NewDecoder(io.LimitReader(input, 8193))
		d.DisallowUnknownFields()
		if e := d.Decode(v); e != nil {
			return errors.New("安装请求无效或过大")
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return errors.New("安装请求存在多余内容")
		}
		return nil
	}
	switch action {
	case "inspect":
		var r struct{}
		if err = decode(&r); err == nil {
			result, err = p.inspect(ctx)
		}
	case "prepare":
		var r struct {
			ID string `json:"id"`
		}
		if err = decode(&r); err == nil {
			result, err = p.prepare(ctx, r.ID)
		}
	case "start":
		var r lnmpinstall.Request
		if err = decode(&r); err == nil {
			result, err = p.start(ctx, r)
		}
	case "read":
		var r lnmpinstall.ReadRequest
		if err = decode(&r); err == nil {
			result, err = p.read(r)
		}
	default:
		err = errors.New("安装操作不受支持")
	}
	if err != nil {
		result = struct {
			Error    string `json:"error"`
			Conflict bool   `json:"conflict"`
		}{err.Error(), errors.Is(err, lnmpinstall.ErrConflict)}
	}
	return json.NewEncoder(output).Encode(result)
}

func installerCommand(ctx context.Context, dir, program string, env []string, emit func(string), args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	configureProcess(cmd)
	cmd.Dir = dir
	cmd.Env = append([]string{"HOME=/root", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "TERM=dumb", "DEBIAN_FRONTEND=noninteractive", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}, env...)
	w := &installOutput{emit: emit}
	cmd.Stdout, cmd.Stderr = w, w
	err := cmd.Run()
	w.flush()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type installOutput struct {
	mu       sync.Mutex
	buf      []byte
	emit     func(string)
	overlong bool
}

func (w *installOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(b)
	for _, c := range b {
		if c == '\n' || c == '\r' {
			if w.overlong {
				w.emit("[超过 64 KiB 的单行输出已省略]")
			} else if len(w.buf) > 0 {
				w.emit(string(w.buf))
			}
			w.buf = nil
			w.overlong = false
		} else {
			if w.overlong {
				continue
			}
			w.buf = append(w.buf, c)
			if len(w.buf) >= 64*1024 {
				w.buf = nil
				w.overlong = true
			}
		}
	}
	return n, nil
}
func (w *installOutput) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.overlong {
		w.emit("[超过 64 KiB 的单行输出已省略]")
		w.overlong = false
	} else if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

var ansiInstall = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func sanitizeInstallLine(line string, secrets []string) string {
	for _, s := range secrets {
		if s != "" {
			line = strings.ReplaceAll(line, s, "[已隐藏]")
		}
	}
	line = ansiInstall.ReplaceAllString(strings.ToValidUTF8(line, ""), "")
	lower := strings.ToLower(line)
	if strings.Contains(lower, "password") || strings.Contains(lower, "passwd") || strings.Contains(lower, "private key") {
		return "[敏感输出已隐藏]"
	}
	return strings.Map(func(r rune) rune {
		if r == '\t' || r >= 32 && r != 127 {
			return r
		}
		return -1
	}, line)
}

func (p rootInstaller) git(ctx context.Context, emit func(string), args ...string) error {
	return installerCommand(ctx, "/root", "/usr/bin/git", nil, emit, args...)
}
func (p rootInstaller) gitValue(ctx context.Context, args ...string) (string, error) {
	var lines []string
	err := p.git(ctx, func(s string) { lines = append(lines, s) }, args...)
	return strings.Join(lines, "\n"), err
}
func (p rootInstaller) verifySource(ctx context.Context, revision string) error {
	if err := TrustedPath(p.repo); err != nil {
		return errors.New("源码目录权限不安全")
	}
	head, err := p.gitValue(ctx, "-C", p.repo, "rev-parse", "HEAD")
	if err != nil || head != revision {
		return errors.New("源码提交与安装表单不一致，请重新准备")
	}
	dirty, err := p.gitValue(ctx, "-C", p.repo, "status", "--porcelain")
	if err != nil || dirty != "" {
		return errors.New("源码目录存在修改或残留，请管理员处理后重新准备")
	}
	return nil
}
func (p rootInstaller) prepareSource(ctx context.Context, j *lnmpinstall.Job, emit func(string)) error {
	if !executable("/usr/bin/git") {
		emit("正在安装源码拉取工具 git")
		pm := firstExecutable("/usr/bin/apt", "/usr/bin/dnf", "/bin/dnf", "/usr/bin/yum", "/bin/yum")
		if pm == "" || TrustedPath(pm) != nil {
			return errors.New("自动安装 git 需要可信的 apt、dnf 或 yum")
		}
		if filepath.Base(pm) == "apt" {
			if err := installerCommand(ctx, "/root", pm, nil, emit, "update"); err != nil {
				return errors.New("安装 git 前更新软件包索引失败")
			}
		}
		if err := installerCommand(ctx, "/root", pm, nil, emit, "install", "-y", "git"); err != nil {
			return errors.New("安装 git 失败，请查看输出")
		}
	}
	if err := TrustedPath("/usr/bin/git"); err != nil {
		return errors.New("请先安装可信的 git 程序")
	}
	marker := filepath.Join(p.state, "repository")
	if _, err := os.Lstat(p.repo); os.IsNotExist(err) {
		if err = p.git(ctx, emit, "clone", "--progress", "--depth=1", "--branch=main", lnmpRepository, p.repo); err != nil {
			return errors.New("LNMP 源码拉取失败，请查看输出")
		}
		if err = atomicInstallFile(marker, []byte(p.repo), 0600); err != nil {
			return err
		}
	} else {
		if err != nil {
			return err
		}
		if err = TrustedPath(marker); err != nil {
			return errors.New("源码目录已存在且未经本功能登记，请管理员移开后重试")
		}
		if err = TrustedPath(p.repo); err != nil {
			return err
		}
		origin, e := p.gitValue(ctx, "-C", p.repo, "remote", "get-url", "origin")
		if e != nil || origin != lnmpRepository {
			return errors.New("已有源码仓库来源不符")
		}
		dirty, e := p.gitValue(ctx, "-C", p.repo, "status", "--porcelain")
		if e != nil || dirty != "" {
			return errors.New("源码目录存在修改或残留，请管理员处理后重新准备")
		}
		if err = p.git(ctx, emit, "-C", p.repo, "fetch", "--progress", "--depth=1", "origin", "main"); err != nil {
			return errors.New("更新 LNMP 源码失败")
		}
		if err = p.git(ctx, emit, "-C", p.repo, "checkout", "--detach", "FETCH_HEAD"); err != nil {
			return err
		}
	}
	rev, err := p.gitValue(ctx, "-C", p.repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	read := func(path string) (string, error) {
		b, e := os.ReadFile(filepath.Join(p.repo, path))
		return string(b), e
	}
	main, err := read("include/main.sh")
	if err != nil {
		return err
	}
	install, err := read("install.sh")
	if err != nil {
		return err
	}
	security, err := read("include/security.sh")
	if err != nil {
		return err
	}
	ip := ""
	if addrs, e := net.InterfaceAddrs(); e == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				ip = n.IP.String()
				break
			}
		}
	}
	opts, err := lnmpinstall.ParseOptions(main, install, security, rev, runtime.GOARCH, ip)
	if err != nil {
		return err
	}
	j.Options = &opts
	emit("源码准备完成，提交：" + rev)
	return nil
}

func yesNo(b bool) string {
	if b {
		return "y"
	}
	return "n"
}
func installEnvironment(r lnmpinstall.Request) []string {
	return []string{"LNMP_Auto=y", "DBSelect=" + strconv.Itoa(r.Database), "Bin=" + yesNo(r.Binary), "DB_Root_Password=" + r.DatabasePassword, "InstallInnodb=" + yesNo(r.InnoDB), "PHPSelect=" + strconv.Itoa(r.PHP), "SelectMalloc=" + strconv.Itoa(r.Allocator), "PhpMyAdmin_Mode=" + r.Protection, "PhpMyAdmin_Auth_User=" + r.AuthUser, "PhpMyAdmin_Auth_Password=" + r.AuthPassword, "Default_Server_IP=" + r.IPv4}
}
func moduleConfig(original string, paths []string) (string, error) {
	for _, key := range []string{"Nginx_Modules_Options", "Enable_Ngx_FancyIndex"} {
		if !regexp.MustCompile(`(?m)^` + key + `=.*$`).MatchString(original) {
			return "", errors.New("上游 Nginx 模块配置结构不兼容")
		}
	}
	var flags []string
	for _, path := range paths {
		flags = append(flags, "--add-module="+path)
	}
	out := regexp.MustCompile(`(?m)^Nginx_Modules_Options=.*$`).ReplaceAllString(original, "Nginx_Modules_Options='"+strings.Join(flags, " ")+"'")
	out = regexp.MustCompile(`(?m)^Enable_Ngx_FancyIndex=.*$`).ReplaceAllString(out, "Enable_Ngx_FancyIndex='n'")
	return out, nil
}
func (p rootInstaller) install(ctx context.Context, j *lnmpinstall.Job, r lnmpinstall.Request, emit func(string), phase func(string)) error {
	release, err := LockNginx()
	if err != nil {
		return err
	}
	defer release()
	if installedLNMP() {
		return errors.New("LNMP 已安装，禁止重复安装")
	}
	if b := installBlockers(); len(b) > 0 {
		return errors.New(strings.Join(b, "；"))
	}
	if err = checkInstallPorts(r.Database != 0); err != nil {
		return err
	}
	if err = r.Validate(*j.Options); err != nil {
		return err
	}
	if err = p.verifySource(ctx, r.Revision); err != nil {
		return err
	}
	paths := []string{}
	j.ModuleRevisions = map[string]string{}
	selected := map[string]bool{}
	for _, id := range r.Modules {
		selected[id] = true
	}
	if len(selected) > 0 {
		if err = os.Mkdir(filepath.Join(p.repo, "panel-modules"), 0700); err != nil {
			return err
		}
	}
	for _, m := range lnmpinstall.Modules {
		if !selected[m.ID] {
			continue
		}
		phase("模块准备：" + m.Name)
		dest := filepath.Join(p.repo, "panel-modules", m.ID)
		if err = p.git(ctx, emit, "clone", "--progress", "--depth=1", m.Repository, dest); err != nil {
			return fmt.Errorf("模块 %s 拉取失败", m.Name)
		}
		rev, e := p.gitValue(ctx, "-C", dest, "rev-parse", "HEAD")
		if e != nil {
			return e
		}
		j.ModuleRevisions[m.ID] = rev
		paths = append(paths, dest)
		emit(m.Name + " 提交：" + rev)
	}
	if selected["geoip2"] {
		phase("GeoIP2 依赖安装")
		if apt := firstExecutable("/usr/bin/apt"); apt != "" {
			if err = installerCommand(ctx, p.repo, apt, nil, emit, "update"); err != nil {
				return errors.New("apt 软件包索引更新失败")
			}
			err = installerCommand(ctx, p.repo, apt, nil, emit, "install", "libmaxminddb0", "libmaxminddb-dev", "mmdb-bin", "-y")
		} else if pm := firstExecutable("/usr/bin/dnf", "/bin/dnf", "/usr/bin/yum", "/bin/yum"); pm != "" {
			release, _ := os.ReadFile("/etc/os-release")
			err = installerCommand(ctx, p.repo, pm, nil, emit, maxMindRPMArgs(string(release))...)
		} else {
			return errors.New("GeoIP2 依赖安装需要 apt、dnf 或 yum")
		}
		if err != nil {
			return errors.New("GeoIP2 依赖安装失败，请检查软件源及上方输出")
		}
		if firstExecutable("/usr/bin/mmdblookup", "/bin/mmdblookup") == "" {
			return errors.New("GeoIP2 依赖缺少 mmdblookup")
		}
		// The clean server may not have a compiler yet; LNMP installs it later.
		headers, _ := filepath.Glob("/usr/include/*/maxminddb.h")
		if _, e := os.Stat("/usr/include/maxminddb.h"); e == nil {
			headers = append(headers, "/usr/include/maxminddb.h")
		}
		if len(headers) == 0 {
			return errors.New("GeoIP2 开发头文件缺失")
		}
		if err = installerCommand(ctx, p.repo, firstExecutable("/usr/bin/mmdblookup", "/bin/mmdblookup"), nil, emit, "--version"); err != nil {
			return errors.New("GeoIP2 运行库检查失败")
		}
	}
	conf := filepath.Join(p.repo, "lnmp.conf")
	original, err := os.ReadFile(conf)
	if err != nil {
		return err
	}
	if err = atomicInstallFile(p.path(j.ID, "lnmp.conf.original"), original, 0600); err != nil {
		return err
	}
	modified, err := moduleConfig(string(original), paths)
	if err != nil {
		return err
	}
	if err = atomicInstallFile(conf, []byte(modified), 0644); err != nil {
		return err
	}
	if err = atomicInstallFile(p.path(j.ID, "config.sha256"), []byte(configHash([]byte(modified))), 0600); err != nil {
		return err
	}
	logPath := "/root/lnmp-install.log"
	if _, e := os.Lstat(logPath); e == nil {
		if e = TrustedPath(logPath); e != nil {
			return errors.New("上游日志文件权限不安全")
		}
		if e = os.Chmod(logPath, 0600); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	phase("LNMP 安装与编译")
	completed, failed := false, false
	err = installerCommand(ctx, p.repo, "/bin/bash", installEnvironment(r), func(line string) {
		if strings.Contains(line, "completed! enjoy it.") {
			completed = true
		}
		if strings.Contains(line, "Failed to install LNMP!") {
			failed = true
		}
		if strings.Contains(line, "[+] Installing ") {
			switch {
			case strings.Contains(line, "mysql-") || strings.Contains(line, "mariadb-"):
				phase("数据库安装")
			case strings.Contains(line, "php-"):
				phase("PHP 编译")
			case strings.Contains(line, "nginx-"):
				phase("Nginx 编译")
			}
		}
		emit(line)
	}, "./install.sh", "lnmp")
	if err != nil {
		return errors.New("LNMP 安装脚本执行失败，请查看编译输出")
	}
	phase("安装结果校验")
	if err = checkInstallResult(r, completed, failed, installedLNMP(), func(path string) bool {
		if strings.HasSuffix(path, ".conf") || strings.HasSuffix(path, ".ini") || strings.HasSuffix(path, ".cnf") {
			info, e := os.Stat(path)
			return e == nil && info.Mode().IsRegular() && info.Size() > 0
		}
		return executable(path)
	}); err != nil {
		return err
	}
	if err = installerCommand(ctx, p.repo, "/usr/local/nginx/sbin/nginx", nil, emit, "-t"); err != nil {
		return errors.New("Nginx 配置检查失败")
	}
	var nginxVersion strings.Builder
	if err = installerCommand(ctx, p.repo, "/usr/local/nginx/sbin/nginx", nil, func(s string) { nginxVersion.WriteString(s); emit(s) }, "-V"); err != nil {
		return errors.New("Nginx 编译参数检查失败")
	}
	for _, path := range paths {
		if !strings.Contains(nginxVersion.String(), "--add-module="+path) {
			return errors.New("Nginx 缺少所选模块：" + filepath.Base(path))
		}
	}
	if r.Database != 0 { // Verify credentials without putting the password in argv or process output.
		db, e := openSiteDatabase(ctx, r.DatabasePassword)
		if e != nil {
			return errors.New("数据库登录校验失败，请检查数据库安装输出")
		}
		_ = db.Close()
	}
	if err = p.mergeServices(ctx); err != nil {
		return fmt.Errorf("组件已安装，更新面板服务配置失败：%w", err)
	}
	j.Result = "LNMP 已安装，所选 Nginx 模块已校验。默认站点：https://" + r.IPv4 + "/（使用自签名证书）"
	return nil
}

func checkInstallResult(r lnmpinstall.Request, completed, failed, installed bool, present func(string) bool) error {
	if !completed || failed || !installed {
		return errors.New("安装脚本未确认成功，组件可能未完整安装")
	}
	paths := []string{"/usr/local/nginx/sbin/nginx", "/usr/local/nginx/conf/nginx.conf", "/usr/local/php/bin/php", "/usr/local/php/sbin/php-fpm", "/usr/local/php/etc/php.ini", "/usr/local/php/etc/php-fpm.conf"}
	if r.Database != 0 {
		dir := "/usr/local/mysql"
		if r.Database >= 6 && r.Database != 11 {
			dir = "/usr/local/mariadb"
		}
		paths = append(paths, dir+"/bin/mysql", "/etc/my.cnf")
	}
	for _, path := range paths {
		if !present(path) {
			return errors.New("安装组件或配置缺失：" + path)
		}
	}
	return nil
}

// EL development headers are in the official CRB/PowerTools repository, which
// fresh installations commonly leave disabled. Enable it for this command only.
func maxMindRPMArgs(release string) []string {
	values := map[string]string{}
	for _, line := range strings.Split(release, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = strings.Trim(v, "\"'")
		}
	}
	args := []string{}
	switch values["ID"] {
	case "rocky", "almalinux", "centos":
		major, _, _ := strings.Cut(values["VERSION_ID"], ".")
		if n, _ := strconv.Atoi(major); n >= 9 {
			args = append(args, "--enablerepo=crb")
		} else if n == 8 {
			args = append(args, "--enablerepo=powertools")
		}
	}
	return append(args, "install", "-y", "libmaxminddb", "libmaxminddb-devel")
}
func (p rootInstaller) mergeServices(ctx context.Context) error {
	if err := TrustedPath(p.services); err != nil {
		return err
	}
	existing, err := config.LoadServices(p.services)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range existing {
		seen[s.Name] = true
	}
	for _, s := range Detect(ctx) {
		if !seen[s.Name] {
			existing = append(existing, s)
		}
	}
	data, err := yaml.Marshal(config.Services{Services: existing})
	if err != nil {
		return err
	}
	return atomicInstallFile(p.services, data, 0644)
}

func RunLNMPInstallWorker(mode, id string) error {
	if !lnmpinstall.ValidID(id) || (mode != "prepare" && mode != "install") {
		return errors.New("安装执行参数无效")
	}
	p := defaultInstaller()
	// Only the already recorded current job can be executed. No worker entry is
	// granted in sudoers; systemd launches it through the trusted helper binary.
	j, err := p.current()
	if err != nil {
		return err
	}
	if j == nil || j.ID != id || (mode == "prepare" && j.Status != "preparing") || (mode == "install" && j.Status != "running") {
		return errors.New("安装任务状态不匹配")
	}
	workerLock, err := os.OpenFile(p.path(id, mode+"-worker.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer workerLock.Close()
	if err = unix.Flock(int(workerLock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	defer unix.Flock(int(workerLock.Fd()), unix.LOCK_UN)
	var r lnmpinstall.Request
	if mode == "install" {
		path := p.path(id, "request.json")
		if err = TrustedPath(path); err == nil {
			var b []byte
			b, err = os.ReadFile(path)
			if err == nil {
				err = json.Unmarshal(b, &r)
				clear(b)
			}
		}
		_ = os.Remove(path)
		if err == nil && (j.Options == nil || r.ID != id) {
			err = errors.New("安装凭据与任务不匹配")
		}
	}
	f, e := os.OpenFile(p.path(id, "output.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if e != nil {
		j.Status, j.Error = "failed", "无法打开安装日志"
		_ = p.save(j)
		return e
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logErr error
	emit := func(line string) {
		if logErr != nil {
			return
		}
		entry := lnmpinstall.Line{Time: time.Now().UTC(), Phase: j.Phase, Text: sanitizeInstallLine(line, []string{r.DatabasePassword, r.AuthPassword})}
		b, e := json.Marshal(entry)
		if e == nil {
			_, e = f.Write(append(b, '\n'))
		}
		if e != nil {
			logErr = e
			cancel()
		}
	}
	phase := func(s string) {
		j.Phase = s
		if e := p.save(j); e != nil {
			logErr = e
			cancel()
		}
		emit("阶段：" + s)
	}
	if err == nil {
		if mode == "prepare" {
			prepareCtx, done := context.WithTimeout(ctx, 15*time.Minute)
			err = p.prepareSource(prepareCtx, j, emit)
			done()
		} else {
			err = p.install(ctx, j, r, emit, phase)
		}
	}
	if logErr != nil {
		err = errors.New("日志或任务状态写入失败，已终止执行")
	}
	if err != nil {
		j.Status, j.Error = "failed", sanitizeInstallLine(err.Error(), []string{r.DatabasePassword, r.AuthPassword})
		emit("失败：" + j.Error)
	} else if mode == "prepare" {
		j.Status, j.Phase = "ready", "等待安装选项"
	} else {
		j.Status, j.Phase = "succeeded", "安装完成"
		emit(j.Result)
	}
	if e = f.Sync(); e != nil {
		j.Status, j.Error = "failed", "安装日志保存失败，请检查磁盘空间"
	}
	return p.save(j)
}
