//go:build linux

package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/go-sql-driver/mysql"
)

func openBackupDatabase(ctx context.Context, password string) (*sql.DB, error) {
	c := localDatabaseConfig(password)
	c.User = "root"
	connector, err := mysql.NewConnector(c)
	if err != nil {
		return nil, errors.New("数据库连接配置无效")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("无法认证本机数据库管理员，请输入管理员密码重试")
	}
	return db, nil
}
func inspectBackupDatabase(ctx context.Context, action string, r backups.DatabaseInspectRequest) (any, error) {
	if len(r.Password) > 4096 || len(r.Search) > 256 || r.Page < 0 || r.Page > 100000 {
		return nil, backups.ErrInvalid
	}
	if action == "tables" {
		if err := backups.ValidateNames([]string{r.Database}); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	db, err := openBackupDatabase(ctx, r.Password)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return queryBackupDatabase(ctx, db, action, r)
}
func queryBackupDatabase(ctx context.Context, db *sql.DB, action string, r backups.DatabaseInspectRequest) (any, error) {
	if action == "databases" {
		rows, err := db.QueryContext(ctx, `SELECT s.SCHEMA_NAME, COALESCE(SUM(COALESCE(t.DATA_LENGTH,0)+COALESCE(t.INDEX_LENGTH,0)),0) FROM information_schema.SCHEMATA s LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA=s.SCHEMA_NAME GROUP BY s.SCHEMA_NAME ORDER BY s.SCHEMA_NAME`)
		if err != nil {
			return nil, errors.New("数据库清单读取失败，请确认管理员权限")
		}
		defer rows.Close()
		items := []backups.DatabaseItem{}
		for rows.Next() {
			var item backups.DatabaseItem
			if err = rows.Scan(&item.Name, &item.Size); err != nil {
				return nil, errors.New("数据库大小读取失败")
			}
			items = append(items, item)
		}
		if rows.Err() != nil {
			return nil, errors.New("数据库清单读取中断")
		}
		return items, nil
	}
	page := r.Page
	if page < 1 {
		page = 1
	}
	result := backups.TableList{Items: []backups.TableItem{}, Page: page, PageSize: 100}
	// LOCATE performs a literal substring search: names never become SQL syntax.
	where := ` FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND LOCATE(?,TABLE_NAME)>0`
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*)"+where, r.Database, r.Search).Scan(&result.Total); err != nil {
		return nil, errors.New("表清单读取失败，请确认管理员权限")
	}
	rows, err := db.QueryContext(ctx, `SELECT TABLE_NAME,TABLE_TYPE,CASE WHEN TABLE_TYPE='VIEW' THEN NULL ELSE COALESCE(DATA_LENGTH,0)+COALESCE(INDEX_LENGTH,0) END`+where+` ORDER BY TABLE_NAME LIMIT ? OFFSET ?`, r.Database, r.Search, result.PageSize, (page-1)*result.PageSize)
	if err != nil {
		return nil, errors.New("表清单读取失败")
	}
	defer rows.Close()
	for rows.Next() {
		var item backups.TableItem
		var size sql.NullInt64
		if err = rows.Scan(&item.Name, &item.Type, &size); err != nil {
			return nil, errors.New("表大小读取失败")
		}
		if size.Valid {
			item.Size = &size.Int64
		}
		result.Items = append(result.Items, item)
	}
	if rows.Err() != nil {
		return nil, errors.New("表清单读取中断")
	}
	return result, nil
}
func virtualBackupPath(path string) bool {
	for _, root := range []string{"/proc", "/sys", "/dev"} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}
func backupSourceInfo(source backups.FileSource) (os.FileInfo, error) {
	if err := backups.ValidateFileSource(source); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(source.Path)
	if err != nil {
		return nil, fmt.Errorf("%w：路径不存在或不可访问：%s", backups.ErrInvalid, source.Path)
	}
	if virtualBackupPath(resolved) {
		return nil, fmt.Errorf("%w：不支持虚拟文件系统路径", backups.ErrInvalid)
	}
	info, err := os.Stat(resolved)
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return nil, fmt.Errorf("%w：来源必须是普通文件或目录", backups.ErrInvalid)
	}
	if !info.IsDir() && len(source.Excludes) > 0 {
		return nil, fmt.Errorf("%w：普通文件不能设置目录排除规则", backups.ErrInvalid)
	}
	return info, nil
}
func excludedBackupPath(relative string, patterns []string) bool {
	// Match ancestors too: excluding a directory always excludes its descendants.
	for path := relative; path != "." && path != ""; path = filepath.ToSlash(filepath.Dir(path)) {
		for _, pattern := range patterns {
			matched, _ := doublestar.Match(strings.TrimSuffix(pattern, "/"), path)
			if matched {
				return true
			}
		}
	}
	return false
}
func inspectBackupFile(ctx context.Context, source backups.FileSource) (backups.FileSize, error) {
	result := backups.FileSize{Path: source.Path}
	info, err := backupSourceInfo(source)
	if err != nil {
		return result, err
	}
	result.Valid = true
	result.Complete = true
	if !info.IsDir() {
		result.Type = "file"
		result.OriginalBytes = info.Size()
		result.IncludedBytes = info.Size()
		return result, nil
	}
	result.Type = "directory"
	resolvedRoot, err := filepath.EvalSymlinks(source.Path)
	if err != nil || virtualBackupPath(resolvedRoot) {
		return result, fmt.Errorf("%w：目录路径已变化或不支持", backups.ErrInvalid)
	}
	root, err := os.OpenRoot(resolvedRoot)
	if err != nil {
		return result, fmt.Errorf("%w：目录不可读取", backups.ErrInvalid)
	}
	defer root.Close()
	// Keep traversal anchored to an open descriptor and do not follow symlinks.
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			result.Complete = false
			result.Error = "部分路径不可访问，大小统计不完整"
			return nil
		}
		absolute := filepath.Join(resolvedRoot, path)
		if virtualBackupPath(absolute) {
			return fs.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			result.Complete = false
			result.Error = "扫描期间文件发生变化，大小统计不完整"
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		result.OriginalBytes += info.Size()
		if !excludedBackupPath(filepath.ToSlash(path), source.Excludes) {
			result.IncludedBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		result.Complete = false
		result.Error = "扫描超时或被取消，当前为部分统计结果"
	}
	return result, nil
}
