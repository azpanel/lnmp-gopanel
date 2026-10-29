//go:build linux

package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const nginxManagementConfig = "/etc/lnmp-gopanel/services.yaml"
const lnmpNginxBinary = "/usr/local/nginx/sbin/nginx"
const lnmpNginxConfig = "/usr/local/nginx/conf/nginx.conf"
const lnmpNginxPID = "/usr/local/nginx/logs/nginx.pid"
const lnmpNginxInit = "/etc/init.d/nginx"

type nginxMaster struct {
	PID       int
	StartTime string
	Binary    string
	Cgroup    string
	Command   string
}

type managementFile struct {
	Exists bool
	Data   []byte
	Mode   os.FileMode
	GID    int
}

type nginxManagementController struct {
	services, proc, root string
	trust                func(string) error
	lock                 func() (func(), error)
	run                  CommandRunner
	write                func(string, []byte, os.FileMode, int) error
}

func defaultNginxManagementController() nginxManagementController {
	return nginxManagementController{services: nginxManagementConfig, proc: "/proc", trust: TrustedPath, lock: LockNginx, run: RunCommand, write: writeManagementFile}
}

func (p nginxManagementController) path(path string) string {
	if p.root != "" {
		return filepath.Join(p.root, path)
	}
	return path
}

func (p nginxManagementController) saveFile(path string, data []byte, mode os.FileMode, gid int) error {
	return p.write(p.path(path), data, mode, gid)
}

func writeManagementFile(path string, data []byte, mode os.FileMode, gid int) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".nginx-management-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chown(0, gid); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (p nginxManagementController) readFile(path string, optional bool) (managementFile, error) {
	var snapshot managementFile
	path = p.path(path)
	if err := p.trust(filepath.Dir(path)); err != nil {
		return snapshot, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if optional && os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return snapshot, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || st.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return snapshot, errors.New("管理配置或 PID 文件权限不安全")
	}
	data, err := io.ReadAll(io.LimitReader(f, 128*1024+1))
	if err != nil || len(data) > 128*1024 {
		return snapshot, errors.New("管理配置或 PID 文件无法读取或过大")
	}
	return managementFile{true, data, info.Mode().Perm(), int(st.Gid)}, nil
}

func (p nginxManagementController) masters(ctx context.Context, binary string) ([]nginxMaster, error) {
	resolved, err := filepath.EvalSymlinks(p.path(binary))
	if err != nil {
		return nil, errors.New("无法访问 Nginx 可执行文件")
	}
	entries, err := os.ReadDir(p.proc)
	if err != nil {
		return nil, err
	}
	masters := []nginxMaster{}
	denied := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(p.proc, entry.Name())
		target, err := os.Readlink(filepath.Join(dir, "exe"))
		if errors.Is(err, os.ErrPermission) {
			denied = true
		}
		if err != nil || strings.TrimSuffix(target, " (deleted)") != resolved {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			return nil, errors.New("无法验证 Nginx 进程身份")
		}
		if !bytes.HasPrefix(cmdline, []byte("nginx: master process ")) {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil {
			return nil, err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 {
			return nil, errors.New("Nginx 主进程所有者异常")
		}
		stat, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			return nil, err
		}
		_, rest, ok := strings.Cut(string(stat), ") ")
		fields := strings.Fields(rest)
		if !ok || len(fields) < 20 {
			return nil, errors.New("无法验证 Nginx 进程启动时间")
		}
		if start, err := strconv.ParseUint(fields[19], 10, 64); err != nil || start == 0 {
			return nil, errors.New("Nginx 进程启动时间无效")
		}
		cgroup, err := os.ReadFile(filepath.Join(dir, "cgroup"))
		if err != nil {
			return nil, err
		}
		masters = append(masters, nginxMaster{pid, fields[19], resolved, string(cgroup), strings.TrimSpace(strings.TrimRight(string(cmdline), "\x00"))})
	}
	if len(masters) == 0 && denied {
		return nil, errors.New("部分进程不可读取，无法确认 Nginx 管理状态")
	}
	return masters, nil
}

func (p nginxManagementController) unit(ctx context.Context, s config.Service) (map[string]string, error) {
	out, err := p.run(ctx, s.Program, "show", "--property=ActiveState,MainPID,ControlGroup", s.Target)
	if err != nil {
		return nil, errors.New("无法查询 Nginx 系统服务归属")
	}
	properties := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			properties[key] = value
		}
	}
	if properties["ActiveState"] == "" {
		return nil, errors.New("Nginx 系统服务状态不完整")
	}
	if pid, err := strconv.Atoi(properties["MainPID"]); err != nil || pid < 0 {
		return nil, errors.New("Nginx 系统服务主进程记录不完整")
	}
	return properties, nil
}

