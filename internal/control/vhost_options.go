package control

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

const acmeHome = "/root/.acme.sh"
const acmeCertHome = acmeHome + "/gopanel-certs"
const managedSites = vhosts.Prefix + "/conf/gopanel-sites"
const managedSSL = vhosts.Prefix + "/conf/ssl/gopanel"

var phpIncludePattern = regexp.MustCompile(`^enable-php([0-9]+\.[0-9]+)?\.conf$`)
var rewriteNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,80}$`)

func readTrustedFile(path string, limit int64) ([]byte, error) {
	if err := TrustedPath(path); err != nil {
		return nil, errors.New("文件路径权限不安全或不存在")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("文件类型无效或过大")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("文件读取失败或过大")
	}
	return data, nil
}

func RootVhostOptions(ctx context.Context) (vhosts.CreateOptions, error) {
	o := vhosts.CreateOptions{PHP: []vhosts.PHPRuntime{}, Rewrites: []string{"none"}, Certificates: []vhosts.CertificateOption{}, Warnings: []string{}}
	conf := filepath.Join(vhosts.Prefix, "conf")
	if err := TrustedPath(vhosts.Directory); err != nil {
		return o, errors.New("Nginx vhost 目录不存在或权限不安全")
	}
	entries, err := os.ReadDir(conf)
	if err != nil {
		return o, err
	}
	for _, entry := range entries {
		match := phpIncludePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		if TrustedPath(filepath.Join(conf, entry.Name())) != nil {
			continue
		}
		id, label := "default", "默认 PHP"
		if match[1] != "" {
			id = match[1]
			label = "PHP " + id
		}
		pathinfo := strings.TrimSuffix(entry.Name(), ".conf") + "-pathinfo.conf"
		if TrustedPath(filepath.Join(conf, pathinfo)) != nil {
			pathinfo = ""
		}
		program := "/usr/local/php" + match[1] + "/bin/php-config"
		if TrustedPath(program) == nil {
			if version, e := RunCommand(ctx, program, "--version"); e == nil && regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
				label += " (" + version + ")"
			}
		}
		o.PHP = append(o.PHP, vhosts.PHPRuntime{ID: id, Label: label, Include: entry.Name(), PathInfoInclude: pathinfo, PathInfoAvailable: pathinfo != ""})
	}
	if rewrites, e := os.ReadDir(filepath.Join(conf, "rewrite")); e == nil {
		for _, entry := range rewrites {
			name := strings.TrimSuffix(entry.Name(), ".conf")
			if entry.Name() != name+".conf" || name == "none" || !rewriteNamePattern.MatchString(name) {
				continue
			}
			if TrustedPath(filepath.Join(conf, "rewrite", entry.Name())) == nil {
				o.Rewrites = append(o.Rewrites, name)
			}
		}
	}
	o.ACMEAvailable = TrustedPath(acmeHome+"/acme.sh") == nil && TrustedPath("/usr/bin/flock") == nil
	o.Certificates = scanWildcardCertificates(acmeHome)
	if !o.ACMEAvailable {
		o.Warnings = append(o.Warnings, "需要可信的 /root/.acme.sh/acme.sh 和 /usr/bin/flock 才能自动申请及续期 SSL")
	}
	// Probe credentials without exposing root configuration or passwords.
	probe, done := context.WithTimeout(ctx, 2*time.Second)
	defer done()
	db, e := openSiteDatabase(probe, "")
	if e == nil {
		defer db.Close()
		o.DatabaseAvailable = true
		o.DatabaseAuthAvailable = true
	} else {
		o.DatabaseAvailable = databaseInstalled()
	}
	return o, nil
}

func scanWildcardCertificates(root string) []vhosts.CertificateOption {
	out := []vhosts.CertificateOption{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	if len(entries) > 2000 {
		entries = entries[:2000]
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "ca" || entry.Name() == "dnsapi" {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		certPath := filepath.Join(dir, "fullchain.cer")
		certPEM, err := readTrustedFile(certPath, 4*1024*1024)
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), "_ecc")
		keyPath := filepath.Join(dir, base+".key")
		keyPEM, err := readTrustedFile(keyPath, 1024*1024)
		if err != nil {
			continue
		}
		// Metadata remains useful for expired certificates, which cannot be selected.
		leaf, checkErr := vhosts.CheckCertificate(certPEM, keyPEM, nil, time.Now())
		if leaf == nil {
			pair, e := parseCertificatePair(certPEM, keyPEM)
			if e != nil {
				continue
			}
			leaf = pair
		}
		wildcard := false
		for _, name := range leaf.DNSNames {
			wildcard = wildcard || strings.HasPrefix(name, "*.")
		}
		if !wildcard {
			continue
		}
		sum := sha256.Sum256([]byte(certPath + "\x00" + keyPath))
		o := vhosts.CertificateOption{ID: hex.EncodeToString(sum[:]), CertFile: certPath, KeyFile: keyPath, Usable: checkErr == nil, Certificate: certificateMetadata(certPath, leaf)}
		if checkErr != nil {
			o.Error = checkErr.Error()
		}
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b vhosts.CertificateOption) int { return strings.Compare(a.CertFile, b.CertFile) })
	return out
}

func chooseSiteOptions(r vhosts.CreateRequest, o vhosts.CreateOptions) (vhosts.PHPRuntime, error) {
	php := vhosts.PHPRuntime{}
	if r.PHP != "none" {
		for _, p := range o.PHP {
			if p.ID == r.PHP {
				php = p
				break
			}
		}
		if php.Include == "" {
			return php, errors.New("所选 PHP 版本不可用")
		}
		if r.PathInfo && php.PathInfoInclude == "" {
			return php, errors.New("所选 PHP 缺少 Pathinfo 配置")
		}
	}
	if !slices.Contains(o.Rewrites, r.Rewrite) {
		return php, errors.New("所选伪静态模板不存在或权限不安全")
	}
	return php, nil
}

func resolveSiteCertificate(r vhosts.CreateRequest, o vhosts.CreateOptions) (string, string, error) {
	cert, key := r.SSL.CertFile, r.SSL.KeyFile
	if r.SSL.Mode == "existing" {
		found := false
		for _, candidate := range o.Certificates {
			if candidate.ID == r.SSL.CertificateID {
				cert, key = candidate.CertFile, candidate.KeyFile
				found = true
				break
			}
		}
		if !found {
			return "", "", errors.New("所选通配符证书已不存在，请重新扫描")
		}
	}
	c, e := readTrustedFile(cert, 4*1024*1024)
	if e != nil {
		return "", "", fmt.Errorf("证书：%w", e)
	}
	k, e := readTrustedFile(key, 1024*1024)
	if e != nil {
		return "", "", fmt.Errorf("私钥：%w", e)
	}
	if _, e = vhosts.CheckCertificate(c, k, r.Domains(), time.Now()); e != nil {
		return "", "", e
	}
	return cert, key, nil
}

func parseCertificatePair(c, k []byte) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair(c, k)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(pair.Certificate[0])
}
func certificateMetadata(path string, leaf *x509.Certificate) vhosts.Certificate {
	now := time.Now()
	status := "valid"
	switch {
	case now.Before(leaf.NotBefore):
		status = "not_yet_valid"
	case !now.Before(leaf.NotAfter):
		status = "expired"
	case leaf.NotAfter.Sub(now) <= 30*24*time.Hour:
		status = "expiring"
	}
	sum := sha256.Sum256(leaf.Raw)
	return vhosts.Certificate{Path: path, Issuer: leaf.Issuer.String(), Subject: leaf.Subject.String(), DNSNames: leaf.DNSNames, NotBefore: &leaf.NotBefore, NotAfter: &leaf.NotAfter, DaysRemaining: int(leaf.NotAfter.Sub(now).Hours() / 24), Serial: leaf.SerialNumber.Text(16), Fingerprint: hex.EncodeToString(sum[:]), Status: status}
}
