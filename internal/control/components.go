package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/components"
)

var componentPatterns = map[string]*regexp.Regexp{
	"python3":  regexp.MustCompile(`(?m)^Python (3\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))\s*$`),
	"go":       regexp.MustCompile(`^go version go([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[a-zA-Z0-9.+-]*))\s`),
	"node":     regexp.MustCompile(`^v([0-9]+\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))\s*$`),
	"codex":    regexp.MustCompile(`^codex-cli ([0-9]+\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))\s*$`),
	"composer": regexp.MustCompile(`(?m)^Composer(?: version)? ([0-9]+\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))(?:[ \t].*)?$`),
}

func parseComponentVersion(name, output string) (string, error) {
	if p := componentPatterns[name]; p != nil {
		if match := p.FindStringSubmatch(strings.TrimSpace(output)); len(match) == 2 {
			return match[1], nil
		}
	}
	return "", fmt.Errorf("无法识别组件版本号")
}

var nvmInstalledVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
var nvmVersion = regexp.MustCompile(`^v?([0-9]+)(?:\.([0-9]+))?(?:\.([0-9]+))?$`)
var nvmAlias = regexp.MustCompile(`^(?:default|lts/(?:\*|[a-zA-Z][a-zA-Z0-9_-]*))$`)

// Resolve aliases as data. Never source root's shell profile or nvm.sh.
func nvmDefaultBin(root string, trust func(string) error) (string, error) {
	alias := "default"
	for depth := 0; depth < 8; depth++ {
		if nvmVersion.MatchString(alias) || alias == "node" || alias == "stable" {
			entries, err := os.ReadDir(filepath.Join(root, "versions/node"))
			if err != nil {
				return "", fmt.Errorf("无法读取 root NVM 安装目录")
			}
			best := ""
			prefix := strings.Split(strings.TrimPrefix(alias, "v"), ".")
			for _, e := range entries {
				v := strings.TrimPrefix(e.Name(), "v")
				parts := strings.Split(v, ".")
				if !e.IsDir() || !nvmInstalledVersion.MatchString(e.Name()) {
					continue
				}
				match := alias == "node" || alias == "stable"
				if !match {
					match = true
					for i, p := range prefix {
						if parts[i] != p {
							match = false
						}
					}
				}
				if match && versionGreater(v, best) {
					best = v
				}
			}
			if best == "" {
				return "", fmt.Errorf("root NVM 默认版本未安装")
			}
			return filepath.Join(root, "versions/node/v"+best, "bin"), nil
		}
		if !nvmAlias.MatchString(alias) {
			return "", fmt.Errorf("root NVM 默认别名不受支持")
		}
		path := filepath.Join(root, "alias", alias)
		info, err := os.Stat(path)
		if os.IsNotExist(err) && depth == 0 {
			return "", nil
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > 256 {
			return "", fmt.Errorf("无法读取 root NVM 默认别名")
		}
		if err := trust(path); err != nil {
			return "", fmt.Errorf("root NVM 别名权限不安全或无法读取")
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 256 {
			return "", fmt.Errorf("无法读取 root NVM 默认别名")
		}
		alias = strings.TrimSpace(string(data))
	}
	return "", fmt.Errorf("root NVM 别名循环或层级过多")
}
func versionGreater(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		x, _ := strconv.Atoi(aa[i])
		y := 0
		if i < len(bb) {
			y, _ = strconv.Atoi(bb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}
func componentProgram(name, nvmBin string) string {
	switch name {
	case "python3":
		return firstExecutable("/usr/local/bin/python3", "/usr/bin/python3")
	case "go":
		return firstExecutable("/usr/local/bin/go", "/usr/local/go/bin/go", "/usr/bin/go")
	case "composer":
		return firstExecutable("/usr/local/bin/composer", "/usr/bin/composer", "/usr/local/php/bin/composer")
	case "node", "codex":
		if nvmBin != "" {
			if program := firstExecutable(filepath.Join(nvmBin, name)); program != "" {
				return program
			}
		}
		return firstExecutable("/usr/local/bin/"+name, "/usr/bin/"+name)
	}
	return ""
}
func componentInterpreter(name, program string) string {
	switch name {
	case "codex":
		return firstExecutable(filepath.Join(filepath.Dir(program), "node"), "/usr/local/bin/node", "/usr/bin/node")
	case "composer":
		return firstExecutable("/usr/local/php/bin/php", "/usr/local/bin/php", "/usr/bin/php")
	}
	return ""
}
func componentCommand(ctx context.Context, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=" + filepath.Dir(program) + ":/usr/local/bin:/usr/bin:/bin", "HOME=/root", "LANG=C", "LC_ALL=C", "GOTOOLCHAIN=local", "PYTHONNOUSERSITE=1"}
	configureProcess(cmd)
	out := &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return strings.TrimSpace(string(out.data)), err
}
func probeComponent(ctx context.Context, name, program string, run CommandRunner, trust func(string) error) components.Item {
	return probeComponentWithInterpreter(ctx, name, program, componentInterpreter(name, program), run, trust)
}
func probeComponentWithInterpreter(ctx context.Context, name, program, interpreter string, run CommandRunner, trust func(string) error) components.Item {
	now := time.Now()
	item := components.Item{Name: name, Status: "missing", CheckedAt: &now}
	if program == "" {
		return item
	}
	item.Path = program
	item.Status = "failed"
	if err := trust(program); err != nil {
		item.Error = "组件可执行文件权限不安全"
		return item
	}
	if name == "composer" && interpreter == "" {
		item.Error = "未找到 PHP CLI 解释器"
		return item
	}
	// Script launchers must not bypass the executable's trust checks via an interpreter.
	if interpreter != "" {
		if err := trust(interpreter); err != nil {
			item.Error = "Node.js 解释器权限不安全"
			if name == "composer" {
				item.Error = "PHP 解释器权限不安全"
			}
			return item
		}
	}
	args := []string{"--version"}
	if name == "go" {
		args = []string{"version"}
	}
	if name == "composer" {
		args = []string{program, "--no-plugins", "--no-scripts", "--no-interaction", "--no-ansi", "--version"}
		program = interpreter
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	output, err := run(ctx, program, args...)
	if err != nil {
		item.Error = "组件版本读取失败或超时"
		return item
	}
	item.Version, err = parseComponentVersion(name, output)
	if err != nil {
		item.Error = err.Error()
		return item
	}
	item.Status = "ready"
	return item
}
func ComponentSnapshot(ctx context.Context) components.Snapshot {
	nvmBin, nvmErr := nvmDefaultBin("/root/.nvm", TrustedPath)
	items := make([]components.Item, len(components.Names()))
	var wg sync.WaitGroup
	for i, name := range components.Names() {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			if (name == "node" || name == "codex") && nvmErr != nil {
				now := time.Now()
				items[i] = components.Item{Name: name, Status: "failed", Error: nvmErr.Error(), CheckedAt: &now}
				return
			}
			if name == "phpmyadmin" {
				items[i] = probePHPMyAdmin()
				return
			}
			items[i] = probeComponent(ctx, name, componentProgram(name, nvmBin), componentCommand, TrustedPath)
		}(i, name)
	}
	wg.Wait()
	status := "ready"
	for _, item := range items {
		if item.Status == "failed" {
			status = "partial"
		}
	}
	return components.Snapshot{Status: status, Items: items}
}
func (h Helper) Components(ctx context.Context) (components.Snapshot, error) {
	output, err := RunCommand(ctx, "/usr/bin/sudo", "-n", h.Path, "components", "snapshot")
	if err != nil {
		return components.Snapshot{}, fmt.Errorf("组件查询辅助程序不可用，请检查安装与 components snapshot 的 sudoers 权限")
	}
	var result components.Snapshot
	if json.Unmarshal([]byte(output), &result) != nil || len(result.Items) != len(components.Names()) || (result.Status != "ready" && result.Status != "partial") {
		return components.Snapshot{}, fmt.Errorf("组件辅助程序返回无效数据")
	}
	seen := map[string]bool{}
	for _, item := range result.Items {
		if !components.ValidName(item.Name) || seen[item.Name] || (item.Status != "ready" && item.Status != "missing" && item.Status != "failed") {
			return components.Snapshot{}, fmt.Errorf("组件辅助程序返回无效条目")
		}
		seen[item.Name] = true
	}
	return result, nil
}
