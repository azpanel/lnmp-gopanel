// Package lnmpinstall defines the installation protocol shared by the panel and
// its privileged helper. Requests never contain commands or filesystem paths.
package lnmpinstall

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
)

type Module struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Repository string `json:"repository"`
}

var Modules = []Module{
	{"vts", "VTS 流量统计", "https://github.com/vozlt/nginx-module-vts.git"},
	{"geoip2", "GeoIP2 地理信息", "https://github.com/leev/ngx_http_geoip2_module.git"},
	{"fancyindex", "FancyIndex 目录索引", "https://github.com/aperezdc/ngx-fancyindex.git"},
	{"substitutions_filter", "Substitutions Filter 内容替换", "https://github.com/yaoweibin/ngx_http_substitutions_filter_module.git"},
}

type Choice struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
type Options struct {
	Revision         string   `json:"revision"`
	Databases        []Choice `json:"databases"`
	PHP              []Choice `json:"php"`
	DefaultDatabase  int      `json:"default_database"`
	DefaultPHP       int      `json:"default_php"`
	DefaultAllocator int      `json:"default_allocator"`
	Architecture     string   `json:"architecture"`
	IPv4             string   `json:"ipv4"`
	Modules          []Module `json:"modules"`
}
type Request struct {
	ID               string   `json:"id"`
	Revision         string   `json:"revision"`
	Database         int      `json:"database"`
	Binary           bool     `json:"binary"`
	DatabasePassword string   `json:"database_password,omitempty"`
	InnoDB           bool     `json:"innodb"`
	PHP              int      `json:"php"`
	Allocator        int      `json:"allocator"`
	Protection       string   `json:"protection"`
	AuthUser         string   `json:"auth_user,omitempty"`
	AuthPassword     string   `json:"auth_password,omitempty"`
	IPv4             string   `json:"ipv4"`
	Modules          []string `json:"nginx_modules"`
}
type Job struct {
	ID              string            `json:"id"`
	Status          string            `json:"status"`
	Phase           string            `json:"phase"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Error           string            `json:"error,omitempty"`
	Options         *Options          `json:"options,omitempty"`
	Modules         []string          `json:"nginx_modules,omitempty"`
	ModuleRevisions map[string]string `json:"module_revisions,omitempty"`
	Result          string            `json:"result,omitempty"`
}
type Snapshot struct {
	Installed bool     `json:"installed"`
	Blocked   []string `json:"blocked"`
	Job       *Job     `json:"job,omitempty"`
}
type Line struct {
	ID    int64     `json:"id"`
	Time  time.Time `json:"time"`
	Phase string    `json:"phase"`
	Text  string    `json:"text"`
}
type ReadRequest struct {
	ID    string `json:"id"`
	After int64  `json:"after"`
}
type LogPage struct {
	Lines  []Line `json:"lines"`
	Cursor int64  `json:"cursor"`
	More   bool   `json:"more"`
}
type Backend interface {
	InstallInspect(context.Context) (Snapshot, error)
	InstallPrepare(context.Context, string) (Job, error)
	InstallStart(context.Context, Request) (Job, error)
	InstallRead(context.Context, ReadRequest) (LogPage, error)
}

var ErrConflict = errors.New("安装任务或服务器状态冲突")
var idPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var passwordPattern = regexp.MustCompile(`^[a-zA-Z0-9!@#$%^&*()_+=.,:?-]{12,72}$`)
var userPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,63}$`)

func ValidID(id string) bool    { return idPattern.MatchString(id) }
func Active(status string) bool { return status == "preparing" || status == "running" }
func BinaryAllowed(db int, arch string) bool {
	if db == 0 || db == 1 {
		return false
	}
	if arch == "amd64" || arch == "x86_64" {
		return true
	}
	if arch == "386" || arch == "i686" {
		return db <= 8 || db == 11 || db == 12 || db == 13
	}
	return (arch == "arm64" || arch == "aarch64") && (db == 5 || db >= 11)
}
func (r Request) Validate(o Options) error {
	if !ValidID(r.ID) || !revisionPattern.MatchString(r.Revision) || r.Revision != o.Revision {
		return errors.New("源码提交已变化，请重新准备安装")
	}
	has := func(choices []Choice, id int) bool {
		for _, c := range choices {
			if c.ID == id {
				return true
			}
		}
		return false
	}
	if !has(o.Databases, r.Database) || !has(o.PHP, r.PHP) || r.Allocator < 1 || r.Allocator > 3 {
		return errors.New("安装版本或内存分配器选项无效")
	}
	if r.PHP == 1 && r.Database == 0 {
		return errors.New("PHP 5.2 需要安装数据库")
	}
	if r.Binary && !BinaryAllowed(r.Database, o.Architecture) {
		return errors.New("该数据库版本或服务器架构不支持二进制安装")
	}
	if r.Database != 0 && !passwordPattern.MatchString(r.DatabasePassword) {
		return errors.New("数据库密码须为 12–72 位英文字母、数字或 !@#$%^&*()_+=.,:?-，以兼容安装脚本")
	}
	if r.Protection != "random" && r.Protection != "basic" {
		return errors.New("phpMyAdmin 保护方式无效")
	}
	if r.Protection == "basic" && (!userPattern.MatchString(r.AuthUser) || len(r.AuthPassword) < 12 || len(r.AuthPassword) > 128 || strings.ContainsAny(r.AuthPassword, "\x00\r\n")) {
		return errors.New("Basic Auth 用户名无效，或密码不满足 12–128 字节且无换行的要求")
	}
	ip := net.ParseIP(r.IPv4)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.Equal(net.IPv4bcast) || ip.String() != r.IPv4 {
		return errors.New("请输入有效的默认站点 IPv4")
	}
	if r.Modules == nil {
		return errors.New("必须提交 Nginx 模块选择（可为空数组）")
	}
	seen := map[string]bool{}
	for _, id := range r.Modules {
		known := false
		for _, m := range Modules {
			known = known || m.ID == id
		}
		if !known || seen[id] {
			return errors.New("Nginx 模块未知或重复")
		}
		seen[id] = true
	}
	return nil
}
