//go:build linux

package control

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"golang.org/x/sys/unix"
)

const gdriveLatestURL = "https://api.github.com/repos/glotlabs/gdrive/releases/latest"
const rcloneScriptURL = "https://rclone.org/install.sh"

var backupVersionPatterns = map[string]*regexp.Regexp{
	"gdrive": regexp.MustCompile(`(?m)^gdrive v?([0-9]+\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))\s*$`),
	"rclone": regexp.MustCompile(`(?m)^rclone v([0-9]+\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))\s*$`),
}

func backupVersion(name, out string) (string, error) {
	if p := backupVersionPatterns[name]; p != nil {
		if m := p.FindStringSubmatch(strings.TrimSpace(out)); len(m) == 2 && len(m[1]) <= 128 {
			return m[1], nil
		}
	}
	return "", errors.New("无法识别组件版本号")
}

// Both supported CLIs expose a version subcommand; gdrive disables --version.
func readBackupVersion(ctx context.Context, name, program string, run CommandRunner) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := run(ctx, program, "version")
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("组件版本读取失败或超时")
		}
		return "", fmt.Errorf("组件无法运行：%w；%s", err, backupDiagnostic(strings.TrimSpace(out)))
	}
	return backupVersion(name, out)
}
func probeBackup(ctx context.Context, name, arch string) backups.Item {
	item := backups.Item{Name: name, Status: "missing", Supported: arch == "amd64" || (name == "rclone" && arch == "arm64")}
	if !item.Supported {
		item.Error = "当前架构没有受支持的安装包"
	}
	program := firstExecutable("/usr/local/bin/"+name, "/usr/bin/"+name)
	if program == "" {
		return item
	}
	item.Status = "failed"
	if TrustedPath(program) != nil {
		item.Error = "组件可执行文件权限不安全"
		return item
	}
	var err error
	item.Version, err = readBackupVersion(ctx, name, program, componentCommand)
	if err != nil {
		item.Error = backupDiagnostic(err.Error())
		return item
	}
	item.Status = "ready"
	if item.Supported {
		item.Error = ""
	}
	return item
}

type backupInstaller struct {
	state, helper, arch string
	accountHome         string
	run                 CommandRunner
	probe               func(context.Context, string, string) backups.Item
	install             func(context.Context, string, string) error
}