func unitOwnsMaster(properties map[string]string, master nginxMaster) bool {
	if properties["MainPID"] != strconv.Itoa(master.PID) || properties["ControlGroup"] == "" {
		return false
	}
	for _, line := range strings.Split(master.Cgroup, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || (parts[0] != "0" && parts[1] != "name=systemd") {
			continue
		}
		if parts[2] == properties["ControlGroup"] || strings.HasPrefix(parts[2], properties["ControlGroup"]+"/") {
			return true
		}
	}
	return false
}

func (p nginxManagementController) status(ctx context.Context, s config.Service) (Result, bool) {
	if s.Name != "nginx" || s.Manager != "systemd" {
		return Result{}, false
	}
	properties, err := p.unit(ctx, s)
	if err != nil {
		return Result{State: "unknown", Message: err.Error(), Manager: s.Manager}, true
	}
	binary := s.CheckProgram
	if binary == "" {
		binary = firstExecutable(serviceBinaries("nginx")...)
	}
	masters, err := p.masters(ctx, binary)
	if err != nil {
		return Result{State: "unknown", Message: err.Error(), Manager: s.Manager}, true
	}
	if properties["ActiveState"] == "activating" || properties["ActiveState"] == "deactivating" {
		return Result{State: "transitioning", Message: "Nginx 系统服务正在切换，请稍后再试", Manager: s.Manager}, true
	}
	active := properties["ActiveState"] == "active" || properties["ActiveState"] == "reloading"
	if len(masters) > 0 && (!active || len(masters) != 1 || !unitOwnsMaster(properties, masters[0])) {
		return Result{State: "unmanaged", Message: "管理状态异常：Nginx 进程正在运行，但未由配置的 systemd 单元管理；请先预览并修复管理方式", Manager: s.Manager}, true
	}
	return Result{}, false
}

func NginxManagementStatus(ctx context.Context, s config.Service) (Result, bool) {
	return defaultNginxManagementController().status(ctx, s)
}

type managementRepairSnapshot struct {
	Plan                          ManagementRepairPlan
	Services, PID, Script, Config managementFile
	Master                        nginxMaster
	Unit                          map[string]string
	Service                       config.Service
	Candidate                     []byte
}

func scriptValue(data []byte, name string) string {
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), name+"="); ok {
			return strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return ""
}

var nginxPIDDirective = regexp.MustCompile(`(?m)^[\t ]*pid[\t ]+([^;\r\n]+);`)

