package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"

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
	c.JSON(200, out)
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
	c.JSON(200, out)
}
