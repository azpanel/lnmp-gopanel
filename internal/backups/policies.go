package backups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	cronpreview "github.com/azpanel/lnmp-gopanel/internal/cron"
	"github.com/bmatcuk/doublestar/v4"
)

var ErrNotFound = errors.New("备份政策不存在")
var ErrConflict = errors.New("备份政策已被修改，请刷新后重试")

const MaxPolicyBytes = 256 * 1024

type Compression struct {
	Format      string `json:"format"`
	Level       int    `json:"level"`
	HasPassword bool   `json:"has_password"`
}
type Retention struct {
	Databases int `json:"databases"`
	Files     int `json:"files"`
}
type TableRule struct {
	Database string   `json:"database"`
	Mode     string   `json:"mode"`
	Tables   []string `json:"tables"`
}
type DatabaseSelection struct {
	Enabled  bool        `json:"enabled"`
	Mode     string      `json:"mode"`
	Excluded []string    `json:"excluded"`
	Selected []string    `json:"selected"`
	Rules    []TableRule `json:"rules"`
}
type FileSource struct {
	Path     string   `json:"path"`
	Excludes []string `json:"excludes"`
}
type Policy struct {
	ID           string            `json:"id"`
	Revision     uint64            `json:"revision"`
	Name         string            `json:"name"`
	Notes        string            `json:"notes"`
	Enabled      bool              `json:"enabled"`
	Schedule     string            `json:"schedule"`
	Compression  Compression       `json:"compression"`
	Retention    Retention         `json:"retention"`
	Databases    DatabaseSelection `json:"databases"`
	Files        []FileSource      `json:"files"`
	Destinations []Destination     `json:"destinations"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}
type SavePolicyRequest struct {
	Policy
	PasswordAction string          `json:"password_action"`
	Password       string          `json:"password"`
	CopyFrom       *PolicyIdentity `json:"copy_from,omitempty"`
}
type PolicyIdentity struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}
type PolicySummary struct {
	ID              string        `json:"id"`
	Revision        uint64        `json:"revision"`
	Name            string        `json:"name"`
	Enabled         bool          `json:"enabled"`
	Schedule        string        `json:"schedule"`
	Compression     Compression   `json:"compression"`
	Retention       Retention     `json:"retention"`
	DatabaseEnabled bool          `json:"database_enabled"`
	DatabaseMode    string        `json:"database_mode"`
	DatabaseCount   int           `json:"database_count"`
	FileCount       int           `json:"file_count"`
	UpdatedAt       time.Time     `json:"updated_at"`
	Destinations    []Destination `json:"destinations"`
}

func (p Policy) Summary() PolicySummary {
	return PolicySummary{p.ID, p.Revision, p.Name, p.Enabled, p.Schedule, p.Compression, p.Retention, p.Databases.Enabled, p.Databases.Mode, len(p.Databases.Selected), len(p.Files), p.UpdatedAt, p.Destinations}
}
func ValidPolicyID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func invalidPolicy(message string) error { return fmt.Errorf("%w：%s", ErrInvalid, message) }
func ValidateNames(names []string) error {
	if len(names) > 10000 {
		return invalidPolicy("库表选择项过多")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || !utf8.ValidString(name) || len([]rune(name)) > 64 || strings.ContainsRune(name, 0) || seen[name] {
			return invalidPolicy("库表名称无效或重复")
		}
		seen[name] = true
	}
	return nil
}
func ValidateFileSource(source FileSource) error {
	if !filepath.IsAbs(source.Path) || filepath.Clean(source.Path) != source.Path || strings.ContainsRune(source.Path, 0) || len(source.Path) > 4096 {
		return invalidPolicy("请填写规范的绝对路径")
	}
	if len(source.Excludes) > 200 {
		return invalidPolicy("每个目录最多设置 200 条排除规则")
	}
	for _, pattern := range source.Excludes {
		if pattern == "" || len(pattern) > 1024 || strings.HasPrefix(pattern, "/") || strings.ContainsRune(pattern, 0) || !doublestar.ValidatePattern(pattern) {
			return invalidPolicy("排除规则无效")
		}
		for _, part := range strings.Split(pattern, "/") {
			if part == ".." {
				return invalidPolicy("排除规则不能越出备份目录")
			}
		}
	}
	return nil
}
func ValidateSchedule(expression string) error {
	if len(expression) > 256 || len(strings.Fields(expression)) != 5 {
		return invalidPolicy("请输入五字段 Cron 表达式")
	}
	_, err := cronpreview.ParseSchedule(expression)
	if err != nil {
		return invalidPolicy("Cron 表达式无效")
	}
	return nil
}
func SchedulePreview(expression string, now time.Time) (cronpreview.Preview, error) {
	if err := ValidateSchedule(expression); err != nil {
		return cronpreview.Preview{}, err
	}
	result := cronpreview.NextRuns(cronpreview.Snapshot{}, cronpreview.Task{Schedule: expression}, now)
	if len(result.Times) == 0 {
		return result, invalidPolicy("Cron 表达式在未来五年没有匹配时间")
	}
	return result, nil
}
func ValidatePolicy(r SavePolicyRequest) error {
	p := r.Policy
	if r.CopyFrom != nil && (p.ID != "" || !ValidPolicyID(r.CopyFrom.ID) || r.CopyFrom.Revision == 0) {
		return invalidPolicy("复制来源无效，复制只能创建新政策")
	}
	if err := ValidateDestinations(p.Destinations, p.Enabled); err != nil {
		return err
	}
	if p.ID != "" && !ValidPolicyID(p.ID) {
		return invalidPolicy("政策标识无效")
	}
	if strings.TrimSpace(p.Name) == "" || !utf8.ValidString(p.Name) || len([]rune(p.Name)) > 80 || len([]rune(p.Notes)) > 1000 {
		return invalidPolicy("名称须为 1–80 字，备注最多 1000 字")
	}
	if _, err := SchedulePreview(p.Schedule, time.Now()); err != nil {
		return err
	}
	validLevel := p.Compression.Level >= 0 && p.Compression.Level <= 9
	if p.Compression.Format == "7z" {
		validLevel = p.Compression.Level == 0 || p.Compression.Level == 1 || p.Compression.Level == 3 || p.Compression.Level == 5 || p.Compression.Level == 7 || p.Compression.Level == 9
	} else if p.Compression.Format != "zip" {
		return invalidPolicy("压缩格式须为 ZIP 或 7z")
	}
	if !validLevel {
		return invalidPolicy("压缩等级无效")
	}
	if p.Retention.Databases < 1 || p.Retention.Databases > 1000000 || p.Retention.Files < 1 || p.Retention.Files > 1000000 {
		return invalidPolicy("保留份数须为 1–1000000")
	}
	if r.PasswordAction != "keep" && r.PasswordAction != "replace" && r.PasswordAction != "clear" {
		return invalidPolicy("密码操作无效")
	}
	if len(r.Password) > 4096 || !utf8.ValidString(r.Password) || (r.PasswordAction == "replace" && r.Password == "") || (r.PasswordAction != "replace" && r.Password != "") {
		return invalidPolicy("压缩密码设置无效")
	}
	db := p.Databases
	if db.Mode != "all" && db.Mode != "selected" {
		return invalidPolicy("数据库选择模式无效")
	}
	if err := ValidateNames(db.Excluded); err != nil {
		return err
	}
	if err := ValidateNames(db.Selected); err != nil {
		return err
	}
	if db.Enabled && db.Mode == "selected" && len(db.Selected) == 0 {
		return invalidPolicy("请至少选择一个数据库")
	}
	seen := map[string]bool{}
	for _, rule := range db.Rules {
		if err := ValidateNames([]string{rule.Database}); err != nil {
			return err
		}
		if seen[rule.Database] {
			return invalidPolicy("数据库表规则重复")
		}
		seen[rule.Database] = true
		if rule.Mode != "all" && rule.Mode != "include" && rule.Mode != "exclude" {
			return invalidPolicy("表选择模式无效")
		}
		if err := ValidateNames(rule.Tables); err != nil {
			return err
		}
		active := db.Enabled && ((db.Mode == "all" && !slices.Contains(db.Excluded, rule.Database)) || (db.Mode == "selected" && slices.Contains(db.Selected, rule.Database)))
		if active && rule.Mode == "include" && len(rule.Tables) == 0 {
			return invalidPolicy("仅备份所选表时至少选择一张表")
		}
	}
	if len(p.Files) > 100 {
		return invalidPolicy("最多添加 100 个文件来源")
	}
	seen = map[string]bool{}
	for _, source := range p.Files {
		if err := ValidateFileSource(source); err != nil {
			return err
		}
		if seen[source.Path] {
			return invalidPolicy("文件来源重复")
		}
		seen[source.Path] = true
	}
	if !db.Enabled && len(p.Files) == 0 {
		return invalidPolicy("请至少配置数据库或文件来源")
	}
	if data, err := json.Marshal(r); err != nil || len(data) > MaxPolicyBytes {
		return invalidPolicy("政策内容超过大小限制")
	}
	return nil
}

type PolicyBackend interface {
	BackupPolicy(context.Context, string, any) (json.RawMessage, error)
}
type DatabaseInspectRequest struct {
	Password string `json:"password"`
	Database string `json:"database"`
	Search   string `json:"search"`
	Page     int    `json:"page"`
}
type DatabaseItem struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}
type TableItem struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size *int64 `json:"size"`
}
type TableList struct {
	Items    []TableItem `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}
type FileSize struct {
	Path          string        `json:"path"`
	Type          string        `json:"type"`
	Valid         bool          `json:"valid"`
	OriginalBytes int64         `json:"original_bytes"`
	IncludedBytes int64         `json:"included_bytes"`
	Complete      bool          `json:"complete"`
	Error         string        `json:"error,omitempty"`
	Rules         []RulePreview `json:"rules"`
}
type RulePreview struct {
	Pattern   string   `json:"pattern"`
	Count     int64    `json:"count"`
	Samples   []string `json:"samples"`
	Truncated bool     `json:"truncated"`
}
