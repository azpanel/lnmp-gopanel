package vhosts

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const CreateTimeout = 20 * time.Minute
const AccessLogDirectory = "/home/wwwlogs"
const ChallengeDirectory = Prefix + "/conf/gopanel-acme"

type SSLInput struct {
	Mode          string `json:"mode"`
	Email         string `json:"email,omitempty"`
	CertFile      string `json:"cert_file,omitempty"`
	KeyFile       string `json:"key_file,omitempty"`
	CertificateID string `json:"certificate_id,omitempty"`
	Redirect      bool   `json:"redirect"`
}
type DatabaseInput struct {
	Enabled       bool   `json:"enabled"`
	Name          string `json:"name,omitempty"`
	Password      string `json:"password,omitempty"`
	AdminPassword string `json:"admin_password,omitempty"`
}
type CreateRequest struct {
	RequestID string        `json:"request_id"`
	Name      string        `json:"name,omitempty"`
	Retry     bool          `json:"retry,omitempty"`
	Domain    string        `json:"domain"`
	Aliases   []string      `json:"aliases"`
	Root      string        `json:"root"`
	PHP       string        `json:"php"`
	PathInfo  bool          `json:"path_info"`
	Rewrite   string        `json:"rewrite"`
	AccessLog bool          `json:"access_log"`
	LogName   string        `json:"log_name"`
	IPv6      bool          `json:"ipv6"`
	SSL       SSLInput      `json:"ssl"`
	Database  DatabaseInput `json:"database"`
}
type PHPRuntime struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	Include           string `json:"-"`
	PathInfoInclude   string `json:"-"`
	PathInfoAvailable bool   `json:"path_info_available"`
}
type CertificateOption struct {
	ID          string      `json:"id"`
	CertFile    string      `json:"cert_file"`
	KeyFile     string      `json:"key_file"`
	Certificate Certificate `json:"certificate"`
	Usable      bool        `json:"usable"`
	Error       string      `json:"error,omitempty"`
}
type CreateOptions struct {
	PHP                   []PHPRuntime        `json:"php"`
	Rewrites              []string            `json:"rewrites"`
	Certificates          []CertificateOption `json:"certificates"`
	ACMEAvailable         bool                `json:"acme_available"`
	DatabaseAvailable     bool                `json:"database_available"`
	DatabaseAuthAvailable bool                `json:"database_auth_available"`
	Warnings              []string            `json:"warnings"`
}
type CreateResult struct {
	Name        string `json:"name"`
	SiteCreated bool   `json:"site_created"`
	Database    string `json:"database"`
	SSL         string `json:"ssl"`
	Message     string `json:"message"`
}
type CreateEvent struct {
	Phase  string        `json:"phase,omitempty"`
	Line   string        `json:"line,omitempty"`
	Result *CreateResult `json:"result,omitempty"`
}
type ProvisionBackend interface {
	VhostOptions(context.Context) (CreateOptions, error)
	CreateVhost(context.Context, CreateRequest, func(CreateEvent)) (CreateResult, error)
}

var domainLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,80}$`)
var DatabaseName = regexp.MustCompile(`^[a-zA-Z0-9_]{1,32}$`)
var logFilename = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,239}\.log$`)

func NormalizeDomain(domain string) (string, error) {
	domain, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	domain = strings.ToLower(domain)
	if err != nil || len(domain) > 230 || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return "", errors.New("请输入有效域名，不包含协议、端口或通配符")
	}
	for _, label := range strings.Split(domain, ".") {
		if !domainLabel.MatchString(label) {
			return "", errors.New("域名格式无效")
		}
	}
	return domain, nil
}
func (r CreateRequest) Domains() []string { return append([]string{r.Domain}, r.Aliases...) }
func ValidateSSL(s SSLInput) error {
	switch s.Mode {
	case "none":
	case "letsencrypt", "zerossl":
		address, err := mail.ParseAddress(s.Email)
		if err != nil || address.Address != s.Email || len(s.Email) > 254 || strings.ContainsAny(s.Email, "\r\n\x00") {
			return errors.New("申请证书需要有效的联系邮箱")
		}
	case "files":
		if !SafeAbsolutePath(s.CertFile) || !SafeAbsolutePath(s.KeyFile) {
			return errors.New("cert 和 key 必须为绝对文件路径")
		}
	case "existing":
		if len(s.CertificateID) != 64 {
			return errors.New("请选择已有通配符证书")
		}
	default:
		return errors.New("SSL 模式无效")
	}
	return nil
}
func SafeAbsolutePath(path string) bool {
	return len(path) > 1 && len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n\t$\\")
}
func (r *CreateRequest) Validate() error {
	if !safeID.MatchString(r.RequestID) {
		return errors.New("提交标识无效，请重新提交")
	}
	if err := ValidateSSL(r.SSL); err != nil {
		return err
	}
	if r.Retry {
		if !ValidName(r.Name) || (r.SSL.Mode != "letsencrypt" && r.SSL.Mode != "zerossl") {
			return errors.New("SSL 重试参数无效")
		}
		return nil
	}
	var err error
	r.Domain, err = NormalizeDomain(r.Domain)
	if err != nil {
		return err
	}
	if len(r.Aliases) > 49 {
		return errors.New("每个站点最多绑定 50 个域名")
	}
	seen := map[string]bool{r.Domain: true}
	aliases := []string{}
	for _, alias := range r.Aliases {
		d, err := NormalizeDomain(alias)
		if err != nil {
			return err
		}
		if !seen[d] {
			aliases = append(aliases, d)
			seen[d] = true
		}
	}
	r.Aliases = aliases
	r.Name = r.Domain + ".conf"
	if r.Root == "" {
		r.Root = "/home/wwwroot/" + r.Domain
	}
	if !SafeAbsolutePath(r.Root) {
		return errors.New("站点目录必须是规范的绝对路径")
	}
	if r.PHP == "" {
		r.PHP = "default"
	}
	if r.Rewrite == "" {
		r.Rewrite = "none"
	}
	if r.LogName == "" {
		r.LogName = r.Domain + ".log"
	}
	if !logFilename.MatchString(r.LogName) {
		return errors.New("访问日志文件名无效，需以 .log 结尾")
	}
	if r.Database.Enabled {
		if !DatabaseName.MatchString(r.Database.Name) {
			return errors.New("数据库及同名用户名需为 1–32 位字母、数字或下划线")
		}
		if len(r.Database.Password) < 8 || len(r.Database.Password) > 256 || strings.ContainsRune(r.Database.Password, 0) {
			return errors.New("数据库密码需为 8–256 字节，不能包含空字符")
		}
		if len(r.Database.AdminPassword) > 256 || strings.ContainsRune(r.Database.AdminPassword, 0) {
			return errors.New("数据库管理员密码无效")
		}
	}
	return nil
}

