package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"path/filepath"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/ipgeo"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/gin-gonic/gin"
)

func (s *Server) logSlot(c *gin.Context) bool {
	select {
	case s.logReads <- struct{}{}:
		return true
	default:
		fail(c, 429, "日志读取繁忙，请稍后重试")
		return false
	}
}
func (s *Server) logList(c *gin.Context) {
	if s.Logs == nil {
		fail(c, 503, "日志读取未配置")
		return
	}
	if !s.logSlot(c) {
		return
	}
	defer func() { <-s.logReads }()
	ctx, cancel := context.WithTimeout(c.Request.Context(), logs.Timeout)
	defer cancel()
	out, err := s.Logs.List(ctx)
	if err != nil {
		fail(c, 503, err.Error())
		return
	}
	if s.CustomLogsError != "" {
		out.Warnings = append(out.Warnings, s.CustomLogsError)
	}
	if s.CustomLogs != nil {
		for _, path := range s.CustomLogs.Paths() {
			file, err := s.logFileInfo(ctx, path)
			file.Custom = true
			if err != nil {
				file.Error = err.Error()
			}
			found := false
			for i := range out.Files {
				if out.Files[i].Path == path {
					out.Files[i], found = file, true
					break
				}
			}
			if !found {
				out.Files = append(out.Files, file)
			}
		}
	}
	c.JSON(200, out)
}

func (s *Server) logFileInfo(ctx context.Context, path string) (logs.File, error) {
	file := logs.File{Path: path}
	if err := ctx.Err(); err != nil {
		return file, err
	}
	out, err := s.Logs.Read(ctx, logs.Request{Path: path, Mode: "stat"})
	if err != nil {
		return file, err
	}
	file.Size, file.Modified, file.Available = out.Size, out.Modified, true
	return file, nil
}

func customLogPath(c *gin.Context) (string, error) {
	var input struct {
		Path string `json:"path"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return "", logs.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return "", logs.ErrInvalid
	}
	return input.Path, logs.ValidatePath(input.Path)
}

func (s *Server) logAdd(c *gin.Context) {
	path, err := customLogPath(c)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	if s.CustomLogs == nil || s.Logs == nil {
		fail(c, 503, "自定义日志列表不可用，请检查状态目录及日志辅助程序")
		return
	}
	if !s.logSlot(c) {
		return
	}
	defer func() { <-s.logReads }()
	ctx, cancel := context.WithTimeout(c.Request.Context(), logs.Timeout)
	defer cancel()
	file, err := s.logFileInfo(ctx, path)
	if err == nil && !logs.BuiltinPath(path) {
		err = s.CustomLogs.Add(path)
		file.Custom = true
	}
	if err != nil {
		status := 503
		if errors.Is(err, logs.ErrInvalid) {
			status = 400
		}
		fail(c, status, err.Error())
		return
	}
	c.JSON(200, file)
}

func (s *Server) logRemove(c *gin.Context) {
	path, err := customLogPath(c)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	if s.CustomLogs == nil {
		fail(c, 503, "自定义日志列表不可用，请检查状态目录")
		return
	}
	if err := s.CustomLogs.Remove(path); err != nil {
		fail(c, 503, "无法保存自定义日志列表")
		return
	}
	c.Status(204)
}

func (s *Server) logRead(c *gin.Context) {
	var req logs.Request
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		fail(c, 400, "日志请求无效或超过 4 KiB")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(c, 400, "日志请求无效")
		return
	}
	if err := logs.Validate(req); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if !logs.BuiltinPath(req.Path) && !s.CustomLogs.Contains(req.Path) {
		fail(c, 400, "请先添加此自定义日志文件")
		return
	}
	if s.Logs == nil {
		fail(c, 503, "日志读取未配置")
		return
	}
	if !s.logSlot(c) {
		return
	}
	defer func() { <-s.logReads }()
	ctx, cancel := context.WithTimeout(c.Request.Context(), logs.Timeout)
	defer cancel()
	out, err := s.Logs.Read(ctx, req)
	if err != nil {
		status := 503
		if errors.Is(err, logs.ErrInvalid) {
			status = 400
		}
		if errors.Is(err, logs.ErrChanged) {
			status = 409
		}
		fail(c, status, err.Error())
		return
	}
	enrichLogLocations(req.Path, out.Lines)
	c.JSON(200, out)
}

func enrichLogLocations(path string, lines []logs.Line) {
	if dir := filepath.Dir(path); dir != logs.Directory && dir != logs.ManagedDirectory {
		return
	}
	locations := make(map[string]string)
	for i := range lines {
		text := lines[i].Text
		end := strings.IndexAny(text, " \t")
		if end <= 0 {
			continue
		}
		client := text[:end]
		addr, err := netip.ParseAddr(client)
		if err != nil || addr.Zone() != "" {
			continue
		}
		location, ok := locations[client]
		if !ok {
			location = ipgeo.Lookup(client)
			locations[client] = location
		}
		lines[i].IPLocation = location
	}
}
