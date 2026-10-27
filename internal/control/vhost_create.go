package control

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

type managedSite struct {
	Request  vhosts.CreateRequest `json:"request"`
	Hash     string               `json:"hash"`
	Database string               `json:"database"`
}

func configHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
func (p siteProvisioner) saveManaged(site managedSite) error {
	if err := os.MkdirAll(p.stateDir, 0700); err != nil {
		return err
	}
	if err := p.trust(p.stateDir); err != nil {
		return err
	}
	path := filepath.Join(p.stateDir, site.Request.Name+".json")
	if _, err := os.Lstat(path); err == nil {
		if err := p.trust(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	site.Request.Database = vhosts.DatabaseInput{}
	site.Request.RequestID = ""
	site.Request.Retry = false
	data, err := json.Marshal(site)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(p.stateDir, ".site-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
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
func (p siteProvisioner) loadManaged(name string) (managedSite, error) {
	var site managedSite
	if !vhosts.ValidName(name) {
		return site, errors.New("站点名称无效")
	}
	path := filepath.Join(p.stateDir, name+".json")
	if err := p.trust(path); err != nil {
		return site, errors.New("站点记录权限不安全或不存在")
	}
	f, err := os.Open(path)
	if err != nil {
		return site, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return site, errors.New("站点记录类型无效")
	}
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return site, errors.New("仅支持重试通过本功能创建的站点")
	}
	if len(data) > 64*1024 || json.Unmarshal(data, &site) != nil || site.Request.Name != name {
		return site, errors.New("站点记录无效")
	}
	return site, nil
}

type siteProvisioner struct {
	store    vhosts.Store
	stateDir string
	trust    func(string) error
	lock     func() (func(), error)
	options  func(context.Context) (vhosts.CreateOptions, error)
	run      CommandRunner
	issue    func(context.Context, vhosts.CreateRequest, string, func(string)) (string, string, error)
	database func(context.Context, string) (*sql.DB, error)
}

func RootCreateVhost(ctx context.Context, r vhosts.CreateRequest, binary string, emit func(vhosts.CreateEvent)) (vhosts.CreateResult, error) {
	p := siteProvisioner{store: vhosts.DefaultStore(), stateDir: managedSites, trust: TrustedPath, lock: LockNginx, options: RootVhostOptions, run: RunCommand, issue: issueSiteCertificate, database: openSiteDatabase}
	return p.create(ctx, r, binary, emit)
}
func (p siteProvisioner) create(ctx context.Context, r vhosts.CreateRequest, binary string, emit func(vhosts.CreateEvent)) (result vhosts.CreateResult, err error) {
	result = vhosts.CreateResult{Name: r.Name, Database: "skipped", SSL: "skipped"}
	if err = r.Validate(); err != nil {
		return result, err
	}
	result.Name = r.Name
	if err = p.trust(binary); err != nil {
		return result, errors.New("Nginx 程序权限不安全")
	}
	if err = p.trust(p.store.Directory); err != nil {
		return result, errors.New("vhost 目录权限不安全")
	}
	progress := func(phase, line string) {
		copy := result
		emit(vhosts.CreateEvent{Phase: phase, Line: line, Result: &copy})
	}
	progress("preflight", "正在检查域名、配置选项和文件")
	var state managedSite
	if r.Retry {
		state, err = p.loadManaged(r.Name)
		if err != nil {
			return result, err
		}
		ssl, id := r.SSL, r.RequestID
		r = state.Request
		r.SSL = ssl
		r.RequestID = id
		r.Retry = true
		r.Database = vhosts.DatabaseInput{}
		result.SiteCreated = true
		result.Database = state.Database
	}
	opts, err := p.options(ctx)
	if err != nil {
		return result, err
	}
	php, err := chooseSiteOptions(r, opts)
	if err != nil {
		return result, err
	}
	cert, key := "", ""
	if r.SSL.Mode == "files" || r.SSL.Mode == "existing" {
		cert, key, err = resolveSiteCertificate(r, opts)
		if err != nil {
			return result, err
		}
	}
	auto := r.SSL.Mode == "letsencrypt" || r.SSL.Mode == "zerossl"
	if auto && !opts.ACMEAvailable {
		return result, errors.New("自动申请 SSL 需要可信的 acme.sh 和 flock，请安装后重试")
	}
	check := func(parent context.Context) error {
		task, done := context.WithTimeout(parent, 8*time.Second)
		defer done()
		out, e := p.run(task, binary, "-t")
		if e != nil {
			progress("nginx", out)
		}
		return e
	}
	reload := func(parent context.Context) error {
		task, done := context.WithTimeout(parent, 8*time.Second)
		defer done()
		_, e := p.run(task, binary, "-s", "reload")
		return e
	}
	store := p.store
	var old []byte
	var authFile string
	if !r.Retry {
		var db *sql.DB
		if r.Database.Enabled {
			db, err = p.database(ctx, r.Database.AdminPassword)
			if err != nil {
				return result, err
			}
			defer db.Close()
			if err = databasePreflight(ctx, db, r.Database.Name); err != nil {
				return result, err
			}
		}
		if err = os.MkdirAll(p.stateDir, 0700); err != nil {
			return result, err
		}
		if err = p.trust(p.stateDir); err != nil {
			return result, err
		}
		content := []byte(vhosts.RenderSite(r, php, "", ""))
		// Record ownership before publishing, so retries never infer ownership from
		// a filename alone. A failed transaction removes only this new record.
		state = managedSite{Request: r, Hash: configHash(content), Database: "skipped"}
		lock, e := p.lock()
		if e != nil {
			return result, e
		}
		err = func() error {
			sites, e := store.List(ctx)
			if e != nil {
				return e
			}
			for _, site := range sites {
				if site.Error != "" {
					return errors.New("现有站点配置无法解析，请修复后再添加")
				}
				for _, domain := range r.Domains() {
					for _, existing := range site.Domains {
						if strings.EqualFold(strings.TrimSuffix(existing, "."), domain) {
							return fmt.Errorf("域名 %s 已绑定到 %s", domain, site.Name)
						}
					}
				}
			}
			for _, path := range []string{filepath.Join(store.Directory, r.Name), filepath.Join(store.Directory, r.Name+".disabled")} {
				if _, e := os.Lstat(path); !os.IsNotExist(e) {
					return errors.New("同名站点配置已存在，不会覆盖")
				}
			}
			if _, e := os.Lstat(filepath.Join(p.stateDir, r.Name+".json")); !os.IsNotExist(e) {
				return errors.New("同名建站记录已存在，请检查原站点")
			}
			if e = prepareSiteRoot(r.Root); e != nil {
				return e
			}
			if r.AccessLog {
				if e = prepareSiteLog(r.LogName); e != nil {
					return e
				}
			}
			if e = p.saveManaged(state); e != nil {
				return e
			}
			if e = store.Install(ctx, r.Name, content, nil, check, reload); e != nil {
				_ = os.Remove(filepath.Join(p.stateDir, r.Name+".json"))
				return e
			}
			return nil
		}()
		lock()
		if err != nil {
			return result, err
		}
		result.SiteCreated = true
		old = content
		progress("http", "HTTP 站点已创建，Nginx 已重载")
		if r.Database.Enabled {
			result.Database = "failed"
			progress("database", "正在创建数据库及同名本机用户")
			if err = createSiteDatabase(ctx, db, r.Database); err != nil {
				return result, err
			}
			result.Database = "created"
			state.Database = "created"
			if err = p.saveManaged(state); err != nil {
				return result, errors.New("数据库已创建，但站点记录更新失败，请检查服务器")
			}
			progress("database", "数据库及本机用户已创建")
		}
	} else {
		view, e := store.Read(r.Name)
		if e != nil {
			return result, e
		}
		old = []byte(view.Content)
		base, credential, authErr := store.StripBasicAuth(r.Name, view.Content)
		if authErr != nil {
			return result, authErr
		}
		authFile = credential
		if view.Filename != r.Name || configHash([]byte(base)) != state.Hash {
			return result, errors.New("站点已停用或配置已修改，无法自动重试 SSL")
		}
	}
	if r.SSL.Mode == "none" {
		result.Message = "站点已创建"
		return result, nil
	}
	result.SSL = "failed"
	progress("ssl", "正在准备 SSL 证书")
	if auto {
		cert, key, err = p.issue(ctx, r, binary, func(line string) { progress("ssl", line) })
		if err != nil {
			return result, err
		}
	}
	lock, err := p.lock()
	if err != nil {
		return result, err
	}
	defer lock()
	baseContent := vhosts.RenderSite(r, php, cert, key)
	content := []byte(baseContent)
	if authFile != "" {
		withAuth, e := store.AttachBasicAuth(r.Name, baseContent, authFile)
		if e != nil {
			return result, e
		}
		content = []byte(withAuth)
	}
	if err = store.Install(ctx, r.Name, content, old, check, reload); err != nil {
		return result, err
	}
	state.Request = r
	state.Hash = configHash([]byte(baseContent))
	if err = p.saveManaged(state); err != nil {
		if e := store.Install(context.WithoutCancel(ctx), r.Name, old, content, check, reload); e != nil {
			return result, errors.New("HTTPS 已部署，但站点记录写入及配置恢复失败，请检查 Nginx")
		}
		return result, errors.New("站点记录更新失败，已恢复原配置")
	}
	result.SSL = "enabled"
	result.Message = "站点与 SSL 已启用"
	progress("ssl", result.Message)
	return result, nil
}

type acmeExitError struct{ Code int }

func (e *acmeExitError) Error() string {
	return "证书申请或部署失败，请查看上方实时输出；HTTP 站点已保留"
}
func (client acmeClient) stream(ctx context.Context, args []string, emit func(string)) error {
	if !executable(client.program()) || client.trust(client.program()) != nil {
		return errors.New("acme.sh 不存在或权限不安全")
	}
	cmd := exec.CommandContext(ctx, client.program(), args...)
	configureProcess(cmd)
	cmd.Env = []string{"HOME=/root", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "NO_COLOR=1"}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	stderr, e := cmd.StderrPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return errors.New("无法启动 acme.sh")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	consume := func(reader io.Reader) {
		defer wg.Done()
		scan := bufio.NewScanner(reader)
		scan.Buffer(make([]byte, 4096), 256*1024)
		for scan.Scan() {
			mu.Lock()
			emit(scan.Text())
			mu.Unlock()
		}
		if scan.Err() != nil {
			mu.Lock()
			emit("证书输出过长或读取中断")
			mu.Unlock()
			_ = cmd.Process.Kill()
		}
	}
	wg.Add(2)
	go consume(stdout)
	go consume(stderr)
	wg.Wait()
	err := cmd.Wait()
	if ctx.Err() != nil {
		return errors.New("证书申请超时或取消")
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return &acmeExitError{Code: exit.ExitCode()}
		}
		return errors.New("证书程序执行中断，请查看实时输出")
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func issueSiteCertificate(ctx context.Context, r vhosts.CreateRequest, binary string, emit func(string)) (string, string, error) {
	client, err := findACMEClient(acmeHomes(), TrustedPath)
	if err != nil {
		return "", "", err
	}
	acmeCertHome := client.certHome()
	// ACME runs as root; never write challenge files into a website's writable
	// .well-known directory, where an application could substitute a symlink.
	for _, path := range []string{vhosts.ChallengeDirectory, filepath.Join(vhosts.ChallengeDirectory, ".well-known"), filepath.Join(vhosts.ChallengeDirectory, ".well-known/acme-challenge")} {
		if err := TrustedPath(filepath.Dir(path)); err != nil {
			return "", "", err
		}
		if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return "", "", err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			return "", "", errors.New("ACME 验证路径不是普通目录")
		}
		if err := TrustedPath(path); err != nil {
			return "", "", err
		}
		if err := os.Chmod(path, 0755); err != nil {
			return "", "", err
		}
	}
	if err := os.MkdirAll(acmeCertHome, 0700); err != nil {
		return "", "", err
	}
	if err := TrustedPath(acmeCertHome); err != nil {
		return "", "", err
	}
	base := client.baseArgs(r.SSL.Mode)
	if err := client.stream(ctx, append(append([]string{}, base...), "--register-account", "-m", r.SSL.Email), emit); err != nil {
		return "", "", err
	}
	args := append(append([]string{}, base...), "--issue", "--webroot", vhosts.ChallengeDirectory, "--keylength", "ec-256")
	for _, domain := range r.Domains() {
		args = append(args, "-d", domain)
	}
	if err := client.stream(ctx, args, emit); err != nil {
		// acme.sh returns 2 when a still-valid existing order is not due. Only
		// reuse files after complete key, lifetime and requested-SAN validation.
		var exit *acmeExitError
		if ctx.Err() != nil || !errors.As(err, &exit) || exit.Code != 2 {
			return "", "", err
		}
		source := filepath.Join(acmeCertHome, r.Domain+"_ecc")
		c, e1 := readTrustedFile(filepath.Join(source, "fullchain.cer"), 4*1024*1024)
		k, e2 := readTrustedFile(filepath.Join(source, r.Domain+".key"), 1024*1024)
		if e1 != nil || e2 != nil {
			return "", "", err
		}
		if _, e := vhosts.CheckCertificate(c, k, r.Domains(), time.Now()); e != nil {
			return "", "", err
		}
		emit("已有证书仍有效，继续部署")
	}
	dir := filepath.Join(managedSSL, r.Domain)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	if err := TrustedPath(dir); err != nil {
		return "", "", err
	}
	cert, key := filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	for _, path := range []string{cert, key} {
		if _, err := os.Lstat(path); err == nil {
			if TrustedPath(path) != nil {
				return "", "", errors.New("证书部署路径权限不安全")
			}
		} else if !os.IsNotExist(err) {
			return "", "", err
		}
	}
	// The renewal hook contains only a trusted administrator-configured nginx
	// binary. It survives panel removal and never contains user-supplied shell.
	if err := TrustedPath("/usr/bin/flock"); err != nil {
		return "", "", errors.New("缺少可信的 flock，无法配置安全续期重载")
	}
	hook := "/usr/bin/flock -w 60 /run/lock/lnmp-gopanel-nginx.lock /bin/sh -c " + shellQuote(shellQuote(binary)+" -t && "+shellQuote(binary)+" -s reload")
	args = append(append([]string{}, base...), "--install-cert", "--ecc", "-d", r.Domain, "--fullchain-file", cert, "--key-file", key, "--reloadcmd", hook)
	if err := client.stream(ctx, args, emit); err != nil {
		return "", "", err
	}
	c, e1 := readTrustedFile(cert, 4*1024*1024)
	k, e2 := readTrustedFile(key, 1024*1024)
	if e1 != nil || e2 != nil {
		return "", "", errors.New("无法读取已部署证书")
	}
	if _, err := vhosts.CheckCertificate(c, k, r.Domains(), time.Now()); err != nil {
		return "", "", err
	}
	// A separate cron uses this cert-home and does not change existing root
	// crontab or existing certificates' renewal settings.
	cron := client.renewalCron()
	if err := writeRootAtomic("/etc/cron.d/lnmp-gopanel-acme", []byte(cron), 0644); err != nil {
		return "", "", errors.New("证书已签发，但自动续期任务配置失败；请检查 cron 后重试")
	}
	return cert, key, nil
}

func writeRootAtomic(path string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	if err := TrustedPath(parent); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		if err := TrustedPath(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(parent, ".gopanel-*")
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

// Root-opened nginx logs require a trusted directory so another account cannot
// substitute a symlink before nginx opens them. Reject unsafe existing directory
// permissions rather than changing a shared directory's permissions implicitly.
func prepareSiteLog(name string) error {
	dir := vhosts.AccessLogDirectory
	if err := TrustedPath(filepath.Dir(dir)); err != nil {
		return errors.New("Nginx 日志目录权限不安全")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := TrustedPath(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if os.IsExist(err) {
		if err = TrustedPath(path); err != nil {
			return errors.New("同名日志文件权限不安全")
		}
		info, e := os.Stat(path)
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("日志路径不是普通文件")
		}
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}
