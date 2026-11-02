package backups

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Destination struct {
	Type     string `json:"type"`
	Path     string `json:"path,omitempty"`
	Account  string `json:"account,omitempty"`
	FolderID string `json:"folder_id,omitempty"`
	Remote   string `json:"remote,omitempty"`
}

type PathsRequest struct {
	Files        []FileSource  `json:"files"`
	Destinations []Destination `json:"destinations"`
}
type PathDiagnostic struct {
	Field   string `json:"field"`
	Index   int    `json:"index"`
	Message string `json:"message"`
}
type PathsResult struct {
	Errors   []PathDiagnostic `json:"errors"`
	Warnings []PathDiagnostic `json:"warnings"`
}

var driveFolderID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func safeDestinationText(v string, limit int) bool {
	return utf8.ValidString(v) && len(v) <= limit && strings.IndexFunc(v, unicode.IsControl) == -1
}
func ValidateDestination(d Destination) error {
	switch d.Type {
	case "local":
		if !safeDestinationText(d.Path, 4096) || !filepath.IsAbs(d.Path) || filepath.Clean(d.Path) != d.Path || d.Account != "" || d.FolderID != "" || d.Remote != "" {
			return invalidPolicy("本地存储位置须为规范的绝对目录")
		}
	case "gdrive":
		if d.Account == "" || !safeDestinationText(d.Account, 256) || !driveFolderID.MatchString(d.FolderID) || len(d.FolderID) > 256 || d.Path != "" || d.Remote != "" {
			return invalidPolicy("请选择 gdrive 账户并填写文件夹 ID")
		}
	case "rclone":
		if d.Remote == "" || !safeDestinationText(d.Remote, 256) || !safeDestinationText(d.Path, 4096) || strings.ContainsAny(d.Path, ":\\") || d.Account != "" || d.FolderID != "" {
			return invalidPolicy("请选择 rclone 远程并填写有效的远程内路径")
		}
		for _, part := range strings.Split(d.Path, "/") {
			if part == ".." {
				return invalidPolicy("远程内路径不能包含上级目录")
			}
		}
	default:
		return invalidPolicy("存储位置类型无效")
	}
	return nil
}
func DestinationKey(d Destination) string {
	p := d.Path
	if d.Type == "rclone" {
		// Preserve absolute-vs-relative semantics for backends such as SFTP.
		p = path.Clean(d.Path)
	}
	return d.Type + "\x00" + d.Account + "\x00" + d.Remote + "\x00" + d.FolderID + "\x00" + p
}
func ValidateDestinations(destinations []Destination, enabled bool) error {
	if len(destinations) > 100 {
		return invalidPolicy("最多添加 100 个存储位置")
	}
	if enabled && len(destinations) == 0 {
		return invalidPolicy("启用政策至少需要一个存储位置")
	}
	seen := map[string]bool{}
	for _, d := range destinations {
		if err := ValidateDestination(d); err != nil {
			return err
		}
		key := DestinationKey(d)
		if seen[key] {
			return invalidPolicy("存储位置重复")
		}
		seen[key] = true
	}
	return nil
}
