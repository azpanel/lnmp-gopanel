package control

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

var ErrPHPConfigInvalid = errors.New("PHP 配置参数无效")
var phpFunctionName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var phpSize = regexp.MustCompile(`(?i)^[0-9]+[KMG]?$`)
var phpConfigKeys = []string{"upload_max_filesize", "post_max_size", "memory_limit", "max_execution_time", "max_input_time", "date.timezone", "display_errors"}

const PHPConfigRequestLimit = 32 * 1024
const phpINIConfig = "/usr/local/php/etc/php.ini"

type PHPConfigField struct {
	Value    string `json:"value"`
	Source   string `json:"source"`
	Editable bool   `json:"editable"`
	Message  string `json:"message,omitempty"`
}
type PHPPoolConfig struct {
	Pool             string            `json:"pool"`
	Source           string            `json:"source"`
	Values           map[string]string `json:"values"`
	DisableFunctions []string          `json:"disable_functions"`
}
type PHPConfigSnapshot struct {
	Revision                 string                    `json:"revision"`
	Path                     string                    `json:"path"`
	Editable                 bool                      `json:"editable"`
	Message                  string                    `json:"message"`
	Fields                   map[string]PHPConfigField `json:"fields"`
	DisableFunctions         []string                  `json:"disable_functions"`
	DisableFunctionsSource   string                    `json:"disable_functions_source"`
	DisableFunctionsEditable bool                      `json:"disable_functions_editable"`
	Pools                    []PHPPoolConfig           `json:"pools"`
}
type PHPConfigRequest struct {
	Revision         string            `json:"revision"`
	Fields           map[string]string `json:"fields"`
	DisableFunctions *[]string         `json:"disable_functions,omitempty"`
}
type PHPConfigResponse struct {
	PHPConfigSnapshot
	Message string `json:"message"`
}
type PHPConfigBackend interface {
	PHPConfig(context.Context) (PHPConfigSnapshot, error)
	UpdatePHPConfig(context.Context, PHPConfigRequest) (PHPConfigResponse, error)
}

func NormalizePHPFunctions(names []string) ([]string, error) {
	if len(names) > 512 {
		return nil, fmt.Errorf("%w：禁用函数数量不能超过 512", ErrPHPConfigInvalid)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if len(name) > 128 || !phpFunctionName.MatchString(name) {
			return nil, fmt.Errorf("%w：函数名 %q 无效", ErrPHPConfigInvalid, name)
		}
		if !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	sort.Strings(out)
	return out, nil
}
func (r PHPConfigRequest) Validate() error {
	if b, err := hex.DecodeString(r.Revision); err != nil || len(b) != 32 {
		return fmt.Errorf("%w：请重新读取配置", ErrPHPConfigInvalid)
	}
	if len(r.Fields) == 0 && r.DisableFunctions == nil {
		return fmt.Errorf("%w：没有提交修改", ErrPHPConfigInvalid)
	}
	for key, value := range r.Fields {
		valid := false
		if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
			return fmt.Errorf("%w：%s 的值无效", ErrPHPConfigInvalid, key)
		}
		switch key {
		case "upload_max_filesize", "post_max_size", "memory_limit":
			if key == "memory_limit" && value == "-1" {
				valid = true
				break
			}
			if phpSize.MatchString(value) {
				number := strings.TrimRight(value, "KkMmGg")
				n, err := strconv.ParseUint(number, 10, 31)
				valid = err == nil && (key != "memory_limit" || n > 0)
			}
		case "max_execution_time", "max_input_time":
			n, err := strconv.ParseInt(value, 10, 32)
			valid = err == nil && strconv.FormatInt(n, 10) == value && (n >= 0 || key == "max_input_time" && n == -1)
		case "date.timezone":
			_, err := time.LoadLocation(value)
			valid = err == nil && value != "Local" && !strings.ContainsAny(value, "\r\n\x00\";${}")
		case "display_errors":
			valid = value == "On" || value == "Off"
		}
		if !valid {
			return fmt.Errorf("%w：%s 的值无效", ErrPHPConfigInvalid, key)
		}
	}
	if r.DisableFunctions != nil {
		_, err := NormalizePHPFunctions(*r.DisableFunctions)
		return err
	}
	return nil
}
func DecodePHPConfigRequest(reader io.Reader) (PHPConfigRequest, error) {
	var r PHPConfigRequest
	data, err := io.ReadAll(io.LimitReader(reader, PHPConfigRequestLimit+1))
	if err != nil || len(data) > PHPConfigRequestLimit {
		return r, ErrPHPConfigInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, ErrPHPConfigInvalid
	}
	return r, r.Validate()
}
func (h Helper) phpConfigCall(ctx context.Context, action string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "php-config", action)
	configureProvisionProcess(cmd)
	cmd.WaitDelay = 35 * time.Second
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = bytes.NewReader(data)
	// Pool overrides may exceed the ordinary service response buffer.
	stdout, stderr := &phpConfigBuffer{}, &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err = cmd.Run(); err != nil {
		var result Result
		if json.Unmarshal(stdout.data, &result) == nil && result.Message != "" {
			if strings.Contains(result.Message, "服务或操作不受支持") {
				return errors.New("PHP 配置辅助程序版本过旧，请同步升级辅助程序和 sudoers 权限")
			}
			return errors.New(result.Message)
		}
		return errors.New("PHP 配置辅助程序不可用，请同步升级辅助程序和 sudoers 权限")
	}
	if stdout.overflow || json.Unmarshal(stdout.data, output) != nil {
		return errors.New("PHP 配置辅助程序返回无效数据")
	}
	return nil
}

