// Package metricssetup implements the root-only installer wizard. No HTTP or
// sudo helper entry point is provided for privileged configuration changes.
package metricssetup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"gopkg.in/yaml.v3"
)

const nginxConfig = "/usr/local/nginx/conf/nginx.conf"
const nginxBinary = "/usr/local/nginx/sbin/nginx"
const managedConfig = "/usr/local/nginx/conf/panel-metrics.conf"
const fpmConfig = "/usr/local/php/etc/php-fpm.conf"
const fpmBinary = "/usr/local/php/sbin/php-fpm"
const managedHeader = "# Managed by LNMP GoPanel metrics configure; do not edit manually.\n"

var safePath = regexp.MustCompile(`^/[a-zA-Z0-9_./-]+$`)
var accountName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,31}$`)

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func validStatusPath(path string) bool {
	return safePath.MatchString(path) && filepath.Clean(path) == path && path != "/"
}

func addMetricsInclude(data []byte) ([]byte, error) {
	nodes, err := vhosts.ParseAuthConfig(string(data))
	if err != nil {
		return nil, err
	}
	var http *vhosts.AuthDirective
	for i := range nodes {
		if nodes[i].Name == "http" {
			if http != nil {
				return nil, errors.New("存在多个 http 块，请手动配置状态入口")
			}
			http = &nodes[i]
		}
	}
	if http == nil {
		return nil, errors.New("找不到 http 块，请手动配置状态入口")
	}
	count := 0
	for _, n := range http.Children {
		if n.Name == "include" && len(n.Args) == 1 && n.Args[0] == managedConfig {
			count++
		}
	}
	if count > 1 {
		return nil, errors.New("监控 include 重复，请先检查 Nginx 配置")
	}
	if count == 1 {
		return bytes.Clone(data), nil
	}
	insert := "\n    # LNMP GoPanel local metrics\n    include " + managedConfig + ";\n"
	at := http.Open + 1
	return []byte(string(data[:at]) + insert + string(data[at:])), nil
}

func renderNginx(port int, nginx bool, statusPath, listen string) ([]byte, error) {
	if port < 1024 || port > 65535 {
		return nil, errors.New("监控端口需在 1024–65535 之间")
	}
	var out strings.Builder
	out.WriteString(managedHeader)
	fmt.Fprintf(&out, "server {\n    listen 127.0.0.1:%d;\n    server_name 127.0.0.1;\n    access_log off;\n    allow 127.0.0.1;\n    deny all;\n", port)
	if nginx {
		out.WriteString("    location = /nginx_status {\n        stub_status;\n    }\n")
	}
	if statusPath != "" {
		if !validStatusPath(statusPath) || statusPath == "/nginx_status" {
			return nil, errors.New("PHP-FPM 状态路径无效或与 Nginx 状态路径冲突")
		}
		upstream := ""
		if strings.HasPrefix(listen, "/") {
			if !safePath.MatchString(listen) || filepath.Clean(listen) != listen {
				return nil, errors.New("PHP-FPM socket 路径不支持自动配置")
			}
			upstream = "unix:" + listen
		} else {
			host, port, err := net.SplitHostPort(listen)
			ip := net.ParseIP(host)
			number, e := strconv.Atoi(port)
			if err != nil || e != nil || number < 1 || number > 65535 || ip == nil || !ip.IsLoopback() {
				return nil, errors.New("PHP-FPM TCP 监听必须是明确的本机回环地址")
			}
			upstream = listen
		}
		fmt.Fprintf(&out, "    location = %s {\n        fastcgi_pass %s;\n        fastcgi_param QUERY_STRING $query_string;\n        fastcgi_param REQUEST_METHOD GET;\n        fastcgi_param SCRIPT_NAME %s;\n        fastcgi_param SCRIPT_FILENAME %s;\n        fastcgi_param REQUEST_URI $request_uri;\n        fastcgi_param SERVER_PROTOCOL $server_protocol;\n        fastcgi_param GATEWAY_INTERFACE CGI/1.1;\n        fastcgi_param SERVER_NAME $server_name;\n        fastcgi_param SERVER_PORT $server_port;\n        fastcgi_param REMOTE_ADDR $remote_addr;\n        fastcgi_param CONTENT_LENGTH 0;\n    }\n", statusPath, upstream, statusPath, statusPath)
	}
	out.WriteString("    location / { return 404; }\n}\n")
	return []byte(out.String()), nil
}

// Patch only the selected keys. yaml.Node preserves other fields, values and
// comments instead of serializing config.Load's inferred defaults into a file.
func patchPanel(data []byte, m config.MetricsConfig, selected map[string]bool) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, errors.New("面板 YAML 无效")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("面板配置必须是 YAML 映射")
	}
	root := document.Content[0]
	metricsNode, err := mapping(root, "metrics")
	if err != nil {
		return nil, err
	}
	set := func(n *yaml.Node, key, value string) {
		replaceNode(n, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	}
	if selected["nginx"] {
		set(metricsNode, "nginx_status_url", m.NginxStatusURL)
	}
	if selected["php_fpm"] {
		set(metricsNode, "php_fpm_status_url", m.PHPFPMStatusURL)
	}
	if selected["database"] {
		db, e := mapping(metricsNode, "mysql")
		if e != nil {
			return nil, e
		}
		set(db, "socket", m.MySQL.Socket)
		set(db, "address", m.MySQL.Address)
		set(db, "user", m.MySQL.User)
		set(db, "password", m.MySQL.Password)
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err = encoder.Encode(&document); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func mapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			n := parent.Content[i+1]
			if n.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("%s 必须是 YAML 映射", key)
			}
			return n, nil
		}
	}
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	replaceNode(parent, key, n)
	return n, nil
}
func replaceNode(parent *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			value.HeadComment = parent.Content[i+1].HeadComment
			value.LineComment = parent.Content[i+1].LineComment
			parent.Content[i+1] = value
			return
		}
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
