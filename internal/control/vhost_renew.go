package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func RootPreviewRenewal(ctx context.Context, names []string) ([]vhosts.RenewPreview, error) {
	if err := vhosts.ValidateRenewNames(names); err != nil {
		return nil, err
	}
	store := vhosts.DefaultStore()
	if err := TrustedPath(store.Directory); err != nil {
		return nil, err
	}
	_, acmeErr := findACMEClient(acmeHomes(), TrustedPath)
	if acmeErr == nil && (!executable("/usr/bin/flock") || TrustedPath("/usr/bin/flock") != nil) {
		acmeErr = errors.New("缺少可信的 flock，无法配置自动续期")
	}
	out := []vhosts.RenewPreview{}
	p := siteProvisioner{store: store, stateDir: managedSites, trust: TrustedPath}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := vhosts.RenewPreview{Name: name, Domains: []string{}, Provider: "letsencrypt"}
		plan, err := store.PlanRenewal(name, TrustedPath)
		item.Domains = append(item.Domains, plan.Domains...)
		if strings.Contains(strings.ToLower(plan.Certificate.Issuer), "zerossl") {
			item.Provider = "zerossl"
		}
		if state, e := p.loadManaged(name); e == nil {
			item.Email = state.Request.SSL.Email
		}
		if err == nil {
			err = acmeErr
		}
		item.Eligible = err == nil
		if err != nil {
			item.Reason = err.Error()
		}
		out = append(out, item)
	}
	return out, nil
}