func (p nginxManagementController) prepare(ctx context.Context) (managementRepairSnapshot, error) {
	var snapshot managementRepairSnapshot
	var err error
	snapshot.Services, err = p.readFile(p.services, false)
	if err != nil {
		return snapshot, err
	}
	services, err := config.LoadServices(p.path(p.services))
	if err != nil {
		return snapshot, err
	}
	index := -1
	for i, s := range services {
		if s.Name == "nginx" {
			index = i
			snapshot.Service = s
		}
	}
	s := snapshot.Service
	if index < 0 || s.Manager != "systemd" || s.CheckProgram != lnmpNginxBinary {
		return snapshot, errors.New("仅支持将标准 LNMP Nginx 从失配的 systemd 配置修复为 init 脚本管理")
	}
	if err = p.trust(s.Program); err != nil {
		return snapshot, err
	}
	if err = p.trust(s.CheckProgram); err != nil {
		return snapshot, err
	}
	snapshot.Unit, err = p.unit(ctx, s)
	if err != nil {
		return snapshot, err
	}
	state := snapshot.Unit["ActiveState"]
	if state != "inactive" && state != "failed" {
		return snapshot, errors.New("系统服务正在运行或切换，无法安全切换管理方式")
	}
	if snapshot.Unit["MainPID"] != "0" {
		return snapshot, errors.New("系统服务仍保留主进程记录，请通过 SSH 检查后再修复")
	}
	masters, err := p.masters(ctx, s.CheckProgram)
	if err != nil || len(masters) != 1 {
		return snapshot, errors.New("需要唯一且可验证的 Nginx 主进程；请检查进程后重新预览")
	}
	snapshot.Master = masters[0]
	if snapshot.Master.Command != "nginx: master process "+lnmpNginxBinary+" -c "+lnmpNginxConfig {
		return snapshot, errors.New("Nginx 主进程使用自定义启动参数，请通过 SSH 检查后再修复")
	}
	for _, line := range strings.Split(snapshot.Master.Cgroup, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && (parts[0] == "0" || parts[1] == "name=systemd") && strings.Contains(parts[2], ".service") {
			return snapshot, errors.New("Nginx 主进程仍属于系统服务，请通过 SSH 统一管理入口")
		}
	}
	if unitOwnsMaster(snapshot.Unit, snapshot.Master) {
		return snapshot, errors.New("Nginx 仍属于系统服务，不能切换管理方式")
	}
	snapshot.Script, err = p.readFile(lnmpNginxInit, false)
	if err != nil || snapshot.Script.Mode&0111 == 0 || scriptValue(snapshot.Script.Data, "NGINX_BIN") != lnmpNginxBinary || scriptValue(snapshot.Script.Data, "CONFIG") != lnmpNginxConfig {
		return snapshot, errors.New("需要可信且使用标准路径的 LNMP Nginx init 脚本")
	}
	snapshot.Config, err = p.readFile(lnmpNginxConfig, false)
	if err != nil {
		return snapshot, err
	}
	matches := nginxPIDDirective.FindAllSubmatch(snapshot.Config.Data, -1)
	if len(matches) != 1 || strings.Trim(string(matches[0][1]), "\"' ") != lnmpNginxPID {
		return snapshot, errors.New("Nginx PID 路径不是标准 LNMP 路径，请通过 SSH 检查配置")
	}
	snapshot.PID, err = p.readFile(lnmpNginxPID, true)
	if err != nil {
		return snapshot, err
	}
	pid := strings.TrimSpace(string(snapshot.PID.Data))
	if pid != "" && pid != strconv.Itoa(snapshot.Master.PID) {
		return snapshot, errors.New("PID 文件与实际 Nginx 主进程不一致，请通过 SSH 检查后再修复")
	}
	services[index].Manager, services[index].Program, services[index].Target = "init", lnmpNginxInit, ""
	snapshot.Candidate, err = yaml.Marshal(config.Services{Services: services})
	if err != nil {
		return snapshot, err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot, err
	}
	hash := sha256.Sum256(data)
	snapshot.Plan = ManagementRepairPlan{
		Revision: hex.EncodeToString(hash[:]), CurrentManager: "systemd", ProposedManager: "init",
		MasterPID: snapshot.Master.PID, RestorePID: pid == "",
		Message: "验证现有主进程，按需恢复 PID 记录并切换为 LNMP init 脚本管理；保留修复前备份，失败自动恢复配置与 PID 记录。现有 Nginx 进程保持运行。",
	}
	return snapshot, nil
}

func RootNginxManagementPreview(ctx context.Context) (ManagementRepairPlan, error) {
	p := defaultNginxManagementController()
	release, err := p.lock()
	if err != nil {
		return ManagementRepairPlan{}, err
	}
	defer release()
	snapshot, err := p.prepare(ctx)
	return snapshot.Plan, err
}

