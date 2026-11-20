//go:build linux

package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type phpConfigController struct {
	loggingController
	attest func(context.Context) (int, error)
}
type phpConfigState struct {
	snapshot PHPConfigSnapshot
	service  config.Service
	sources  []loggingSource
}

func defaultPHPConfigController() phpConfigController {
	p := defaultLoggingController()
	p.run = runPHPConfigCommand
	return phpConfigController{loggingController: p, attest: attestPHPConfigRuntime}
}

func runPHPConfigCommand(ctx context.Context, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/sbin:/usr/local/bin", "LANG=C", "LC_ALL=C"}
	configureProcess(cmd)
	out, stderr := &phpConfigBuffer{}, &phpConfigBuffer{}
	cmd.Stdout, cmd.Stderr = out, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if out.overflow || stderr.overflow {
		return "", errors.New("PHP 配置检查输出过大")
	}
	output := string(out.data) + "\n" + string(stderr.data)
	// PHP may report an invalid INI value on stderr and still exit successfully.
	if strings.Contains(output, "PHP Warning:") || strings.Contains(output, "PHP Parse error:") || strings.Contains(output, "PHP: ") {
		return "", errors.New("PHP 配置包含警告或错误，请通过 SSH 检查")
	}
	return output, err
}
func phpInfoValues(output string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Split(line, " => ")
		if len(parts) >= 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}