// Every attempt uses a fresh cert-home. Existing acme installation hooks cannot
// overwrite the live certificate while the new order is being validated.
func RootRenewVhost(ctx context.Context, id string, item vhosts.RenewItem, emit func(vhosts.CreateEvent), binary string) error {
	if err := (vhosts.RenewRequest{RequestID: id, Items: []vhosts.RenewItem{item}}).Validate(); err != nil {
		return err
	}
	client, err := findACMEClient(acmeHomes(), TrustedPath)
	if err != nil {
		return err
	}
	key := vhosts.BasicAuthKey(item.Name)
	parent := filepath.Join(managedSSL, "renew-"+key)
	if err := TrustedPath(managedSSL); err != nil {
		if e := TrustedPath(filepath.Dir(managedSSL)); e != nil {
			return e
		}
		if e := os.Mkdir(managedSSL, 0700); e != nil && !os.IsExist(e) {
			return e
		}
		if e := TrustedPath(managedSSL); e != nil {
			return e
		}
	}
	if err := os.Mkdir(parent, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := TrustedPath(parent); err != nil {
		return err
	}
	version, err := os.MkdirTemp(parent, "version-")
	if err != nil {
		return err
	}
	client.certDirectory = filepath.Join(version, "acme")
	destination := filepath.Join(version, "deployed")
	cronPath := "/etc/cron.d/lnmp-gopanel-acme-" + key[:32]
	oldCron, cronErr := readTrustedFile(cronPath, 64*1024)
	if cronErr != nil {
		if _, e := os.Lstat(cronPath); !os.IsNotExist(e) {
			return errors.New("已有续期任务权限不安全或无法读取")
		}
	}
	p := siteProvisioner{store: vhosts.DefaultStore(), stateDir: managedSites, trust: TrustedPath, lock: LockNginx, run: RunCommand}
	p.issue = func(ctx context.Context, r vhosts.CreateRequest, binary string, log func(string)) (string, string, error) {
		return issueCertificate(ctx, r, binary, log, client, true, destination, false)
	}
	activate := func() error { return writeRootAtomic(cronPath, []byte(client.renewalCron()), 0644) }
	restore := func() error {
		if cronErr == nil {
			return writeRootAtomic(cronPath, oldCron, 0644)
		}
		err := os.Remove(cronPath)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	err = p.renew(ctx, id, item, binary, emit, activate, restore)
	// Keep candidate files on failure for diagnosis; they have no cron entry or
	// live configuration reference unless rollback explicitly reported uncertainty.
	return err
}
func (p siteProvisioner) renew(ctx context.Context, id string, item vhosts.RenewItem, binary string, emit func(vhosts.CreateEvent), activate, restore func() error) error {
	if err := p.trust(binary); err != nil {
		return errors.New("Nginx 程序权限不安全")
	}
	if err := p.trust(p.store.Directory); err != nil {
		return err
	}
	plan, err := p.store.PlanRenewal(item.Name, p.trust)
	if err != nil {
		return err
	}
	progress := func(phase, line string) { emit(vhosts.CreateEvent{Phase: phase, Line: line}) }
	check := func(parent context.Context) error {
		task, done := context.WithTimeout(parent, 8*time.Second)
		defer done()
		out, e := p.run(task, binary, "-t")
		if out != "" {
			progress("nginx", out)
		}
		return e
	}
	reload := func(parent context.Context) error {
		task, done := context.WithTimeout(parent, 8*time.Second)
		defer done()
		out, e := p.run(task, binary, "-s", "reload")
		if out != "" {
			progress("nginx", out)
		}
		return e
	}
	prepared, _, err := p.store.RenewalContent(plan, "", "")
	if err != nil {
		return err
	}
	unlock, err := p.lock()
	if err != nil {
		return err
	}
	err = p.store.Install(ctx, item.Name, []byte(prepared), []byte(plan.Original), check, reload)
	unlock()
	if err != nil {
		return err
	}
	// Do not hold the Nginx lock while waiting on the CA. Deployment checks exact
	// expected bytes so concurrent administrator edits cannot be overwritten.
	rollback := func(cause error) error {
		task, done := context.WithTimeout(context.Background(), 25*time.Second)
		defer done()
		unlock, e := p.lock()
		if e != nil {
			return fmt.Errorf("%w；原配置恢复失败：%v", cause, e)
		}
		defer unlock()
		if e = p.store.Install(task, item.Name, []byte(plan.Original), []byte(prepared), check, reload); e != nil {
			return fmt.Errorf("%w；配置恢复失败，请检查 Nginx：%v", cause, e)
		}
		return cause
	}
	progress("ssl", "正在强制签发，保留在线证书："+strings.Join(plan.Domains, ", "))
	r := vhosts.CreateRequest{RequestID: id, Name: item.Name, Domain: plan.Domains[0], Aliases: plan.Domains[1:], SSL: vhosts.SSLInput{Mode: item.Provider, Email: item.Email}}
	cert, key, err := p.issue(ctx, r, binary, func(line string) { progress("ssl", line) })
	if err != nil {
		return rollback(err)
	}
	content, base, err := p.store.RenewalContent(plan, cert, key)
	if err != nil {
		return rollback(err)
	}
	unlock, err = p.lock()
	if err != nil {
		return rollback(err)
	}
	defer unlock()
	progress("deploy", "正在检查证书配置并重载 Nginx")
	if err = p.store.Install(ctx, item.Name, []byte(content), []byte(prepared), check, reload); err != nil {
		// Install restores prepared bytes on ordinary failures; restore the original
		// challenge routing too, without overwriting any external edits.
		task, done := context.WithTimeout(context.Background(), 25*time.Second)
		defer done()
		if e := p.store.Install(task, item.Name, []byte(plan.Original), []byte(prepared), check, reload); e != nil {
			return fmt.Errorf("%w；原配置恢复失败：%v", err, e)
		}
		return err
	}
	var originalState *managedSite
	if state, e := p.loadManaged(item.Name); e == nil {
		originalBase, _, e := p.store.StripBasicAuth(item.Name, plan.Original)
		if e == nil && state.Hash == configHash([]byte(originalBase)) {
			copy := state
			originalState = &copy
			state.Hash = configHash([]byte(base))
			state.Request.SSL.Mode = item.Provider
			state.Request.SSL.Email = item.Email
			if err = p.saveManaged(state); err != nil {
				progress("deploy", "站点记录更新失败，正在恢复")
			}
		}
	}
	if err == nil {
		err = activate()
	}
	if err != nil {
		task, done := context.WithTimeout(context.Background(), 25*time.Second)
		defer done()
		configErr := p.store.Install(task, item.Name, []byte(plan.Original), []byte(content), check, reload)
		cronErr := restore()
		var stateErr error
		if originalState != nil {
			stateErr = p.saveManaged(*originalState)
		}
		if configErr != nil || cronErr != nil || stateErr != nil {
			return fmt.Errorf("续期登记失败，恢复不完整，请检查 Nginx：%v / %v / %v", configErr, cronErr, stateErr)
		}
		return fmt.Errorf("续期登记失败，已恢复原配置：%w", err)
	}
	progress("done", "新证书已部署，自动续期已配置")
	return nil
}
