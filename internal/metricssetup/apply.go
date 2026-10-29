package metricssetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"gopkg.in/yaml.v3"
)

// All selected changes form a separate transaction from panel installation.
// Only an account whose CREATE completed successfully is eligible for DROP.
func (e *environment) apply(ctx context.Context, p *proposal) (backup string, err error) {
	unlock := func() {}
	if p.nginxReload || p.phpReload {
		unlock, err = e.lock()
		if err != nil {
			return "", err
		}
	}
	defer unlock()
	for _, original := range p.watch {
		if !e.unchanged(original) {
			return "", fmt.Errorf("配置在预览后发生变化，未应用修改：%s", original.Path)
		}
	}
	if e.active(ctx, "panel") != p.panelActive {
		return "", errors.New("面板运行状态已变化，请重新运行向导")
	}
	if len(p.changes) == 0 && p.account == nil {
		if err = e.userCheck(ctx, p.configPath, p.ids); err != nil {
			return "", errors.New("面板账号无法采集所选指标，请检查现有配置")
		}
		return "", nil
	}
	if p.nginxReload && !e.active(ctx, "nginx") {
		return "", errors.New("Nginx 已停止，请启动后重新运行向导")
	}
	if p.phpReload && !e.active(ctx, "php-fpm") {
		return "", errors.New("PHP-FPM 已停止，请启动后重新运行向导")
	}
	if err = e.validatePrograms(p); err != nil {
		return "", err
	}
	backup, err = e.backup(p)
	if err != nil {
		return "", err
	}
	attempted := []change{}
	phpAttempted, nginxAttempted, panelAttempted := false, false, false
	defer func() {
		if err == nil {
			return
		}
		restoreCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		problems := []string{}
		for i := len(attempted) - 1; i >= 0; i-- {
			original := attempted[i].before
			// Do not overwrite a third party's edit made while the wizard was
			// applying changes. Leave it intact and report the incomplete recovery.
			current, readErr := e.read(original.Path, true)
			if readErr != nil {
				problems = append(problems, "检查文件 "+original.Path)
				continue
			}
			if current.Exists && !bytes.Equal(current.Data, attempted[i].after) && (!original.Exists || !bytes.Equal(current.Data, original.Data)) {
				problems = append(problems, "并发修改文件 "+original.Path)
				continue
			}
			if original.Exists {
				if restoreErr := e.write(e.path(original.Path), original.Data, original.Mode, original.GID); restoreErr != nil {
					problems = append(problems, "文件 "+original.Path)
				}
			} else if removeErr := os.Remove(e.path(original.Path)); removeErr != nil && !os.IsNotExist(removeErr) {
				problems = append(problems, "删除新文件 "+original.Path)
			}
		}
		if phpAttempted {
			if e.command(restoreCtx, fpmBinary, "-t", "-y", e.path(fpmConfig)) != nil || e.command(restoreCtx, "/etc/init.d/php-fpm", "reload") != nil {
				problems = append(problems, "PHP-FPM 原配置重载")
			}
		}
		if nginxAttempted {
			if e.command(restoreCtx, nginxBinary, "-t") != nil || e.command(restoreCtx, nginxBinary, "-s", "reload") != nil {
				problems = append(problems, "Nginx 原配置重载")
			}
		}
		if panelAttempted && p.panelActive {
			if e.command(restoreCtx, "/usr/bin/systemctl", "restart", "lnmp-gopanel") != nil || !e.active(restoreCtx, "panel") || e.panelCheck != nil && e.panelCheck(restoreCtx, p.configPath) != nil {
				problems = append(problems, "面板原配置启动")
			}
		}
		if p.accountCreated {
			if dropErr := p.account.Drop(restoreCtx, p.accountUser, p.accountHost); dropErr != nil {
				problems = append(problems, "新账号 "+p.accountUser+"@"+p.accountHost)
			}
		}
		if len(problems) > 0 {
			err = fmt.Errorf("%w；恢复未完成：%s；备份：%s", err, strings.Join(problems, "、"), backup)
		} else {
			err = fmt.Errorf("%w；已恢复本次文件修改及已确认的新账号；备份：%s", err, backup)
		}
	}()
	if p.account != nil {
		// Check again after confirmation to detect names created by another admin.
		exists, checkErr := p.account.Exists(ctx, p.accountUser, p.accountHost)
		if checkErr != nil {
			return backup, checkErr
		}
		if exists {
			return backup, errors.New("同名账号已存在，未覆盖密码或权限")
		}
		if err = p.account.Create(ctx, p.accountUser, p.accountHost, p.accountPassword); err != nil {
			return backup, fmt.Errorf("%w；请核对账号创建结果，未自动删除未确认归属的账号 %s@%s", err, p.accountUser, p.accountHost)
		}
		p.accountCreated = true
		if err = e.probe(ctx, p.options, []string{"database"}); err != nil {
			return backup, errors.New("新监控账号状态查询验证失败")
		}
	}
	for _, update := range p.changes {
		if err = ctx.Err(); err != nil {
			return backup, err
		}
		if !e.unchanged(update.before) {
			return backup, fmt.Errorf("配置发生并发修改：%s", update.before.Path)
		}
		attempted = append(attempted, update)
		if err = e.write(e.path(update.before.Path), update.after, update.mode, update.gid); err != nil {
			return backup, fmt.Errorf("配置写入失败：%s", update.before.Path)
		}
	}
	if p.phpReload {
		if err = e.command(ctx, fpmBinary, "-t", "-y", e.path(fpmConfig)); err != nil {
			return backup, errors.New("PHP-FPM 新配置检查失败")
		}
	}
	if p.nginxReload {
		if err = e.command(ctx, nginxBinary, "-t"); err != nil {
			return backup, errors.New("Nginx 新配置检查失败")
		}
	}
	if p.phpReload {
		phpAttempted = true
		if err = e.command(ctx, "/etc/init.d/php-fpm", "reload"); err != nil {
			return backup, errors.New("PHP-FPM 重载失败")
		}
	}
	if p.nginxReload {
		nginxAttempted = true
		if err = e.command(ctx, nginxBinary, "-s", "reload"); err != nil {
			return backup, errors.New("Nginx 重载失败")
		}
	}
	// Reload commands can return before new workers accept requests. Retry only
	// for this bounded transition window, using the unprivileged panel identity.
	if err = e.waitCheck(ctx, p); err != nil {
		return backup, err
	}
	if p.panelActive && len(p.changes) > 0 {
		panelAttempted = true
		if err = e.command(ctx, "/usr/bin/systemctl", "restart", "lnmp-gopanel"); err != nil {
			return backup, errors.New("面板重启失败")
		}
		if !e.active(ctx, "panel") {
			return backup, errors.New("面板重启后未运行")
		}
		if e.panelCheck != nil {
			if err = e.panelCheck(ctx, p.configPath); err != nil {
				return backup, err
			}
		}
		if err = e.userCheck(ctx, p.configPath, p.ids); err != nil {
			return backup, errors.New("面板重启后的指标检查失败")
		}
	}
	return backup, nil
}
func (e *environment) waitCheck(ctx context.Context, p *proposal) error {
	task, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		if e.userCheck(task, p.configPath, p.ids) == nil {
			return nil
		}
		select {
		case <-task.Done():
			return errors.New("面板账号的服务采集验证失败，请检查状态入口、权限和凭据")
		case <-time.After(250 * time.Millisecond):
		}
	}
}
func (e *environment) validatePrograms(p *proposal) error {
	programs := []string{"/usr/bin/sudo", "/usr/local/bin/lnmp-gopanel"}
	if p.panelActive {
		programs = append(programs, "/usr/bin/systemctl")
	}
	if p.nginxReload {
		programs = append(programs, nginxBinary)
	}
	if p.phpReload {
		programs = append(programs, fpmBinary, "/etc/init.d/php-fpm")
	}
	for _, program := range programs {
		if e.trust(e.path(program)) != nil {
			return fmt.Errorf("程序路径不可信：%s", program)
		}
	}
	for _, update := range p.changes {
		if update.before.Path == p.configPath {
			var value config.Config
			decoder := yaml.NewDecoder(strings.NewReader(string(update.after)))
			decoder.KnownFields(true)
			if decoder.Decode(&value) != nil {
				return errors.New("更新后的面板配置无法解析")
			}
			// StateDir may be implicit; validate using the already resolved value.
			value.Metrics.StateDir = p.options.StateDir
			if value.Metrics.Validate() != nil {
				return errors.New("更新后的监控参数无效")
			}
		}
	}
	return nil
}
func (e *environment) backup(p *proposal) (string, error) {
	base := "/var/backups/lnmp-gopanel"
	full := e.path(base)
	if err := e.trust(filepath.Dir(full)); err != nil {
		return "", errors.New("备份目录的父目录不可信")
	}
	if err := os.MkdirAll(full, 0700); err != nil {
		return "", errors.New("无法创建备份目录")
	}
	if err := e.trust(full); err != nil {
		return "", errors.New("备份目录不可信")
	}
	dir, err := os.MkdirTemp(full, time.Now().UTC().Format("20060102T150405Z")+"-metrics-")
	if err != nil {
		return "", errors.New("无法创建监控备份")
	}
	manifest := struct {
		Files   []fileSnapshot `json:"files"`
		Account string         `json:"new_account,omitempty"`
	}{Files: []fileSnapshot{}}
	if p.account != nil {
		manifest.Account = p.accountUser + "@" + p.accountHost
	}
	for i, update := range p.changes {
		original := update.before
		manifest.Files = append(manifest.Files, original)
		if original.Exists {
			if err = e.write(filepath.Join(dir, fmt.Sprintf("%d.before", i)), original.Data, 0600, e.fileGroup); err != nil {
				return "", errors.New("原配置备份失败，未应用修改")
			}
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err = e.write(filepath.Join(dir, "manifest.json"), append(encoded, '\n'), 0600, e.fileGroup); err != nil {
		return "", errors.New("备份清单写入失败，未应用修改")
	}
	if e.root == "/" {
		return dir, nil
	}
	relative, _ := filepath.Rel(e.root, dir)
	return "/" + relative, nil
}