// Paths are quoted as nginx tokens; dynamic variables are never accepted.
func NginxQuote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }

func RenderSite(r CreateRequest, php PHPRuntime, cert, key string) string {
	names := strings.Join(r.Domains(), " ")
	var out strings.Builder
	listen := func(port int, ssl bool) string {
		suffix := ""
		if ssl {
			suffix = " ssl"
		}
		s := fmt.Sprintf("    listen %d%s;\n", port, suffix)
		if r.IPv6 {
			s += fmt.Sprintf("    listen [::]:%d%s;\n", port, suffix)
		}
		return s
	}
	challenge := nginxLocation("    ", "^~ "+authChallenge, "alias "+NginxQuote(ChallengeDirectory+"/.well-known/acme-challenge/")+";", "default_type text/plain;") + "\n"
	accessLog := "    access_log off;\n"
	if r.AccessLog {
		accessLog = "    access_log " + NginxQuote(AccessLogDirectory+"/"+r.LogName) + ";\n"
	}
	secure := cert != ""
	if secure && r.SSL.Redirect {
		out.WriteString("server {\n" + listen(80, false) + "    server_name " + names + ";\n" + challenge + accessLog + nginxLocation("    ", "/", "return 301 https://"+r.Domain+"$request_uri;") + "\n}\n")
	}
	out.WriteString("# Managed by LNMP GoPanel\nserver {\n")
	if !secure || !r.SSL.Redirect {
		out.WriteString(listen(80, false))
	}
	if secure {
		out.WriteString(listen(443, true))
		out.WriteString("    ssl_certificate " + NginxQuote(cert) + ";\n    ssl_certificate_key " + NginxQuote(key) + ";\n    ssl_protocols TLSv1.2 TLSv1.3;\n    ssl_session_cache shared:GOPANELSSL:10m;\n")
	}
	out.WriteString("    server_name " + names + ";\n    root " + NginxQuote(r.Root) + ";\n    index index.html index.htm index.php default.html default.htm default.php;\n" + challenge)
	if r.Rewrite != "none" {
		out.WriteString("    include " + NginxQuote("rewrite/"+r.Rewrite+".conf") + ";\n")
	}
	if r.PHP != "none" {
		include := php.Include
		if r.PathInfo {
			include = php.PathInfoInclude
		}
		out.WriteString("    include " + NginxQuote(include) + ";\n")
	}
	out.WriteString(nginxLocation("    ", `~ /\.`, "deny all;") + "\n" + nginxLocation("    ", `~* \.(gif|jpg|jpeg|png|bmp|swf)$`, "expires 30d;") + "\n" + nginxLocation("    ", `~* \.(js|css)$`, "expires 12h;") + "\n")
	out.WriteString(accessLog)
	out.WriteString("}\n")
	return out.String()
}

func CheckCertificate(certPEM, keyPEM []byte, domains []string, now time.Time) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("证书或私钥格式无效，或两者不匹配")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, errors.New("证书无法解析")
	}
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, errors.New("证书已过期或尚未生效")
	}
	if len(leaf.ExtKeyUsage) > 0 {
		server := false
		for _, usage := range leaf.ExtKeyUsage {
			server = server || usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny
		}
		if !server {
			return nil, errors.New("证书不支持服务器身份验证")
		}
	}
	for _, domain := range domains {
		if leaf.VerifyHostname(domain) != nil {
			return nil, fmt.Errorf("证书未覆盖域名 %s", domain)
		}
	}
	return leaf, nil
}
