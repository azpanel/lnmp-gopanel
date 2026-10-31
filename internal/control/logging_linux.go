//go:build linux

package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"golang.org/x/sys/unix"
)

const loggingPHPConfig = "/usr/local/php/etc/php-fpm.conf"
const loggingPHPBinary = "/usr/local/php/sbin/php-fpm"
const loggingDBConfig = "/etc/my.cnf"

type loggingController struct {
	root    string
	trust   func(string) error
	run     CommandRunner
	execute func(context.Context, config.Service, string) (Result, error)
	lookup  func(string) (int, int, error)
	group   func(string) (int, error)
	lock    func(string) (func(), error)
	write   func(string, []byte, os.FileMode, int) error
}
type loggingSource struct {
	path string
	file managementFile
}
type loggingPlan struct {
	service  config.Service
	sources  []loggingSource
	target   int
	data     []byte
	uid, gid int
	path     string
}

func defaultLoggingController() loggingController {
	return loggingController{trust: TrustedPath, run: RunCommand, execute: func(ctx context.Context, s config.Service, action string) (Result, error) {
		ctx, cancel := context.WithTimeout(ctx, 28*time.Second)
		defer cancel()
		return ExecuteManaged(ctx, s, action, RunCommand)
	}, lookup: loggingAccount, group: loggingGroup, lock: LockService, write: writeManagementFile}
}
func loggingAccount(name string) (int, int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, fmt.Errorf("找不到运行账号 %s", name)
	}
	uid, e1 := strconv.Atoi(u.Uid)
	gid, e2 := strconv.Atoi(u.Gid)
	if e1 != nil || e2 != nil || uid <= 0 {
		return 0, 0, errors.New("日志运行账号无效")
	}
	return uid, gid, nil
}
func loggingGroup(name string) (int, error) {
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("找不到用户组 %s", name)
	}
	return strconv.Atoi(group.Gid)
}
func (p loggingController) path(path string) string {
	if p.root != "" {
		return filepath.Join(p.root, path)
	}
	return path
}
func (p loggingController) read(path string) (managementFile, error) {
	return readManagementFile(p.path(path), false, p.trust)
}
func (p loggingController) services() ([]config.Service, error) {
	if err := p.trust(p.path(nginxManagementConfig)); err != nil {
		return nil, errors.New("服务配置权限不安全")
	}
	return config.LoadServices(p.path(nginxManagementConfig))
}
func loggingService(services []config.Service, kind string) (config.Service, error) {
	var found []config.Service
	for _, s := range services {
		if kind == "php" && s.Name == "php-fpm" || kind == "database" && (s.Name == "mysql" || s.Name == "mariadb") {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return config.Service{}, errors.New("未配置对应服务")
	}
	if len(found) != 1 {
		return config.Service{}, errors.New("存在多个数据库服务，无法自动选择")
	}
	return found[0], nil
}

// Read INI values without losing the original lines used by the patcher.
func loggingValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > 1 && (raw[0] == '\'' || raw[0] == '"') {
		if at := strings.IndexByte(raw[1:], raw[0]); at >= 0 {
			return raw[1 : at+1]
		}
	}
	for i, ch := range raw {
		if (ch == ';' || ch == '#') && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
			return strings.TrimSpace(raw[:i])
		}
	}
	return raw
}
func loggingLine(line string) (key, value string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return "", ""
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return strings.ToLower(strings.ReplaceAll(line, "-", "_")), "1"
	}
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_")), loggingValue(value)
}
func loggingSection(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return "", false
	}
	end := strings.IndexByte(line, ']')
	if end < 1 {
		return "", false
	}
	tail := strings.TrimSpace(line[end+1:])
	if tail != "" && !strings.HasPrefix(tail, "#") && !strings.HasPrefix(tail, ";") {
		return "", false
	}
	return strings.TrimSpace(line[1:end]), true
}
func loggingOptions(data []byte, section string) map[string]string {
	out := map[string]string{}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		if next, ok := loggingSection(line); ok {
			current = next
			continue
		}
		if current == section {
			key, value := loggingLine(line)
			if key != "" {
				out[key] = value
			}
		}
	}
	return out
}
func patchLoggingSection(data []byte, section string, values map[string]string) ([]byte, error) {
	newline := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		newline = "\r\n"
	}
	lines := strings.SplitAfter(string(data), "\n")
	current := ""
	seen := false
	var out strings.Builder
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, line := range lines {
		if next, ok := loggingSection(line); ok {
			current = next
			if current == section {
				if seen {
					return nil, fmt.Errorf("配置段 [%s] 重复，无法自动修改", section)
				}
				seen = true
				out.WriteString(line)
				if !strings.HasSuffix(line, "\n") {
					out.WriteString(newline)
				}
				for _, key := range keys {
					out.WriteString(key + " = " + values[key] + newline)
				}
				continue
			}
		}
		key, _ := loggingLine(line)
		if current == section {
			if _, replace := values[key]; replace {
				continue
			}
		}
		out.WriteString(line)
	}
	if !seen {
		return nil, fmt.Errorf("找不到配置段 [%s]", section)
	}
	return []byte(out.String()), nil
}