type phpConfigBuffer struct {
	data     []byte
	overflow bool
}

func (b *phpConfigBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 1024*1024 - len(b.data)
	if n > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (h Helper) PHPConfig(ctx context.Context) (PHPConfigSnapshot, error) {
	var out PHPConfigSnapshot
	err := h.phpConfigCall(ctx, "get", struct{}{}, &out)
	if err == nil && (out.Path == "" || out.Revision == "" || len(out.Fields) != len(phpConfigKeys)) {
		err = errors.New("PHP 配置辅助程序版本不匹配，请同步升级")
	}
	return out, err
}
func (h Helper) UpdatePHPConfig(ctx context.Context, r PHPConfigRequest) (PHPConfigResponse, error) {
	if err := r.Validate(); err != nil {
		return PHPConfigResponse{}, err
	}
	var out PHPConfigResponse
	err := h.phpConfigCall(ctx, "update", r, &out)
	return out, err
}
func (m *Manager) PHPConfig(ctx context.Context) (PHPConfigSnapshot, error) {
	backend, ok := m.executor.(PHPConfigBackend)
	if !ok {
		return PHPConfigSnapshot{}, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return backend.PHPConfig(ctx)
}
func (m *Manager) UpdatePHPConfig(ctx context.Context, r PHPConfigRequest) (PHPConfigResponse, error) {
	if err := r.Validate(); err != nil {
		return PHPConfigResponse{}, err
	}
	backend, ok := m.executor.(PHPConfigBackend)
	if !ok {
		return PHPConfigResponse{}, ErrUnsupported
	}
	m.mu.RLock()
	lock := m.locks["php-fpm"]
	m.mu.RUnlock()
	if lock == nil {
		return PHPConfigResponse{}, ErrUnsupported
	}
	if !lock.TryLock() {
		return PHPConfigResponse{}, ErrBusy
	}
	defer lock.Unlock()
	m.mu.Lock()
	v := m.views["php-fpm"]
	v.Busy = true
	m.views["php-fpm"] = v
	m.mu.Unlock()
	defer func() { m.mu.Lock(); v := m.views["php-fpm"]; v.Busy = false; m.views["php-fpm"] = v; m.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	result, err := backend.UpdatePHPConfig(ctx, r)
	slog.Info("update PHP configuration", "success", err == nil)
	refresh, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	m.refreshOne(refresh, "php-fpm")
	return result, err
}