func phpINIValues(data []byte) map[string]string {
	values := map[string]string{}
	conditional := false
	for _, line := range strings.Split(string(data), "\n") {
		if section, ok := loggingSection(line); ok {
			section = strings.ToUpper(section)
			conditional = strings.HasPrefix(section, "PATH=") || strings.HasPrefix(section, "HOST=")
			continue
		}
		if !conditional {
			key, value := loggingLine(line)
			if key != "" {
				values[key] = value
			}
		}
	}
	return values
}
func phpFunctions(value string) ([]string, error) {
	if value == "" || value == "no value" {
		return []string{}, nil
	}
	names := strings.FieldsFunc(value, func(ch rune) bool { return ch == ' ' || ch == ',' })
	return NormalizePHPFunctions(names)
}
func (p phpConfigController) info(ctx context.Context) (map[string]string, error) {
	if err := p.trust(p.path(loggingPHPBinary)); err != nil {
		return nil, errors.New("PHP-FPM 程序权限不安全")
	}
	task, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	output, err := p.run(task, p.path(loggingPHPBinary), "-i", "-c", p.path(phpINIConfig))
	if err != nil {
		return nil, fmt.Errorf("无法读取 PHP-FPM 配置：%w", err)
	}
	info := phpInfoValues(output)
	if info["Loaded Configuration File"] != phpINIConfig {
		return nil, errors.New("仅支持加载标准 LNMP php.ini 的 PHP-FPM")
	}
	return info, nil
}
func (p phpConfigController) snapshot(ctx context.Context) (phpConfigState, error) {
	var state phpConfigState
	services, err := p.services()
	if err != nil {
		return state, err
	}
	state.service, err = loggingService(services, "php")
	if err != nil {
		return state, err
	}
	if err = p.trust(p.path(state.service.Program)); err != nil {
		return state, errors.New("PHP-FPM 服务管理程序权限不安全")
	}
	info, err := p.info(ctx)
	if err != nil {
		return state, err
	}
	sources := []loggingSource{}
	add := func(path string) error {
		if !strings.HasPrefix(path, "/usr/local/php/") || filepath.Clean(path) != path {
			return errors.New("PHP 配置来源不在标准 LNMP 目录内")
		}
		file, e := p.read(path)
		if e != nil {
			return fmt.Errorf("无法读取配置 %s：%w", path, e)
		}
		sources = append(sources, loggingSource{path, file})
		return nil
	}
	if err = add(phpINIConfig); err != nil {
		return state, err
	}
	sourceFor := map[string]string{}
	for key := range phpINIValues(sources[0].file.Data) {
		sourceFor[key] = phpINIConfig
	}
	scan := info["Scan this dir for additional .ini files"]
	if scan != "" && scan != "(none)" {
		for _, dir := range filepath.SplitList(scan) {
			if !strings.HasPrefix(dir, "/usr/local/php/") || filepath.Clean(dir) != dir {
				return state, errors.New("PHP INI 扫描目录不在标准 LNMP 目录内")
			}
			// A missing scan directory is valid; never create or write it.
			if _, e := os.Stat(p.path(dir)); os.IsNotExist(e) {
				continue
			}
			if e := p.trust(p.path(dir)); e != nil {
				return state, errors.New("PHP INI 扫描目录权限不安全")
			}
			paths, e := filepath.Glob(p.path(dir) + "/*.ini")
			if e != nil || len(paths) > 128 {
				return state, errors.New("PHP INI 扫描文件过多或路径无效")
			}
			for _, path := range paths {
				if p.root != "" {
					rel, e := filepath.Rel(p.root, path)
					if e != nil {
						return state, e
					}
					path = "/" + rel
				}
				if err = add(path); err != nil {
					return state, err
				}
				for key := range phpINIValues(sources[len(sources)-1].file.Data) {
					sourceFor[key] = path
				}
			}
		}
	}
	view := PHPConfigSnapshot{Path: phpINIConfig, Fields: map[string]PHPConfigField{}, Pools: []PHPPoolConfig{}, DisableFunctions: []string{}}
	for _, key := range phpConfigKeys {
		value, ok := info[key]
		if !ok {
			return state, fmt.Errorf("PHP-FPM 未返回配置项 %s", key)
		}
		if value == "no value" {
			value = ""
		}
		if key == "display_errors" {
			if normalizeLoggingBool(value) == "1" {
				value = "On"
			} else if normalizeLoggingBool(value) == "0" {
				value = "Off"
			}
		}
		source := sourceFor[key]
		editable := source == "" || source == phpINIConfig
		message := ""
		if !editable {
			message = "被扫描 INI 覆盖，请在来源文件中修改"
		}
		if source == "" {
			source = "PHP 默认值"
		}
		view.Fields[key] = PHPConfigField{value, source, editable, message}
	}
	functions, ok := info["disable_functions"]
	if !ok {
		return state, errors.New("PHP-FPM 未返回禁用函数配置")
	}
	view.DisableFunctions, err = phpFunctions(functions)
	if err != nil {
		return state, err
	}
	view.DisableFunctionsSource = sourceFor["disable_functions"]
	view.DisableFunctionsEditable = view.DisableFunctionsSource == "" || view.DisableFunctionsSource == phpINIConfig
	if view.DisableFunctionsSource == "" {
		view.DisableFunctionsSource = "PHP 默认值"
	}
	// Enumerate every pool, independently of the logging feature's default pool.
	seen := map[string]bool{}
	poolSeen := map[string]bool{}
	poolRaw := map[string]map[string]string{}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 8 || seen[path] || len(seen) >= 64 {
			return errors.New("PHP-FPM include 循环、重复或数量过多")
		}
		seen[path] = true
		if !strings.HasPrefix(path, "/usr/local/php/etc/") {
			return errors.New("PHP-FPM include 不在标准配置目录内")
		}
		if e := add(path); e != nil {
			return e
		}
		data := sources[len(sources)-1].file.Data
		current := ""
		poolName := ""
		for _, line := range strings.Split(string(data), "\n") {
			if section, ok := loggingSection(line); ok {
				current = section
				poolName = ""
				if current != "global" {
					if poolSeen[current] {
						return errors.New("PHP-FPM 存在同名进程池，无法可靠读取配置")
					}
					poolSeen[current] = true
					view.Pools = append(view.Pools, PHPPoolConfig{Pool: current, Source: path, Values: map[string]string{}, DisableFunctions: []string{}})
					poolName = current
					poolRaw[current] = map[string]string{}
				}
				continue
			}
			key, value := loggingLine(line)
			if key == "include" {
				if current != "" && current != "global" {
					return errors.New("暂不支持池内 include，无法可靠识别配置来源")
				}
				if filepath.Clean(value) != value || !filepath.IsAbs(value) || !strings.HasPrefix(value, "/usr/local/php/etc/") {
					return errors.New("仅支持标准配置目录内的绝对 PHP-FPM include 路径")
				}
				matches, e := filepath.Glob(p.path(value))
				if e != nil {
					return e
				}
				for _, match := range matches {
					if p.root != "" {
						rel, e := filepath.Rel(p.root, match)
						if e != nil {
							return e
						}
						match = "/" + rel
					}
					if e := visit(match, depth+1); e != nil {
						return e
					}
				}
			}
			if poolName == "" {
				continue
			}
			for _, name := range append(append([]string{}, phpConfigKeys...), "disable_functions") {
				if key == "php_value["+name+"]" || key == "php_admin_value["+name+"]" || key == "php_flag["+name+"]" || key == "php_admin_flag["+name+"]" {
					poolRaw[poolName][key] = value
				}
			}
		}
		return nil
	}
	if err = visit(loggingPHPConfig, 0); err != nil {
		return state, err
	}
	for i := range view.Pools {
		pool := &view.Pools[i]
		raw := poolRaw[pool.Pool]
		for _, name := range append(append([]string{}, phpConfigKeys...), "disable_functions") {
			for _, prefix := range []string{"php_value", "php_flag", "php_admin_value", "php_admin_flag"} {
				if value, ok := raw[prefix+"["+name+"]"]; ok {
					pool.Values[name] = value
					if name == "disable_functions" {
						names, e := phpFunctions(value)
						if e != nil {
							return state, e
						}
						pool.DisableFunctions, e = NormalizePHPFunctions(append(pool.DisableFunctions, names...))
						if e != nil {
							return state, e
						}
					}
				}
			}
		}
	}
	serviceFile, err := p.read(nginxManagementConfig)
	if err != nil {
		return state, err
	}
	sources = append(sources, loggingSource{nginxManagementConfig, serviceFile})
	hash := sha256.New()
	for _, source := range sources {
		fmt.Fprintf(hash, "%s\x00%d\x00%d\x00", source.path, source.file.Mode, source.file.GID)
		hash.Write(source.file.Data)
		hash.Write([]byte{0})
	}
	view.Revision = hex.EncodeToString(hash.Sum(nil))
	status, statusErr := p.execute(ctx, state.service, "status")
	view.Editable = statusErr == nil && status.State == "running"
	view.Message = "修改全局 PHP 配置，保存后自动重载 PHP-FPM；网站和进程池可覆盖部分参数。"
	if view.Editable {
		pidFile, e := p.masterPID(ctx)
		if e != nil {
			view.Editable = false
			view.Message = e.Error()
		} else {
			pidFile.file.Close()
		}
	} else {
		view.Message = "PHP-FPM 未运行或状态无法确认，请启动服务后保存配置。"
	}
	state.snapshot, state.sources = view, sources
	return state, nil
}
func patchPHPINI(data []byte, values map[string]string) []byte {
	newline := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		newline = "\r\n"
	}
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	conditional := false
	for _, line := range lines {
		if section, ok := loggingSection(line); ok {
			section = strings.ToUpper(section)
			conditional = strings.HasPrefix(section, "PATH=") || strings.HasPrefix(section, "HOST=")
		}
		key, _ := loggingLine(line)
		if _, replace := values[key]; replace && !conditional {
			// Preserve the original directive as a comment for context.
			out.WriteString("; " + strings.TrimRight(line, "\r\n") + newline)
		} else {
			out.WriteString(line)
		}
	}
	// Insert global settings before the first conditional section; ordinary INI
	// section headings do not alter PHP's global scope.
	text := out.String()
	at := len(text)
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if section, ok := loggingSection(line); ok {
			section = strings.ToUpper(section)
			if strings.HasPrefix(section, "PATH=") || strings.HasPrefix(section, "HOST=") {
				at = offset
				break
			}
		}
		offset += len(line)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var block strings.Builder
	if at > 0 && text[at-1] != '\n' {
		block.WriteString(newline)
	}
	for _, key := range keys {
		block.WriteString(key + " = \"" + values[key] + "\"" + newline)
	}
	return []byte(text[:at] + block.String() + text[at:])
}
func phpEquivalent(key, a, b string) bool {
	if key == "disable_functions" {
		aa, e1 := phpFunctions(a)
		bb, e2 := phpFunctions(b)
		return e1 == nil && e2 == nil && strings.Join(aa, ",") == strings.Join(bb, ",")
	}
	if key == "display_errors" {
		return normalizeLoggingBool(a) == normalizeLoggingBool(b)
	}
	if phpSize.MatchString(a) && phpSize.MatchString(b) {
		size := func(s string) uint64 {
			multiplier := uint64(1)
			switch s[len(s)-1] {
			case 'k', 'K':
				multiplier = 1 << 10
			case 'm', 'M':
				multiplier = 1 << 20
			case 'g', 'G':
				multiplier = 1 << 30
			}
			n, _ := strconv.ParseUint(strings.TrimRight(s, "kKmMgG"), 10, 64)
			return n * multiplier
		}
		return size(a) == size(b)
	}
	return a == b
}
func (p phpConfigController) check(ctx context.Context) error {
	task, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	_, err := p.run(task, p.path(loggingPHPBinary), "-t", "-y", p.path(loggingPHPConfig), "-c", p.path(phpINIConfig))
	if err != nil {
		return fmt.Errorf("PHP-FPM 配置检查未通过：%w", err)
	}
	return nil
}