func (p loggingController) phpPlan(s config.Service) (loggingPlan, error) {
	plan := loggingPlan{service: s, target: -1, path: logs.PHP}
	seen := map[string]bool{}
	pools := map[string]bool{}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 8 || seen[path] || len(seen) >= 64 {
			return errors.New("PHP-FPM include 循环、重复或数量过多")
		}
		seen[path] = true
		if !strings.HasPrefix(path, "/usr/local/php/etc/") || filepath.Clean(path) != path {
			return errors.New("PHP-FPM include 不在标准配置目录内")
		}
		file, err := p.read(path)
		if err != nil {
			return fmt.Errorf("无法读取 PHP-FPM 配置：%w", err)
		}
		index := len(plan.sources)
		plan.sources = append(plan.sources, loggingSource{path, file})
		current := ""
		for _, line := range strings.Split(string(file.Data), "\n") {
			if section, ok := loggingSection(line); ok {
				current = section
				if section != "global" {
					if pools[section] {
						return errors.New("PHP-FPM 存在同名进程池，无法自动修改")
					}
					pools[section] = true
				}
				if section == "www" {
					plan.target = index
				}
				continue
			}
			key, value := loggingLine(line)
			if key == "include" {
				if current != "" && current != "global" {
					return errors.New("暂不支持 PHP-FPM 池内 include")
				}
				if !filepath.IsAbs(value) || filepath.Clean(value) != value || !strings.HasPrefix(value, "/usr/local/php/etc/") {
					return errors.New("暂不支持此 PHP-FPM include 路径")
				}
				matches, err := filepath.Glob(p.path(value))
				if err != nil {
					return err
				}
				for _, match := range matches {
					included := match
					if p.root != "" {
						relative, err := filepath.Rel(p.root, match)
						if err != nil {
							return err
						}
						included = "/" + relative
					}
					if err := visit(included, depth+1); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := visit(loggingPHPConfig, 0); err != nil {
		return plan, err
	}
	if plan.target < 0 {
		return plan, errors.New("找不到默认 [www] 进程池")
	}
	opts := loggingOptions(plan.sources[plan.target].file.Data, "www")
	if _, exists := opts["php_admin_value[log_errors]"]; exists {
		return plan, errors.New("www 池存在 php_admin_value[log_errors]，请先合并为 php_admin_flag[log_errors]")
	}
	if opts["user"] != "www" || opts["group"] != "" && opts["group"] != "www" {
		return plan, errors.New("默认 www 池未使用 www 账号，暂不支持自动开启")
	}
	uid, gid, err := p.lookup("www")
	if err != nil {
		return plan, err
	}
	// www:www is intentional for the standard LNMP pool.
	gid, err = p.group("www")
	if err != nil {
		return plan, err
	}
	plan.uid, plan.gid = uid, gid
	plan.data, err = patchLoggingSection(plan.sources[plan.target].file.Data, "www", map[string]string{"php_admin_value[error_log]": logs.PHP, "php_admin_flag[log_errors]": "on"})
	return plan, err
}
func databaseLoggingKey(key string) string {
	switch key {
	case "log_slow_query", "log_slow_queries":
		return "slow_query_log"
	case "log_slow_query_file":
		return "slow_query_log_file"
	case "log_slow_query_time":
		return "long_query_time"
	}
	return key
}
func databaseSection(section string) bool {
	return section == "mysqld" || section == "server" || section == "mariadb" || section == "mariadbd" || strings.HasPrefix(section, "mysqld-") || strings.HasPrefix(section, "mariadb-") || strings.HasPrefix(section, "mariadbd-")
}
func (p loggingController) databasePlan(s config.Service) (loggingPlan, error) {
	plan := loggingPlan{service: s, target: 0, path: logs.MySQL}
	watched := map[string]bool{}
	options := map[string]string{}
	controlled := map[string]bool{"slow_query_log": true, "slow_query_log_file": true, "long_query_time": true, "log_output": true, "user": true}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 8 || watched[path] || len(watched) >= 64 {
			return errors.New("数据库 include 循环、重复或数量过多")
		}
		watched[path] = true
		file, err := p.read(path)
		if err != nil {
			return fmt.Errorf("无法读取数据库配置：%w", err)
		}
		plan.sources = append(plan.sources, loggingSource{path, file})
		section := ""
		for _, line := range strings.Split(string(file.Data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "!") {
				directive, value, ok := strings.Cut(trimmed, " ")
				value = strings.TrimSpace(value)
				if !ok || !filepath.IsAbs(value) || filepath.Clean(value) != value {
					return errors.New("数据库 include 路径无法自动处理")
				}
				switch directive {
				case "!include":
					if err := visit(value, depth+1); err != nil {
						return err
					}
				case "!includedir":
					if err := p.trust(p.path(value)); err != nil {
						return err
					}
					entries, err := os.ReadDir(p.path(value))
					if err != nil {
						return err
					}
					for _, entry := range entries {
						if strings.HasSuffix(entry.Name(), ".cnf") {
							if err := visit(filepath.Join(value, entry.Name()), depth+1); err != nil {
								return err
							}
						}
					}
				default:
					return errors.New("数据库 include 指令无法自动处理")
				}
				continue
			}
			if next, ok := loggingSection(line); ok {
				section = next
				continue
			}
			key, val := loggingLine(line)
			canonical := strings.TrimPrefix(key, "loose_")
			for _, prefix := range []string{"skip_", "disable_", "enable_"} {
				canonical = strings.TrimPrefix(canonical, prefix)
			}
			canonical = databaseLoggingKey(canonical)
			if databaseSection(section) && controlled[canonical] {
				if path != loggingDBConfig || section != "mysqld" || key != canonical {
					return fmt.Errorf("数据库 %s 存在其他配置组、include 或别名设置，请先合并到 /etc/my.cnf 的 [mysqld]", canonical)
				}
				options[key] = val
			}
		}
		return nil
	}
	if err := visit(loggingDBConfig, 0); err != nil {
		return plan, err
	}
	name := options["user"]
	if name == "" {
		name = "mysql"
	}
	uid, gid, err := p.lookup(name)
	if err != nil {
		return plan, err
	}
	plan.uid, plan.gid = uid, gid
	output := "FILE"
	for _, part := range strings.Split(strings.ToUpper(options["log_output"]), ",") {
		if strings.TrimSpace(part) == "TABLE" {
			output = "FILE,TABLE"
		}
	}
	values := map[string]string{"slow_query_log": "1", "slow_query_log_file": logs.MySQL, "log_output": output}
	if options["long_query_time"] == "" {
		values["long_query_time"] = "2"
	}
	plan.data, err = patchLoggingSection(plan.sources[0].file.Data, "mysqld", values)
	return plan, err
}
func (p loggingController) plan(services []config.Service, kind string) (loggingPlan, error) {
	s, err := loggingService(services, kind)
	if err != nil {
		return loggingPlan{}, err
	}
	if err := p.trust(p.path(s.Program)); err != nil {
		return loggingPlan{}, errors.New("服务管理程序权限不安全")
	}
	var plan loggingPlan
	if kind == "php" {
		plan, err = p.phpPlan(s)
	} else {
		plan, err = p.databasePlan(s)
	}
	if err != nil {
		return plan, err
	}
	if kind == "php" {
		if err = p.trust(p.path(loggingPHPBinary)); err != nil {
			return plan, errors.New("配置检查程序权限不安全")
		}
	}
	return plan, nil
}

// Walk with directory descriptors: log directories may belong to www/mysql,
// so path checks alone cannot protect a root chown from a rename/symlink race.
func (p loggingController) logFile(plan loggingPlan, create bool) (*os.File, bool, error) {
	start := "/"
	if p.root != "" {
		start = p.root
	}
	fd, err := unix.Open(start, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, false, err
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(plan.path, "/"), "/")
	for _, part := range parts[:len(parts)-1] {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) && create {
			if e = unix.Mkdirat(fd, part, 0750); e != nil {
				return nil, false, e
			}
			next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if e == nil {
				e = unix.Fchown(next, plan.uid, plan.gid)
				if e != nil {
					unix.Close(next)
				}
			}
		}
		if e != nil {
			return nil, false, e
		}
		var st unix.Stat_t
		if e = unix.Fstat(next, &st); e != nil || (st.Uid != 0 && int(st.Uid) != plan.uid) || st.Mode&0022 != 0 {
			unix.Close(next)
			return nil, false, errors.New("日志目录权限不安全")
		}
		unix.Close(fd)
		fd = next
	}
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if create {
		flags = unix.O_WRONLY | unix.O_CREAT | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	}
	logfd, err := unix.Openat(fd, parts[len(parts)-1], flags, 0640)
	if err != nil {
		return nil, false, err
	}
	f := os.NewFile(uintptr(logfd), plan.path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || (stat.Uid != 0 && int(stat.Uid) != plan.uid) {
		f.Close()
		return nil, false, errors.New("日志文件类型或所有权不安全")
	}
	ready := int(stat.Uid) == plan.uid && int(stat.Gid) == plan.gid && info.Mode().Perm() == 0640
	return f, ready, nil
}
func (p loggingController) snapshot(ctx context.Context, services []config.Service) LoggingSnapshot {
	php, phpErr := p.plan(services, "php")
	db, dbErr := p.plan(services, "database")
	return LoggingSnapshot{PHP: p.view(ctx, "php", php, phpErr), Database: p.view(ctx, "database", db, dbErr)}
}
func (p loggingController) view(ctx context.Context, kind string, plan loggingPlan, err error) LoggingView {
	path := logs.PHP
	if kind == "database" {
		path = logs.MySQL
	}
	v := LoggingView{Kind: kind, Path: path, Service: plan.service.Name}
	if err != nil {
		v.Message = err.Error()
		return v
	}
	v.Available = true
	if state, e := p.execute(ctx, plan.service, "status"); e != nil || state.State != "running" {
		v.Message = "服务未运行或状态无法确认，请先启动服务"
		v.Available = false
		return v
	}
	same := true
	// Compare effective values instead of formatting to recognize manual setup.
	section := "mysqld"
	if kind == "php" {
		section = "www"
	}
	before := loggingOptions(plan.sources[plan.target].file.Data, section)
	after := loggingOptions(plan.data, section)
	keys := []string{"slow_query_log", "slow_query_log_file", "log_output", "long_query_time"}
	if kind == "php" {
		keys = []string{"php_admin_value[error_log]", "php_admin_flag[log_errors]"}
	}
	for _, key := range keys {
		a, b := before[key], after[key]
		if key == "slow_query_log" || key == "php_admin_flag[log_errors]" {
			a = normalizeLoggingBool(a)
			b = normalizeLoggingBool(b)
		}
		if key == "log_output" {
			a = normalizeLogOutput(a)
			b = normalizeLogOutput(b)
		}
		if a != b {
			same = false
		}
	}
	file, ready, e := p.logFile(plan, false)
	if file != nil {
		file.Close()
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		v.Available = false
		v.Message = "无法安全访问日志文件：" + e.Error()
		return v
	}
	v.Configured = same && ready
	if v.Configured {
		v.Message = "已配置，可在日志页面查看"
	} else {
		v.Message = "开启后将重启对应服务"
	}
	return v
}

func normalizeLoggingBool(value string) string {
	switch strings.ToLower(value) {
	case "on", "yes", "true", "1":
		return "1"
	case "off", "no", "false", "0":
		return "0"
	}
	return value
}
func normalizeLogOutput(value string) string {
	parts := strings.Split(strings.ToUpper(value), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
func RootLoggingStatus(ctx context.Context) (LoggingSnapshot, error) {
	p := defaultLoggingController()
	services, err := p.services()
	if err != nil {
		return LoggingSnapshot{}, err
	}
	return p.snapshot(ctx, services), nil
}
func RootEnableLogging(ctx context.Context, kind string) (Result, error) {
	return defaultLoggingController().enable(ctx, kind)
}

func (p loggingController) enable(ctx context.Context, kind string) (result Result, err error) {
	if !validLoggingKind(kind) {
		return result, ErrUnsupported
	}
	lockName := "php-fpm"
	if kind == "database" {
		lockName = "mysql"
	}
	unlock, err := p.lock(lockName)
	if err != nil {
		return result, err
	}
	defer unlock()
	services, err := p.services()
	if err != nil {
		return result, err
	}
	plan, err := p.plan(services, kind)
	if err != nil {
		return result, err
	}
	view := p.view(ctx, kind, plan, nil)
	if view.Configured {
		return Result{State: "running", Message: "日志已配置，无需重复开启"}, nil
	}
	if !view.Available {
		return result, errors.New(view.Message)
	}
	// Private backups survive rollback and never contain log contents.
	backupRoot := p.path("/var/backups/lnmp-gopanel")
	if err = os.MkdirAll(backupRoot, 0700); err != nil {
		return result, err
	}
	if err = p.trust(backupRoot); err != nil {
		return result, err
	}
	backup, err := os.MkdirTemp(backupRoot, "logging-")
	if err != nil {
		return result, err
	}
	original := plan.sources[plan.target]
	if err = os.WriteFile(filepath.Join(backup, filepath.Base(original.path)), original.file.Data, 0600); err != nil {
		return result, err
	}
	// Log files are opened without truncation; keep the descriptor throughout.
	logfile, _, err := p.logFile(plan, true)
	if err != nil {
		return result, fmt.Errorf("日志文件创建失败：%w", err)
	}
	defer logfile.Close()
	oldInfo, err := logfile.Stat()
	if err != nil {
		return result, err
	}
	oldStat := oldInfo.Sys().(*syscall.Stat_t)
	changed, restarted := false, false
	defer func() {
		if err == nil {
			return
		}
		recovery, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		problems := []string{}
		configRestored := true
		if changed {
			latest, e := p.read(original.path)
			if e != nil || !bytes.Equal(latest.Data, plan.data) {
				problems = append(problems, "配置发生并发修改，未覆盖")
				configRestored = false
			} else if e = p.write(p.path(original.path), original.file.Data, original.file.Mode, original.file.GID); e != nil {
				problems = append(problems, "原配置恢复失败")
				configRestored = false
			}
		}
		if e := logfile.Chown(int(oldStat.Uid), int(oldStat.Gid)); e != nil {
			problems = append(problems, "日志所有权恢复失败")
		}
		if e := logfile.Chmod(oldInfo.Mode().Perm()); e != nil {
			problems = append(problems, "日志权限恢复失败")
		}
		if restarted && configRestored {
			if e := p.check(recovery, plan); e != nil {
				problems = append(problems, "原配置检查失败")
			} else if _, e := p.execute(recovery, plan.service, "restart"); e != nil {
				problems = append(problems, "原服务恢复失败")
			} else if e = p.waitRunning(recovery, plan.service); e != nil {
				problems = append(problems, "原服务未恢复运行")
			}
		}
		if len(problems) > 0 {
			err = fmt.Errorf("%w；恢复未完成：%s；备份：%s", err, strings.Join(problems, "、"), backup)
		} else {
			err = fmt.Errorf("%w；已恢复原配置及权限；备份：%s", err, backup)
		}
	}()
	// PHP-FPM validates before and after changes. Database configuration is
	// patched directly; service restart failure uses the same rollback below.
	if err = p.check(ctx, plan); err != nil {
		return result, fmt.Errorf("原配置检查失败：%w", err)
	}
	for _, source := range plan.sources {
		latest, e := p.read(source.path)
		if e != nil || !bytes.Equal(latest.Data, source.file.Data) {
			return result, errors.New("配置发生并发修改，请刷新后重试")
		}
	}
	if err = logfile.Chown(plan.uid, plan.gid); err != nil {
		return result, err
	}
	if err = logfile.Chmod(0640); err != nil {
		return result, err
	}
	if err = p.write(p.path(original.path), plan.data, original.file.Mode, original.file.GID); err != nil {
		return result, fmt.Errorf("配置写入失败：%w", err)
	}
	changed = true
	if err = p.check(ctx, plan); err != nil {
		return result, fmt.Errorf("新配置检查失败：%w", err)
	}
	restarted = true
	if _, err = p.execute(ctx, plan.service, "restart"); err != nil {
		return result, fmt.Errorf("服务重启失败：%w", err)
	}
	if err = p.waitRunning(ctx, plan.service); err != nil {
		return result, err
	}
	return Result{State: "running", Message: "日志已开启，服务已重启"}, nil
}
func (p loggingController) check(ctx context.Context, plan loggingPlan) error {
	if plan.service.Name != "php-fpm" {
		return nil
	}
	task, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	_, err := p.run(task, p.path(loggingPHPBinary), "-t", "-y", p.path(loggingPHPConfig))
	if err != nil {
		return errors.New("配置检查未通过，请检查服务配置")
	}
	return nil
}
func (p loggingController) waitRunning(ctx context.Context, s config.Service) error {
	task, done := context.WithTimeout(ctx, 8*time.Second)
	defer done()
	for {
		result, err := p.execute(task, s, "status")
		if err == nil && result.State == "running" {
			return nil
		}
		select {
		case <-task.Done():
			return errors.New("重启后服务未恢复运行")
		case <-time.After(250 * time.Millisecond):
		}
	}
}
