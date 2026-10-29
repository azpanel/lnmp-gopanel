package server

import (
	"log/slog"

	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/gin-gonic/gin"
)

func (s *Server) isLoginLog(path string) bool {
	return s.LoginLog != nil && s.LoginLog.Path() == path
}

func (s *Server) writeLoginEvent(c *gin.Context, event logs.LoginEvent) {
	if s.LoginLog == nil {
		return
	}
	event.IP, event.UA = c.ClientIP(), c.Request.UserAgent()
	if err := s.LoginLog.Append(event); err != nil {
		slog.Warn("登录日志写入失败", "error", err)
	}
}

func (s *Server) auditLoginFailure(c *gin.Context, event *logs.LoginEvent) {
	status := c.Writer.Status()
	if status < 400 {
		return
	}
	event.Result = "failure"
	switch status {
	case 400, 415:
		event.Reason = "invalid_request"
	case 429:
		event.Reason = "rate_limited"
	case 401, 403:
		event.Reason = "invalid_credentials"
		if event.Stage == "totp" {
			event.Reason = "invalid_second_factor"
		}
	default:
		event.Reason = "authentication_unavailable"
	}
	s.writeLoginEvent(c, *event)
}
