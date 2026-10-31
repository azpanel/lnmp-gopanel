package server

import (
	"errors"
	"io"
	"net/http"

	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/gin-gonic/gin"
)

func loggingFailure(c *gin.Context, err error) {
	status := http.StatusConflict
	if errors.Is(err, control.ErrUnsupported) {
		status = http.StatusBadRequest
	}
	fail(c, status, err.Error())
}
func (s *Server) loggingStatus(c *gin.Context) {
	if s.Services == nil {
		fail(c, 503, "日志设置未配置")
		return
	}
	result, err := s.Services.LoggingStatus(c.Request.Context())
	if err != nil {
		fail(c, 503, err.Error())
		return
	}
	c.JSON(200, result)
}
func (s *Server) enableLogging(c *gin.Context) {
	kind := c.Param("kind")
	if kind != "php" && kind != "database" {
		fail(c, 400, "日志类型无效")
		return
	}
	// This operation has no client-configurable paths, commands or thresholds.
	data, err := io.ReadAll(c.Request.Body)
	if err != nil || len(data) != 0 {
		fail(c, 400, "开启日志不接受请求参数")
		return
	}
	if s.Services == nil {
		fail(c, 503, "日志设置未配置")
		return
	}
	result, err := s.Services.EnableLogging(c.Request.Context(), kind)
	if err != nil {
		loggingFailure(c, err)
		return
	}
	c.JSON(200, result)
}