func defaultBackupInstaller() backupInstaller {
	return backupInstaller{state: "/var/lib/lnmp-gopanel-backups", helper: "/usr/local/libexec/lnmp-gopanel-control", arch: runtime.GOARCH,
		accountHome: "/root", run: RunCommand, probe: probeBackup, install: installBackupTool}
}
func (p backupInstaller) lock() (func(), error) {
	if err := os.MkdirAll(p.state, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(p.state)
	if err != nil || !info.IsDir() || TrustedPath(p.state) != nil {
		return nil, errors.New("备份组件状态目录权限不安全")
	}
	if err = os.Chmod(p.state, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(p.state, "lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = TrustedPath(f.Name()); err != nil {
		f.Close()
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
func (p backupInstaller) load() (*backups.Job, error) {
	path := filepath.Join(p.state, "current.json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, nil
	}
	if TrustedPath(path) != nil {
		return nil, errors.New("备份组件任务权限不安全")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var j backups.Job
	if json.NewDecoder(io.LimitReader(f, 16384)).Decode(&j) != nil || !validBackupID(j.ID) {
		return nil, errors.New("备份组件任务记录损坏")
	}
	return &j, nil
}
func (p backupInstaller) save(j *backups.Job) error {
	// Bound persisted diagnostics so helper responses and future reads stay small.
	j.Error = backupDiagnostic(j.Error)
	for i := range j.Items {
		j.Items[i].Error = backupDiagnostic(j.Items[i].Error)
	}
	j.UpdatedAt = time.Now().UTC()
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicInstallFile(filepath.Join(p.state, "current.json"), b, 0600)
}
func backupDiagnostic(message string) string {
	runes := []rune(message)
	if len(runes) > 512 {
		return string(runes[:512]) + "…"
	}
	return message
}
func validBackupID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && id == strings.ToLower(id)
}
func backupUnit(id string) string { return "lnmp-gopanel-backups-" + id + ".service" }
func (p backupInstaller) reconcile(ctx context.Context, j *backups.Job) error {
	if !backups.Active(j) || time.Since(j.UpdatedAt) < 30*time.Second {
		return nil
	}
	out, err := p.run(ctx, "/usr/bin/systemctl", "show", "--property=ActiveState,LoadState", backupUnit(j.ID))
	if err != nil && !strings.Contains(out, "LoadState=not-found") {
		return errors.New("无法查询组件安装系统任务状态")
	}
	if strings.Contains(out, "ActiveState=active") || strings.Contains(out, "ActiveState=activating") {
		return nil
	}
	j.Status, j.Error = "failed", "安装系统任务已中断，请重试"
	for i := range j.Items {
		if j.Items[i].Status == "running" || j.Items[i].Status == "pending" {
			j.Items[i].Status, j.Items[i].Error = "failed", j.Error
		}
	}
	return p.save(j)
}
func (p backupInstaller) inspect(ctx context.Context) (backups.Snapshot, error) {
	release, err := p.lock()
	if err != nil {
		return backups.Snapshot{}, err
	}
	j, err := p.load()
	if err == nil {
		err = p.reconcile(ctx, j)
	}
	release()
	if err != nil {
		return backups.Snapshot{}, err
	}
	items := make([]backups.Item, 0, 2)
	for _, name := range backups.Names() {
		item := p.probe(ctx, name, p.arch)
		accounts := inspectBackupAccounts(name, p.accountHome)
		item.Accounts = &accounts
		items = append(items, item)
	}
	return backups.Snapshot{Items: items, Job: j}, nil
}
func (p backupInstaller) start(ctx context.Context, r backups.Request) (backups.Job, error) {
	if err := r.Validate(); err != nil {
		return backups.Job{}, err
	}
	for _, name := range r.Components {
		if !p.probe(ctx, name, p.arch).Supported {
			return backups.Job{}, fmt.Errorf("%w：%s 不支持当前架构", backups.ErrInvalid, name)
		}
	}
	release, err := p.lock()
	if err != nil {
		return backups.Job{}, err
	}
	defer release()
	old, err := p.load()
	if err != nil {
		return backups.Job{}, err
	}
	if err = p.reconcile(ctx, old); err != nil {
		return backups.Job{}, err
	}
	if backups.Active(old) {
		return backups.Job{}, backups.ErrBusy
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return backups.Job{}, err
	}
	j := backups.Job{ID: hex.EncodeToString(id), Status: "running"}
	for _, name := range backups.Names() {
		for _, selected := range r.Components {
			if selected == name {
				j.Items = append(j.Items, backups.Result{Name: name, Status: "pending"})
			}
		}
	}
	if err = p.save(&j); err != nil {
		return j, err
	}
	for _, path := range []string{p.helper, "/usr/bin/systemd-run", "/usr/bin/systemctl"} {
		if err = TrustedPath(path); err != nil {
			break
		}
	}
	if err == nil {
		_, err = p.run(ctx, "/usr/bin/systemd-run", "--quiet", "--collect", "--unit="+backupUnit(j.ID), "--property=Type=exec", "--property=UMask=0077", "--property=StandardInput=null", "--property=StandardOutput=null", "--property=StandardError=journal", "--property=RuntimeMaxSec=22min", "--property=TimeoutStopSec=15s", p.helper, "backup-components-worker", j.ID)
	}
	if err != nil {
		j.Status, j.Error = "failed", "无法启动组件安装服务，请检查 systemd 和辅助程序权限"
		for i := range j.Items {
			j.Items[i].Status, j.Items[i].Error = "failed", j.Error
		}
		if saveErr := p.save(&j); saveErr != nil {
			return j, saveErr
		}
		return j, errors.New(j.Error)
	}
	return j, nil
}
func (p backupInstaller) update(j *backups.Job) error {
	release, err := p.lock()
	if err != nil {
		return err
	}
	defer release()
	old, err := p.load()
	if err != nil {
		return err
	}
	if old == nil || old.ID != j.ID || !backups.Active(old) {
		return errors.New("组件安装任务状态不匹配")
	}
	return p.save(j)
}
func (p backupInstaller) work(ctx context.Context, id string) error {
	if !validBackupID(id) {
		return backups.ErrInvalid
	}
	release, err := p.lock()
	if err != nil {
		return err
	}
	// A separate lifetime lock prevents duplicate workers; metadata locks stay short.
	f, err := os.OpenFile(filepath.Join(p.state, "worker.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		release()
		return err
	}
	defer f.Close()
	if err = TrustedPath(f.Name()); err == nil {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	}
	if err != nil {
		release()
		return err
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	j, err := p.load()
	release()
	if err != nil {
		return err
	}
	if j == nil || j.ID != id || !backups.Active(j) {
		return errors.New("组件安装任务状态不匹配")
	}
	for i := range j.Items {
		item := &j.Items[i]
		if !backups.ValidName(item.Name) {
			return backups.ErrInvalid
		}
		probe := p.probe(ctx, item.Name, p.arch)
		if probe.Status == "ready" {
			item.Status, item.Version = "skipped", probe.Version
		} else {
			item.Status = "running"
			if err = p.update(j); err != nil {
				return err
			}
			installCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			installErr := p.install(installCtx, item.Name, p.state)
			if installCtx.Err() != nil {
				installErr = errors.New("组件安装超时，请重试")
			}
			cancel()
			probe = p.probe(ctx, item.Name, p.arch)
			if installErr != nil {
				item.Status, item.Error = "failed", installErr.Error()
			} else if probe.Status != "ready" {
				item.Status, item.Error = "failed", "安装后未能读取有效版本，请检查组件实际状态"
			} else {
				item.Status, item.Version = "succeeded", probe.Version
			}
		}
		if err = p.update(j); err != nil {
			return err
		}
	}
	j.Status = "succeeded"
	for _, item := range j.Items {
		if item.Status == "failed" {
			j.Status = "failed"
		}
	}
	return p.update(j)
}
func HandleBackupComponents(ctx context.Context, action string, in io.Reader, out io.Writer) error {
	p := defaultBackupInstaller()
	var result any
	var err error
	switch action {
	case "inspect":
		result, err = p.inspect(ctx)
	case "install":
		var r backups.Request
		var extra any
		d := json.NewDecoder(io.LimitReader(in, 4097))
		d.DisallowUnknownFields()
		if d.Decode(&r) != nil || d.Decode(&extra) != io.EOF || r.Validate() != nil {
			err = backups.ErrInvalid
		} else {
			result, err = p.start(ctx, r)
		}
	default:
		err = backups.ErrInvalid
	}
	if err != nil {
		kind := "failed"
		if errors.Is(err, backups.ErrBusy) {
			kind = "busy"
		}
		if errors.Is(err, backups.ErrInvalid) {
			kind = "invalid"
		}
		result = map[string]string{"error": err.Error(), "kind": kind}
	}
	return json.NewEncoder(out).Encode(result)
}
func RunBackupComponentsWorker(id string) error {
	return defaultBackupInstaller().work(context.Background(), id)
}

// Only upstream-owned URLs are constructed here; requests never supply URLs or commands.
func fetchBackup(ctx context.Context, client *http.Client, url string, dst io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "lnmp-gopanel")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("组件下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("组件下载失败（HTTP %d）", resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("组件下载失败：%w", err)
	}
	if n > limit {
		return errors.New("组件下载内容超过大小限制")
	}
	return nil
}
func latestGdriveAsset(ctx context.Context, client *http.Client) (string, error) {
	var data strings.Builder
	if err := fetchBackup(ctx, client, gdriveLatestURL, &data, 2*1024*1024); err != nil {
		return "", err
	}
	return selectGdriveAsset([]byte(data.String()))
}
func selectGdriveAsset(data []byte) (string, error) {
	var r struct {
		Draft      bool `json:"draft"`
		Prerelease bool `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.Unmarshal(data, &r) != nil || r.Draft || r.Prerelease {
		return "", errors.New("gdrive 最新正式版信息无效")
	}
	for _, asset := range r.Assets {
		if asset.Name == "gdrive_linux-x64.tar.gz" && strings.HasPrefix(asset.URL, "https://github.com/glotlabs/gdrive/releases/download/") {
			return asset.URL, nil
		}
	}
	return "", errors.New("gdrive 最新正式版未提供 Linux x64 安装包")
}
func extractGdrive(src io.Reader, dst io.Writer) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return errors.New("gdrive 安装包无效")
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 256*1024*1024))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("gdrive 安装包缺少可执行文件")
		}
		if err != nil {
			return err
		}
		if h.Name != "gdrive" && h.Name != "./gdrive" {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 128*1024*1024 {
			return errors.New("gdrive 安装包中的可执行文件无效")
		}
		_, err = io.CopyN(dst, tr, h.Size)
		return err
	}
}
func installBackupTool(ctx context.Context, name, state string) error {
	return installBackupToolWithClient(ctx, name, state, &http.Client{Timeout: 5 * time.Minute})
}
func installBackupToolWithClient(ctx context.Context, name, state string, client *http.Client) error {
	dir, err := os.MkdirTemp(state, "download-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if name == "rclone" {
		path := filepath.Join(dir, "install.sh")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = fetchBackup(ctx, client, rcloneScriptURL, f, 1024*1024)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return runRcloneScript(ctx, dir, path)
	}
	if name != "gdrive" || runtime.GOARCH != "amd64" {
		return backups.ErrInvalid
	}
	url, err := latestGdriveAsset(ctx, client)
	if err != nil {
		return err
	}
	archive, err := os.CreateTemp(dir, "gdrive-*.tar.gz")
	if err != nil {
		return err
	}
	defer archive.Close()
	if err = fetchBackup(ctx, client, url, archive, 128*1024*1024); err != nil {
		return err
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	binDir := "/usr/local/bin"
	if TrustedPath(binDir) != nil {
		return errors.New("组件安装目录权限不安全")
	}
	binary, err := os.CreateTemp(binDir, ".gdrive-*")
	if err != nil {
		return err
	}
	defer os.Remove(binary.Name())
	err = extractGdrive(archive, binary)
	if err == nil {
		err = binary.Chmod(0755)
	}
	if err == nil {
		err = binary.Sync()
	}
	closeErr := binary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err = readBackupVersion(ctx, "gdrive", binary.Name(), componentCommand); err != nil {
		return fmt.Errorf("下载的 gdrive 验证失败：%w", err)
	}
	return os.Rename(binary.Name(), filepath.Join(binDir, "gdrive"))
}

func runRcloneScript(ctx context.Context, dir, path string) error {
	if TrustedPath("/bin/bash") != nil {
		return errors.New("Bash 程序权限不安全")
	}
	cmd := exec.CommandContext(ctx, "/bin/bash", path)
	configureProcess(cmd)
	cmd.Dir = dir
	// Upstream creates its own mktemp directory. Keep it inside this job so
	// failed downloads/extraction are cleaned too, and never consume /tmp tmpfs.
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/root", "LANG=C", "LC_ALL=C", "TMPDIR=" + dir}
	output := &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 3 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rclone 安装失败，请检查 curl、解压工具和网络：%s", strings.TrimSpace(string(output.data)))
	}
	return nil
}