func (p nginxManagementController) restore(path string, snapshot managementFile) error {
	if !snapshot.Exists {
		err := os.Remove(p.path(path))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return p.saveFile(path, snapshot.Data, snapshot.Mode, snapshot.GID)
}

func sameManagementFile(a, b managementFile) bool {
	return a.Exists == b.Exists && a.Mode == b.Mode && a.GID == b.GID && bytes.Equal(a.Data, b.Data)
}

func (p nginxManagementController) rollbackPID(before managementRepairSnapshot) error {
	current, err := p.readFile(lnmpNginxPID, true)
	if err != nil {
		return err
	}
	if sameManagementFile(current, before.PID) {
		return nil
	}
	if strings.TrimSpace(string(before.PID.Data)) != "" {
		return errors.New("原有 PID 记录已被其他操作修改，保留当前记录")
	}
	pid := strings.TrimSpace(string(current.Data))
	if pid != "" {
		if pid != strconv.Itoa(before.Master.PID) {
			return errors.New("PID 记录已被其他操作修改，保留当前记录")
		}
		ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		masters, err := p.masters(ctx, before.Service.CheckProgram)
		if err != nil {
			return err
		}
		for _, master := range masters {
			if master != before.Master {
				return errors.New("Nginx 主进程已变化，保留当前 PID 记录")
			}
		}
	}
	return p.restore(lnmpNginxPID, before.PID)
}

func (p nginxManagementController) repair(ctx context.Context, request ManagementRepairRequest) (result Result, err error) {
	release, err := p.lock()
	if err != nil {
		return result, err
	}
	defer release()
	before, err := p.prepare(ctx)
	if err != nil {
		return result, err
	}
	if len(request.Revision) != 64 || request.Revision != before.Plan.Revision {
		return result, errors.New("管理配置或进程已变化，请重新预览后再确认")
	}
	backup := p.services + ".nginx-repair-" + request.Revision[:16] + ".json"
	data, err := json.Marshal(before)
	if err != nil {
		return result, err
	}
	if err = p.saveFile(backup, data, 0600, 0); err != nil {
		return result, errors.New("无法保存修复前备份，未修改管理配置")
	}
	configWritten := false
	var writtenConfig managementFile
	// nginx -t can create an empty PID file. Include that side effect in rollback.
	defer func() {
		if err == nil {
			return
		}
		var rollback error
		current, readErr := p.readFile(p.services, false)
		switch {
		case readErr != nil:
			rollback = readErr
		case sameManagementFile(current, before.Services):
		case configWritten && (sameManagementFile(current, writtenConfig) || (bytes.Equal(current.Data, before.Candidate) && current.Mode == before.Services.Mode && current.GID == before.Services.GID)):
			rollback = p.restore(p.services, before.Services)
		default:
			rollback = errors.New("服务配置已被其他操作修改，保留当前配置")
		}
		rollback = errors.Join(rollback, p.rollbackPID(before))
		if rollback != nil {
			err = fmt.Errorf("修复失败且自动恢复未完成，请通过 SSH 检查；备份：%s：%w", backup, errors.Join(err, rollback))
		} else {
			err = fmt.Errorf("修复失败，已恢复原配置和 PID 记录：%w", err)
		}
	}()
	if _, err = p.run(ctx, before.Service.CheckProgram, "-t"); err != nil {
		return result, errors.New("Nginx 配置检查失败")
	}
	afterCheck, err := p.prepare(ctx)
	if err != nil {
		return result, err
	}
	if before.Master != afterCheck.Master || !bytes.Equal(before.Services.Data, afterCheck.Services.Data) || !bytes.Equal(before.Config.Data, afterCheck.Config.Data) || !bytes.Equal(before.Script.Data, afterCheck.Script.Data) {
		return result, errors.New("配置检查期间管理配置或进程已变化")
	}
	if before.Plan.RestorePID {
		if err = p.saveFile(lnmpNginxPID, []byte(strconv.Itoa(before.Master.PID)+"\n"), 0644, 0); err != nil {
			return result, err
		}
	}
	configWritten = true
	if err = p.saveFile(p.services, before.Candidate, before.Services.Mode, before.Services.GID); err != nil {
		return result, err
	}
	writtenConfig, err = p.readFile(p.services, false)
	if err != nil {
		return result, err
	}
	services, err := config.LoadServices(p.path(p.services))
	if err != nil {
		return result, err
	}
	found := false
	for _, service := range services {
		if service.Name == "nginx" && service.Manager == "init" && service.Program == lnmpNginxInit && service.CheckProgram == before.Service.CheckProgram && service.Target == "" {
			found = true
		}
	}
	masters, err := p.masters(ctx, before.Service.CheckProgram)
	if err != nil || !found || len(masters) != 1 || masters[0] != before.Master {
		return result, errors.New("修复后进程或管理配置验证失败")
	}
	pid, err := p.readFile(lnmpNginxPID, false)
	if err != nil || strings.TrimSpace(string(pid.Data)) != strconv.Itoa(before.Master.PID) {
		return result, errors.New("修复后 PID 记录验证失败")
	}
	properties, err := p.unit(ctx, before.Service)
	if err != nil || properties["MainPID"] != "0" || (properties["ActiveState"] != "failed" && properties["ActiveState"] != "inactive") {
		return result, errors.New("修复期间系统服务状态已变化")
	}
	return Result{State: "running", Manager: "init", Message: "已验证并修复 Nginx 管理方式，现有进程保持运行；修复前备份：" + backup}, nil
}

func RootNginxManagementRepair(ctx context.Context, request ManagementRepairRequest) (Result, error) {
	return defaultNginxManagementController().repair(ctx, request)
}
