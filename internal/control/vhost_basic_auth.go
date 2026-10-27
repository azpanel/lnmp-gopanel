package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func (h Helper) basicAuthCommand(ctx context.Context, action string, input any) (vhosts.BasicAuthState, error) {
	var state vhosts.BasicAuthState
	var helperErr error
	err := h.vhostCommand(ctx, action, input, func(d *json.Decoder) error { helperErr = readVhostResponse(d, &state); return nil })
	if helperErr != nil {
		return state, helperErr
	}
	return state, err
}
func (h Helper) ReadVhostBasicAuth(ctx context.Context, name string) (vhosts.BasicAuthState, error) {
	return h.basicAuthCommand(ctx, "basic-auth-get", map[string]string{"name": name})
}
func (h Helper) SetVhostBasicAuth(ctx context.Context, r vhosts.BasicAuthInput) (vhosts.BasicAuthState, error) {
	return h.basicAuthCommand(ctx, "basic-auth-set", r)
}
func (h Helper) DisableVhostBasicAuth(ctx context.Context, name string) (vhosts.BasicAuthState, error) {
	return h.basicAuthCommand(ctx, "basic-auth-disable", map[string]string{"name": name})
}

var sha512Password = regexp.MustCompile(`^\$6\$[./a-zA-Z0-9]{1,16}\$[./a-zA-Z0-9]{86}$`)

func hashBasicPassword(ctx context.Context, password string) (string, error) {
	if err := TrustedPath("/usr/bin/openssl"); err != nil {
		return "", errors.New("需要可信的 /usr/bin/openssl 才能生成认证凭证")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/openssl", "passwd", "-6", "-stdin")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = strings.NewReader(password + "\n")
	cmd.Stderr = io.Discard
	configureProcess(cmd)
	out, err := cmd.Output()
	hash := strings.TrimSpace(string(out))
	if err != nil || !sha512Password.MatchString(hash) {
		return "", errors.New("生成认证密码哈希失败")
	}
	return hash, nil
}

type authDumpBuffer struct {
	data     []byte
	overflow bool
}

func (b *authDumpBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := 8*vhosts.MaxConfig - len(b.data)
	if len(p) > left {
		b.overflow = true
		p = p[:left]
	}
	b.data = append(b.data, p...)
	return n, nil
}

// -T reveals the effective main config path, including custom compiled paths.
// Only the path is retained; no dump, hash or configuration enters logs/errors.
func nginxMainConfig(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-T")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	configureProcess(cmd)
	out := &authDumpBuffer{}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || out.overflow {
		return "", errors.New("无法读取 Nginx 有效配置，请检查 nginx -t")
	}
	const prefix = "# configuration file "
	for _, line := range strings.Split(string(out.data), "\n") {
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, ":") {
			path := strings.TrimSuffix(strings.TrimPrefix(line, prefix), ":")
			if filepath.IsAbs(path) {
				return path, nil
			}
			break
		}
	}
	return "", errors.New("无法定位 Nginx 主配置")
}

type basicAuthController struct {
	store         vhosts.Store
	trust         func(string) error
	lock          func() (func(), error)
	global        func(context.Context) (string, error)
	hash          func(context.Context, string) (string, error)
	check, reload func(context.Context) error
}