func (p phpConfigController) update(ctx context.Context, r PHPConfigRequest) (result PHPConfigResponse, err error) {
	if err = r.Validate(); err != nil {
		return result, err
	}
	unlock, err := p.lock("php-fpm")
	if err != nil {
		return result, err
	}
	defer unlock()
	state, err := p.snapshot(ctx)
	if err != nil {
		return result, err
	}
	if state.snapshot.Revision != r.Revision {
		return result, errors.New("配置发生并发修改，请重新读取后再保存")
	}
	if !state.snapshot.Editable {
		return result, errors.New(state.snapshot.Message)
	}
	values := map[string]string{}
	for key, value := range r.Fields {
		field := state.snapshot.Fields[key]
		if !field.Editable {
			return result, fmt.Errorf("%s 被 %s 覆盖，无法在此修改", key, field.Source)
		}
		if !phpEquivalent(key, value, field.Value) {
			values[key] = value
		}
	}
	if r.DisableFunctions != nil {
		if !state.snapshot.DisableFunctionsEditable {
			return result, errors.New("禁用函数被扫描 INI 覆盖，无法在此修改")
		}
		names := slices.Clone(*r.DisableFunctions)
		for i, name := range names {
			name = strings.TrimSpace(name)
			if _, exists := slices.BinarySearch(state.snapshot.DisableFunctions, name); !exists {
				name = strings.ToLower(name)
			}
			names[i] = name
		}
		names, _ = NormalizePHPFunctions(names)
		if strings.Join(names, ",") != strings.Join(state.snapshot.DisableFunctions, ",") {
			values["disable_functions"] = strings.Join(names, ",")
		}
	}
	if len(values) == 0 {
		return PHPConfigResponse{state.snapshot, "配置未变化，无需重载"}, nil
	}
	if err = p.check(ctx); err != nil {
		return result, err
	}
	original := state.sources[0]
	data := patchPHPINI(original.file.Data, values)
	backupRoot := p.path("/var/backups/lnmp-gopanel")
	if err = os.MkdirAll(backupRoot, 0700); err != nil {
		return result, err
	}
	if err = p.trust(backupRoot); err != nil {
		return result, err
	}
	backup, err := os.MkdirTemp(backupRoot, "php-config-")
	if err != nil {
		return result, err
	}
	if err = os.WriteFile(filepath.Join(backup, "php.ini"), original.file.Data, 0600); err != nil {
		return result, err
	}
	// Rebuild the source graph immediately before writing, including new glob matches.
	latest, err := p.snapshot(ctx)
	if err != nil {
		return result, err
	}
	if latest.snapshot.Revision != r.Revision {
		return result, errors.New("配置发生并发修改，请重新读取后再保存")
	}
	if !latest.snapshot.Editable {
		return result, errors.New(latest.snapshot.Message)
	}
	changed, reloaded := false, false
	defer func() {
		if err == nil || !changed {
			return
		}
		recovery, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		current, e := p.read(phpINIConfig)
		if e != nil || !bytes.Equal(current.Data, data) || current.Mode != original.file.Mode || current.GID != original.file.GID {
			err = fmt.Errorf("%w；检测到并发修改，未覆盖当前文件；备份：%s", err, backup)
			return
		}
		if e = p.write(p.path(phpINIConfig), original.file.Data, original.file.Mode, original.file.GID); e != nil {
			err = fmt.Errorf("%w；原配置恢复失败；备份：%s", err, backup)
			return
		}
		if reloaded {
			if e = p.check(recovery); e == nil {
				_, e = p.reload(recovery, state.service)
			}
			if e != nil {
				err = fmt.Errorf("%w；已恢复原配置，服务恢复失败；备份：%s", err, backup)
				return
			}
		}
		err = fmt.Errorf("%w；已恢复原配置；备份：%s", err, backup)
	}()
	if err = p.write(p.path(phpINIConfig), data, original.file.Mode, original.file.GID); err != nil {
		return result, err
	}
	changed = true
	if err = p.check(ctx); err != nil {
		return result, err
	}
	actual, err := p.info(ctx)
	if err != nil {
		return result, err
	}
	for key, value := range values {
		if !phpEquivalent(key, actual[key], value) {
			return result, fmt.Errorf("PHP 未接受 %s 的配置值，未执行重载", key)
		}
	}
	// Re-enumerate glob matches as well as checking every original source.
	validated, e := p.snapshot(ctx)
	if e != nil {
		return result, e
	}
	if !validated.snapshot.Editable {
		return result, errors.New(validated.snapshot.Message)
	}
	if len(validated.sources) != len(state.sources) {
		return result, errors.New("配置来源发生并发修改，未执行重载")
	}
	for i, source := range state.sources {
		expected := source.file.Data
		if source.path == phpINIConfig {
			expected = data
		}
		current := validated.sources[i]
		if current.path != source.path || current.file.Mode != source.file.Mode || current.file.GID != source.file.GID || !bytes.Equal(current.file.Data, expected) {
			return result, errors.New("配置发生并发修改，未执行重载")
		}
	}
	if reloaded, err = p.reload(ctx, state.service); err != nil {
		return result, err
	}
	final, err := p.snapshot(ctx)
	if err != nil {
		return result, err
	}
	if !final.snapshot.Editable {
		return result, errors.New(final.snapshot.Message)
	}
	for key, value := range values {
		actualValue := final.snapshot.Fields[key].Value
		if key == "disable_functions" {
			actualValue = strings.Join(final.snapshot.DisableFunctions, ",")
		}
		if !phpEquivalent(key, actualValue, value) {
			return result, fmt.Errorf("重载后 %s 配置发生变化，请重新读取", key)
		}
	}
	return PHPConfigResponse{final.snapshot, "PHP 配置已保存，PHP-FPM 已重载"}, nil
}
func RootPHPConfig(ctx context.Context) (PHPConfigSnapshot, error) {
	p := defaultPHPConfigController()
	unlock, err := p.lock("php-fpm")
	if err != nil {
		return PHPConfigSnapshot{}, err
	}
	defer unlock()
	state, err := p.snapshot(ctx)
	return state.snapshot, err
}
func RootUpdatePHPConfig(ctx context.Context, r PHPConfigRequest) (PHPConfigResponse, error) {
	return defaultPHPConfigController().update(ctx, r)
}
