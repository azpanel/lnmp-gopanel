//go:build linux

package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	cronpreview "github.com/azpanel/lnmp-gopanel/internal/cron"
	"golang.org/x/sys/unix"
)

const backupRunLimit = 24 * time.Hour
const backupCronPath = "/etc/cron.d/lnmp-gopanel-backups"

var errBackupRunFinished = errors.New("备份任务已经结束")
var errBackupLaunchUnknown = errors.New("无法确认备份系统任务是否已启动，请检查 systemd")

type storedBackupRun struct {
	backups.Run
	Snapshot *storedBackupPolicy `json:"snapshot,omitempty"`
}
type backupRunRef struct {
	ID        string    `json:"id"`
	PolicyID  string    `json:"policy_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func recentBackupRuns(refs []backupRunRef) []backupRunRef {
	result := make([]backupRunRef, len(refs))
	for n := range refs {
		result[n] = refs[len(refs)-1-n]
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if backups.ActiveRun(a.Status) != backups.ActiveRun(b.Status) {
			return backups.ActiveRun(a.Status)
		}
		x, y := a.UpdatedAt, b.UpdatedAt
		if x.IsZero() {
			x = a.CreatedAt
		}
		if y.IsZero() {
			y = b.CreatedAt
		}
		return x.After(y)
	})
	return result
}

type backupRunIndex struct {
	Runs []backupRunRef `json:"runs"`
	// Separate from history: pruning history must never permit duplicate ticks.
	Ticks map[string]time.Time `json:"ticks"`
}
type backupExecutor struct {
	store       backupPolicyStore
	helper      string
	run         CommandRunner
	now         func() time.Time
	execute     func(context.Context, *storedBackupRun) error
	installLock string
	tools       func(string) string
}

func (e backupExecutor) executable(name string) string {
	if e.tools != nil {
		return e.tools(name)
	}
	return backupTool(name)
}

func defaultBackupExecutor() backupExecutor {
	p := defaultBackupInstaller()
	return backupExecutor{store: backupPolicyStore{state: p.state, accountHome: "/root"}, helper: p.helper, run: RunCommand, now: time.Now, installLock: "/run/lnmp-gopanel-install.lock"}
}
func (e backupExecutor) runPath(id string) string {
	return filepath.Join(e.store.state, "runs", id+".json")
}
func (e backupExecutor) workPath(id string) string { return filepath.Join(e.store.state, "work", id) }
func privateBackupDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || TrustedPath(path) != nil {
		return errors.New("备份任务目录权限不安全")
	}
	return nil
}
func readBackupJSON(path string, limit int64, value any) error {
	data, err := readPrivateBackupFile(path, limit)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, value) != nil {
		return errors.New("备份任务记录损坏")
	}
	return nil
}
func writeBackupJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = atomicInstallFile(path, data, 0600); err != nil {
		return err
	}
	f, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (e backupExecutor) installationReadLock() (func(), error) {
	if e.installLock == "" {
		return func() {}, nil
	}
	f, err := os.OpenFile(e.installLock, os.O_CREATE|os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || TrustedPath(e.installLock) != nil {
		f.Close()
		return nil, errors.New("面板安装锁权限不安全")
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("%w：面板正在安装、升级或卸载，请稍后重试", backups.ErrRunBusy)
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}

// All index and run mutations use the policy store lock. Execution and network
// calls never hold it. A separate worker lock lives for the whole execution.
func (e backupExecutor) index() (backupRunIndex, error) {
	i := backupRunIndex{Runs: []backupRunRef{}, Ticks: map[string]time.Time{}}
	err := readBackupJSON(filepath.Join(e.store.state, "run-index.json"), 64<<20, &i)
	if os.IsNotExist(err) {
		err = nil
	}
	if i.Runs == nil {
		i.Runs = []backupRunRef{}
	}
	if i.Ticks == nil {
		i.Ticks = map[string]time.Time{}
	}
	for _, r := range i.Runs {
		if !backups.ValidPolicyID(r.ID) || !backups.ValidPolicyID(r.PolicyID) {
			return i, errors.New("备份任务索引损坏")
		}
	}
	return i, err
}
func (e backupExecutor) saveIndex(i backupRunIndex) error {
	return writeBackupJSON(filepath.Join(e.store.state, "run-index.json"), i)
}
func (e backupExecutor) loadRun(id string) (storedBackupRun, error) {
	var j storedBackupRun
	if !backups.ValidPolicyID(id) {
		return j, backups.ErrInvalid
	}
	err := readBackupJSON(e.runPath(id), 2<<20, &j)
	if os.IsNotExist(err) {
		err = backups.ErrNotFound
	}
	if err == nil && (j.ID != id || !backups.ValidPolicyID(j.PolicyID)) {
		err = errors.New("备份任务记录损坏")
	}
	return j, err
}
func (e backupExecutor) saveRun(j *storedBackupRun) error {
	j.UpdatedAt = e.now().UTC()
	j.Error = backupDiagnostic(j.Error)
	if !backups.ActiveRun(j.Status) {
		j.Snapshot = nil
	}
	if err := privateBackupDirectory(filepath.Dir(e.runPath(j.ID))); err != nil {
		return err
	}
	return writeBackupJSON(e.runPath(j.ID), j)
}
func (e backupExecutor) update(j *storedBackupRun) error {
	release, err := e.store.lock()
	if err != nil {
		return err
	}
	defer release()
	i, err := e.index()
	if err != nil {
		return err
	}
	for n := range i.Runs {
		if i.Runs[n].ID == j.ID {
			if !backups.ActiveRun(i.Runs[n].Status) {
				return errBackupRunFinished
			}
			if err = e.saveRun(j); err != nil {
				return err
			}
			i.Runs[n].Status = j.Status
			i.Runs[n].UpdatedAt = j.UpdatedAt
			return e.saveIndex(i)
		}
	}
	return backups.ErrNotFound
}
func newBackupRunID() (string, error) {
	var id [16]byte
	_, err := rand.Read(id[:])
	return hex.EncodeToString(id[:]), err
}
func (e backupExecutor) enqueue(i *backupRunIndex, p storedBackupPolicy, trigger string, scheduled *time.Time) (storedBackupRun, error) {
	for _, r := range i.Runs {
		if r.PolicyID == p.ID && backups.ActiveRun(r.Status) {
			return storedBackupRun{}, backups.ErrRunBusy
		}
	}
	id, err := newBackupRunID()
	if err != nil {
		return storedBackupRun{}, err
	}
	j := storedBackupRun{Run: backups.Run{ID: id, PolicyID: p.ID, PolicyName: p.Name, Revision: p.Revision, Trigger: trigger, Status: "queued", Phase: "queued", CreatedAt: e.now().UTC(), ScheduledFor: scheduled, Categories: []backups.CategoryResult{}}, Snapshot: &p}
	if err = e.saveRun(&j); err != nil {
		return j, err
	}
	i.Runs = append(i.Runs, backupRunRef{ID: id, PolicyID: p.ID, Status: j.Status, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt})
	return j, nil
}
func (e backupExecutor) start(ctx context.Context, r backups.PolicyIdentity) (backups.Run, error) {
	if !backups.ValidPolicyID(r.ID) || r.Revision == 0 {
		return backups.Run{}, backups.ErrInvalid
	}
	unlock, err := e.installationReadLock()
	if err != nil {
		return backups.Run{}, err
	}
	defer unlock()
	release, err := e.store.lock()
	if err != nil {
		return backups.Run{}, err
	}
	policies, err := e.store.load()
	var selected *storedBackupPolicy
	if err == nil {
		for n := range policies {
			if policies[n].ID == r.ID {
				selected = &policies[n]
				break
			}
		}
		if selected == nil {
			err = backups.ErrNotFound
		}
	}
	if err == nil && selected.Revision != r.Revision {
		err = backups.ErrConflict
	}
	if err == nil {
		err = backups.ValidateDestinations(selected.Destinations, true)
	}
	var j storedBackupRun
	if err == nil {
		var i backupRunIndex
		i, err = e.index()
		if err == nil {
			j, err = e.enqueue(&i, *selected, "manual", nil)
			if err == nil {
				err = e.saveIndex(i)
			}
		}
	}
	release()
	if err != nil {
		return backups.Run{}, err
	}
	if err = e.dispatch(ctx, false); err != nil {
		return j.Run, err
	}
	current, err := e.loadRun(j.ID)
	return current.Run, err
}
func backupExecutionUnit(id string) string { return "lnmp-gopanel-backup-run-" + id + ".service" }
func (e backupExecutor) unitActive(ctx context.Context, id string) (bool, error) {
	if err := TrustedPath("/usr/bin/systemctl"); err != nil {
		return false, err
	}
	out, err := e.run(ctx, "/usr/bin/systemctl", "show", "--property=ActiveState,LoadState", backupExecutionUnit(id))
	if err != nil && !strings.Contains(out, "LoadState=not-found") {
		return false, errors.New("无法查询备份系统任务状态")
	}
	return strings.Contains(out, "ActiveState=active") || strings.Contains(out, "ActiveState=activating") || strings.Contains(out, "ActiveState=deactivating"), nil
}
func (e backupExecutor) launch(ctx context.Context, j storedBackupRun) error {
	for _, p := range []string{e.helper, "/usr/bin/systemd-run", "/usr/bin/systemctl"} {
		if err := TrustedPath(p); err != nil {
			return errors.New("备份辅助程序或 systemd 权限不安全")
		}
	}
	_, err := e.run(ctx, "/usr/bin/systemd-run", "--quiet", "--collect", "--unit="+backupExecutionUnit(j.ID), "--property=Type=exec", "--property=UMask=0077", "--property=StandardInput=null", "--property=StandardOutput=null", "--property=StandardError=null", "--property=RuntimeMaxSec=24h", "--property=TimeoutStopSec=20s", e.helper, "backup-policy-worker", j.ID)
	if err != nil {
		return errBackupLaunchUnknown
	}
	return nil
}

// Tick registration, reservations and FIFO selection are one short transaction.
// Workers execute in separate units and can outlive the panel or dispatcher.
func (e backupExecutor) dispatch(ctx context.Context, tick bool) error {
	unlock, err := e.installationReadLock()
	if errors.Is(err, backups.ErrRunBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	if e.installLock != "" {
		if _, err := os.Lstat("/var/backups/lnmp-gopanel/pending"); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	release, err := e.store.lock()
	if err != nil {
		return err
	}
	i, err := e.index()
	if err != nil {
		release()
		return err
	}
	policies, err := e.store.load()
	if err != nil {
		release()
		return err
	}
	current := map[string]storedBackupPolicy{}
	var interrupted []storedBackupRun
	for _, p := range policies {
		current[p.ID] = p
	}
	now := e.now()
	minute := now.Truncate(time.Minute)
	// Reconcile only old reservations: a just-started unit may not be visible yet.
	for n := range i.Runs {
		r := &i.Runs[n]
		if !backups.ActiveRun(r.Status) {
			continue
		}
		j, loadErr := e.loadRun(r.ID)
		if loadErr != nil {
			release()
			return loadErr
		}
		if j.Status != r.Status {
			r.Status = j.Status
			r.UpdatedAt = j.UpdatedAt
			if !backups.ActiveRun(j.Status) {
				interrupted = append(interrupted, j)
			}
			continue
		}
		p, exists := current[r.PolicyID]
		if r.Status == "queued" && (!exists || (j.Trigger == "schedule" && !p.Enabled)) {
			j.Status = "cancelled"
			j.Error = "政策已删除或停用"
			j.FinishedAt = &now
		} else if r.Status == "running" && now.Sub(j.UpdatedAt) > time.Minute {
			active, checkErr := e.unitActive(ctx, j.ID)
			if checkErr != nil {
				release()
				return checkErr
			}
			if !active {
				j.Status = "failed"
				j.Error = "备份系统任务已中断"
				j.FinishedAt = &now
				interrupted = append(interrupted, j)
			}
		}
		if j.Status != r.Status {
			if err = e.saveRun(&j); err != nil {
				release()
				return err
			}
			r.Status = j.Status
			r.UpdatedAt = j.UpdatedAt
		}
	}
	if tick {
		for _, p := range policies {
			if !p.Enabled {
				continue
			}
			schedule, parseErr := cronpreview.ParseSchedule(p.Schedule)
			if parseErr != nil {
				continue
			}
			if !schedule.Next(minute.Add(-time.Minute)).Equal(minute) || !i.Ticks[p.ID].Before(minute) {
				continue
			}
			i.Ticks[p.ID] = minute
			_, enqueueErr := e.enqueue(&i, p, "schedule", &minute)
			if errors.Is(enqueueErr, backups.ErrRunBusy) {
				id, idErr := newBackupRunID()
				if idErr != nil {
					release()
					return idErr
				}
				j := storedBackupRun{Run: backups.Run{ID: id, PolicyID: p.ID, PolicyName: p.Name, Revision: p.Revision, Trigger: "schedule", Status: "skipped", Phase: "finished", CreatedAt: now.UTC(), FinishedAt: &now, ScheduledFor: &minute, Error: "该政策已有任务排队或运行，跳过本次计划", Categories: []backups.CategoryResult{}}}
				if err = e.saveRun(&j); err != nil {
					release()
					return err
				}
				i.Runs = append(i.Runs, backupRunRef{ID: id, PolicyID: p.ID, Status: j.Status, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt})
			} else if enqueueErr != nil {
				release()
				return enqueueErr
			}
		}
	}
	for id := range i.Ticks {
		if _, ok := current[id]; !ok {
			delete(i.Ticks, id)
		}
	}
	active := 0
	for _, r := range i.Runs {
		if r.Status == "running" {
			active++
		}
	}
	var launch []storedBackupRun
	for n := range i.Runs {
		r := &i.Runs[n]
		if active >= 2 {
			break
		}
		if r.Status != "queued" {
			continue
		}
		j, loadErr := e.loadRun(r.ID)
		if loadErr != nil {
			release()
			return loadErr
		}
		j.Status = "running"
		j.Phase = "preflight"
		started := now.UTC()
		j.StartedAt = &started
		if err = e.saveRun(&j); err != nil {
			release()
			return err
		}
		r.Status = j.Status
		r.UpdatedAt = j.UpdatedAt
		active++
		launch = append(launch, j)
	}
	// Keep the active run and the most recently updated results, up to 100 per
	// policy. A long job finishing after many skipped ticks must retain its result.
	counts := map[string]int{}
	retained := map[string]bool{}
	for _, r := range recentBackupRuns(i.Runs) {
		counts[r.PolicyID]++
		if counts[r.PolicyID] <= 100 || backups.ActiveRun(r.Status) {
			retained[r.ID] = true
		}
	}
	keep := make([]backupRunRef, 0, len(i.Runs))
	var prune []string
	for _, r := range i.Runs {
		if retained[r.ID] {
			keep = append(keep, r)
		} else {
			prune = append(prune, r.ID)
		}
	}
	i.Runs = keep
	err = e.saveIndex(i)
	release()
	if err != nil {
		return err
	}
	for _, id := range prune {
		_ = os.Remove(e.runPath(id))
		_ = os.Remove(filepath.Join(e.store.state, "runs", id+".lock"))
	}
	for _, j := range interrupted {
		e.cleanupWork(j)
	}
	for _, j := range launch {
		if launchErr := e.launch(ctx, j); launchErr != nil {
			if errors.Is(launchErr, errBackupLaunchUnknown) {
				probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				active, probeErr := e.unitActive(probeCtx, j.ID)
				cancel()
				// A timed-out systemd-run may already have created the unit. Keep
				// its reservation until the unit can be authoritatively reconciled.
				if active || probeErr != nil {
					continue
				}
			}
			current, loadErr := e.loadRun(j.ID)
			if loadErr != nil {
				return loadErr
			}
			if !backups.ActiveRun(current.Status) {
				continue
			}
			j = current
			j.Status = "failed"
			j.Error = launchErr.Error()
			finished := e.now().UTC()
			j.FinishedAt = &finished
			if err = e.update(&j); err != nil {
				if errors.Is(err, errBackupRunFinished) {
					continue
				}
				return err
			}
		}
	}
	return nil
}
func (e backupExecutor) list(r backups.RunQuery) (backups.RunList, error) {
	result := backups.RunList{Items: []backups.Run{}, Page: r.Page, PageSize: 20}
	if !backups.ValidPolicyID(r.ID) || r.Page < 0 || r.Page > 5000 {
		return result, backups.ErrInvalid
	}
	if result.Page < 1 {
		result.Page = 1
	}
	release, err := e.store.lock()
	if err != nil {
		return result, err
	}
	defer release()
	i, err := e.index()
	if err != nil {
		return result, err
	}
	for _, ref := range recentBackupRuns(i.Runs) {
		if ref.PolicyID != r.ID {
			continue
		}
		result.Total++
		if result.Total <= (result.Page-1)*result.PageSize || len(result.Items) >= result.PageSize {
			continue
		}
		j, loadErr := e.loadRun(ref.ID)
		if loadErr != nil {
			return result, loadErr
		}
		j.Categories = []backups.CategoryResult{}
		result.Items = append(result.Items, j.Run)
	}
	return result, nil
}

// Called while holding the policy store lock, before policy deletion/disable.
func (s backupPolicyStore) cancelPolicyRuns(id string, deleting bool) error {
	e := defaultBackupExecutor()
	e.store = s
	i, err := e.index()
	if err != nil {
		return err
	}
	for _, r := range i.Runs {
		if r.PolicyID == id && r.Status == "running" && deleting {
			return backups.ErrRunBusy
		}
	}
	for n := range i.Runs {
		r := &i.Runs[n]
		if r.PolicyID != id || r.Status != "queued" {
			continue
		}
		j, err := e.loadRun(r.ID)
		if err != nil {
			return err
		}
		j.Status = "cancelled"
		j.Error = "政策已删除或停用"
		finished := e.now().UTC()
		j.FinishedAt = &finished
		if err = e.saveRun(&j); err != nil {
			return err
		}
		r.Status = j.Status
		r.UpdatedAt = j.UpdatedAt
	}
	return e.saveIndex(i)
}
func (e backupExecutor) worker(ctx context.Context, id string) error {
	if !backups.ValidPolicyID(id) {
		return backups.ErrInvalid
	}
	if err := privateBackupDirectory(filepath.Join(e.store.state, "runs")); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(e.store.state, "runs", id+".lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if TrustedPath(f.Name()) != nil {
		return errors.New("备份任务锁权限不安全")
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("备份任务已在执行")
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	j, err := e.loadRun(id)
	if err != nil {
		return err
	}
	if j.Status != "running" || j.Snapshot == nil {
		return errors.New("备份任务状态无效")
	}
	ctx, cancel := context.WithTimeout(ctx, backupRunLimit)
	defer cancel()
	defer e.cleanupWork(j)
	if e.execute != nil {
		err = e.execute(ctx, &j)
	} else {
		err = e.executeRun(ctx, &j)
	}
	if err != nil {
		j.Status = "failed"
		j.Error = backupDiagnostic(err.Error())
	}
	if ctx.Err() != nil {
		j.Status = "failed"
		j.Error = "备份已超时或中断"
	}
	if backups.ActiveRun(j.Status) {
		j.Status = "failed"
		j.Error = "备份未完成"
	}
	finished := e.now().UTC()
	j.FinishedAt = &finished
	j.Phase = "finished"
	if err = e.update(&j); err != nil {
		return err
	}
	// The periodic dispatcher also recovers queues if this best-effort call fails.
	nextCtx, done := context.WithTimeout(context.Background(), 20*time.Second)
	defer done()
	return e.dispatch(nextCtx, false)
}

func (e backupExecutor) cleanupWork(j storedBackupRun) {
	_ = os.RemoveAll(e.workPath(j.ID))
	if j.Snapshot == nil {
		return
	}
	for _, d := range j.Snapshot.Destinations {
		if d.Type != "local" {
			continue
		}
		root, err := os.OpenRoot(d.Path)
		if err != nil {
			continue
		}
		for _, category := range []string{"databases", "files"} {
			name := backupArchiveName(j, category)
			_ = root.Remove("." + name + ".partial")
		}
		root.Close()
	}
}
func DispatchBackupPolicies() error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	return defaultBackupExecutor().dispatch(ctx, true)
}
func RunBackupPolicyWorker(id string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return defaultBackupExecutor().worker(ctx, id)
}
func backupRunCall(ctx context.Context, action string, in io.Reader) (any, error) {
	e := defaultBackupExecutor()
	switch action {
	case "run":
		var r backups.PolicyIdentity
		if err := decodeBackupPolicy(in, &r); err != nil {
			return nil, err
		}
		return e.start(ctx, r)
	case "runs":
		var r backups.RunQuery
		if err := decodeBackupPolicy(in, &r); err != nil {
			return nil, err
		}
		return e.list(r)
	case "run-get":
		var r backups.RunQuery
		if err := decodeBackupPolicy(in, &r); err != nil {
			return nil, err
		}
		j, err := e.loadRun(r.ID)
		return j.Run, err
	case "runtime":
		return inspectBackupRuntime(ctx), nil
	}
	return nil, backups.ErrInvalid
}

// Kept deterministic for both deployed runtime checks and execution preflight.
func backupTool(name string) string {
	var paths []string
	switch name {
	case "7zip":
		paths = []string{"/usr/bin/7zz", "/usr/bin/7z", "/usr/local/bin/7zz", "/usr/local/bin/7z"}
	case "dump":
		paths = []string{"/usr/bin/mariadb-dump", "/usr/bin/mysqldump", "/usr/local/mariadb/bin/mariadb-dump", "/usr/local/mariadb/bin/mysqldump", "/usr/local/mysql/bin/mysqldump"}
	case "gdrive":
		paths = []string{"/usr/local/bin/gdrive", "/usr/bin/gdrive"}
	case "rclone":
		paths = []string{"/usr/bin/rclone", "/usr/local/bin/rclone"}
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 && TrustedPath(path) == nil {
			return path
		}
	}
	return ""
}
func inspectBackupRuntime(ctx context.Context) backups.Runtime {
	r := backups.Runtime{Dependencies: []backups.Dependency{}}
	for _, name := range []string{"7zip", "dump", "gdrive", "rclone"} {
		d := backups.Dependency{Name: name, Ready: backupTool(name) != ""}
		if !d.Ready {
			d.Error = "未安装或程序权限不安全，请由管理员安装 root 管理的工具"
		}
		r.Dependencies = append(r.Dependencies, d)
	}
	if data, err := readTrustedFile(backupCronPath, 4096); err == nil && strings.Contains(string(data), "lnmp-gopanel-control backup-policy-dispatch") && TrustedPath("/usr/bin/systemctl") == nil {
		for _, service := range []string{"cron.service", "crond.service"} {
			if _, err := RunCommand(ctx, "/usr/bin/systemctl", "is-active", "--quiet", service); err == nil {
				r.Scheduler = true
				break
			}
		}
	}
	if !r.Scheduler {
		r.SchedulerError = "备份调度未就绪，请检查受管 Cron 文件和 cron/crond 服务"
	}
	return r
}