func RootVhostBasicAuth(ctx context.Context, action string, r vhosts.BasicAuthInput, binary string) (vhosts.BasicAuthState, error) {
	if !vhosts.ValidName(r.Name) {
		return vhosts.BasicAuthState{}, errors.New("配置文件名无效")
	}
	if err := TrustedPath(binary); err != nil {
		return vhosts.BasicAuthState{}, errors.New("Nginx 程序权限不安全")
	}
	p := basicAuthController{store: vhosts.DefaultStore(), trust: TrustedPath, lock: LockNginx, hash: hashBasicPassword}
	p.global = func(ctx context.Context) (string, error) {
		path, err := nginxMainConfig(ctx, binary)
		if err != nil {
			return "", err
		}
		return p.read(path, vhosts.MaxConfig)
	}
	command := func(args ...string) func(context.Context) error {
		return func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			_, err := RunCommand(ctx, binary, args...)
			return err
		}
	}
	p.check = command("-t")
	p.reload = command("-s", "reload")
	return p.execute(ctx, action, r)
}
func (p basicAuthController) read(path string, limit int64) (string, error) {
	if err := p.trust(path); err != nil {
		return "", errors.New("认证相关配置路径权限不安全或不存在")
	}
	data, err := readTrustedFileForAuth(path, limit)
	return string(data), err
}
func readTrustedFileForAuth(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("认证相关文件不可读取")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("认证相关文件类型无效或过大")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("认证相关文件读取失败或过大")
	}
	return data, nil
}
func (p basicAuthController) inspect(ctx context.Context, name string) (state vhosts.BasicAuthState, original, base, credential string, err error) {
	state = vhosts.BasicAuthState{Name: name, Status: "unknown"}
	if err = p.trust(p.store.Directory); err != nil {
		return state, "", "", "", errors.New("vhost 目录权限不安全")
	}
	view, e := p.store.Read(name)
	if e != nil {
		return state, "", "", "", e
	}
	original = view.Content
	state.SiteEnabled = view.Filename == name
	if err = p.trust(filepath.Join(p.store.Directory, view.Filename)); err != nil {
		return state, "", "", "", errors.New("站点配置权限不安全")
	}
	unknown := func(reason string) (vhosts.BasicAuthState, string, string, string, error) {
		state.Message = reason
		return state, original, base, credential, nil
	}
	base, credential, e = p.store.StripBasicAuth(name, original)
	if e != nil {
		return unknown(e.Error())
	}
	read := func(path string) (string, error) { return p.read(path, vhosts.MaxConfig) }
	conflict, e := p.store.InspectAuthDirectives(ctx, base, false, read)
	if e != nil {
		return unknown(e.Error())
	}
	if conflict != "" {
		state.Status = "external"
		return unknown(conflict)
	}
	main, e := p.global(ctx)
	if e != nil {
		return unknown(e.Error())
	}
	conflict, e = p.store.InspectAuthDirectives(ctx, main, true, read)
	if e != nil {
		return unknown(e.Error())
	}
	if conflict != "" {
		state.Status = "external"
		return unknown(conflict)
	}
	probe := filepath.Join(p.store.BasicAuthDirectory(), vhosts.BasicAuthKey(name)+"-"+strings.Repeat("0", 32)+".htpasswd")
	if _, e = p.store.AttachBasicAuth(name, base, probe); e != nil {
		return unknown(e.Error())
	}
	state.Status = "disabled"
	if credential != "" {
		info, e := os.Lstat(credential)
		if e != nil || !info.Mode().IsRegular() {
			state.Status = "unknown"
			return unknown("认证凭证文件不存在或不是普通文件")
		}
		data, e := p.read(credential, 4096)
		if e != nil {
			state.Status = "unknown"
			return unknown(e.Error())
		}
		parts := strings.Split(strings.TrimSuffix(data, "\n"), ":")
		if len(parts) != 2 || !sha512Password.MatchString(parts[1]) || (vhosts.BasicAuthInput{Name: name, Username: parts[0], Password: "validation-only"}).Validate() != nil {
			state.Status = "unknown"
			return unknown("认证凭证格式异常")
		}
		state.Status = "enabled"
		state.Managed = true
		state.Username = parts[0]
	}
	return state, original, base, credential, nil
}
func (p basicAuthController) execute(ctx context.Context, action string, r vhosts.BasicAuthInput) (vhosts.BasicAuthState, error) {
	if !vhosts.ValidName(r.Name) {
		return vhosts.BasicAuthState{}, errors.New("配置文件名无效")
	}
	if action != "basic-auth-get" && action != "basic-auth-set" && action != "basic-auth-disable" {
		return vhosts.BasicAuthState{}, errors.New("认证操作不受支持")
	}
	if action == "basic-auth-set" {
		if err := r.Validate(); err != nil {
			return vhosts.BasicAuthState{}, err
		}
	}
	release, err := p.lock()
	if err != nil {
		return vhosts.BasicAuthState{}, err
	}
	defer release()
	state, original, base, _, err := p.inspect(ctx, r.Name)
	if err != nil || action == "basic-auth-get" {
		return state, err
	}
	if !state.SiteEnabled {
		return state, errors.New("站点已停用，请启用后修改认证")
	}
	if state.Status == "external" || state.Status == "unknown" {
		return state, errors.New(state.Message)
	}
	if action == "basic-auth-disable" && state.Status == "disabled" {
		state.Message = "访问认证已关闭"
		return state, nil
	}
	content := base
	newFile := ""
	keepCredential := false
	if action == "basic-auth-set" {
		hash, e := p.hash(ctx, r.Password)
		if e != nil {
			return state, e
		}
		if !sha512Password.MatchString(hash) {
			return state, errors.New("认证密码哈希无效")
		}
		dir := p.store.BasicAuthDirectory()
		if e = os.MkdirAll(dir, 0755); e != nil {
			return state, errors.New("无法创建认证目录")
		}
		info, statErr := os.Lstat(dir)
		if statErr != nil || !info.IsDir() {
			return state, errors.New("认证目录不是普通目录")
		}
		if e = p.trust(dir); e != nil {
			return state, errors.New("认证目录权限不安全")
		}
		nonce := make([]byte, 16)
		if _, e = rand.Read(nonce); e != nil {
			return state, errors.New("无法生成凭证文件名")
		}
		newFile = filepath.Join(dir, vhosts.BasicAuthKey(r.Name)+"-"+hex.EncodeToString(nonce)+".htpasswd")
		f, e := os.OpenFile(newFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			return state, errors.New("无法创建认证凭证")
		}
		_, e = f.WriteString(r.Username + ":" + hash + "\n")
		if e == nil {
			e = f.Chmod(0644)
		}
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			_ = os.Remove(newFile)
			return state, errors.New("写入认证凭证失败")
		}
		// If rollback cannot restore the config, keep every referenced credential.
		defer func() {
			if view, e := p.store.Read(r.Name); !keepCredential && e == nil && !strings.Contains(view.Content, newFile) {
				_ = os.Remove(newFile)
			}
		}()
		content, e = p.store.AttachBasicAuth(r.Name, base, newFile)
		if e != nil {
			return state, e
		}
	}
	if err = p.store.Install(ctx, r.Name, []byte(content), []byte(original), p.check, p.reload); err != nil {
		keepCredential = errors.Is(err, vhosts.ErrInstallUncertain)
		return state, err
	}
	// Keep the previous immutable credential for graceful workers still handling
	// requests under the old configuration. Subsequent reloads never rewrite it.
	state.Status = "disabled"
	state.Managed = false
	state.Username = ""
	state.Message = "访问认证已关闭，Nginx 已重载"
	if action == "basic-auth-set" {
		state.Status = "enabled"
		state.Managed = true
		state.Username = r.Username
		state.Message = "访问认证已更新，Nginx 已重载"
	}
	return state, nil
}
