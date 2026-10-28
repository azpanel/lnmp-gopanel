package server

import (
	"encoding/json"
	"io"
	"log/slog"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerVhostFormat(private *gin.RouterGroup) {
	private.POST("/vhosts/config/format", s.csrf, func(c *gin.Context) {
		if s.Vhosts == nil {
			fail(c, 503, "站点管理未配置")
			return
		}
		var input vhosts.FormatInput
		decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 4097))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			fail(c, 400, "格式化参数无效")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			fail(c, 400, "格式化参数无效")
			return
		}
		if err := input.Validate(); err != nil {
			fail(c, 400, err.Error())
			return
		}
		result, err := s.Vhosts.Format(input)
		slog.Info("vhost format operation", "name", input.Name, "success", err == nil)
		if err != nil {
			fail(c, 502, err.Error())
			return
		}
		c.JSON(200, result)
	})
}
