package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerVhostBasicAuth(private *gin.RouterGroup) {
	private.GET("/vhosts/basic-auth", func(c *gin.Context) {
		if s.Vhosts == nil {
			fail(c, 503, "站点管理未配置")
			return
		}
		name := c.Query("name")
		if !vhosts.ValidName(name) {
			fail(c, 400, "配置文件名无效")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		state, err := s.Vhosts.BasicAuth(ctx, name)
		if err != nil {
			fail(c, 502, err.Error())
			return
		}
		c.JSON(200, state)
	})
	change := func(disable bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			if s.Vhosts == nil {
				fail(c, 503, "站点管理未配置")
				return
			}
			var input vhosts.BasicAuthInput
			if disable {
				input.Name = c.Query("name")
			} else {
				d := json.NewDecoder(c.Request.Body)
				d.DisallowUnknownFields()
				if err := d.Decode(&input); err != nil {
					fail(c, 400, "认证参数无效")
					return
				}
				var extra any
				if err := d.Decode(&extra); err != io.EOF {
					fail(c, 400, "认证参数无效")
					return
				}
				if err := input.Validate(); err != nil {
					fail(c, 400, err.Error())
					return
				}
			}
			if !vhosts.ValidName(input.Name) {
				fail(c, 400, "配置文件名无效")
				return
			}
			state, err := s.Vhosts.ChangeBasicAuth(input, disable)
			slog.Info("vhost basic auth operation", "name", input.Name, "disable", disable, "success", err == nil)
			if err != nil {
				fail(c, 502, err.Error())
				return
			}
			c.JSON(200, state)
		}
	}
	private.PUT("/vhosts/basic-auth", s.csrf, change(false))
	private.DELETE("/vhosts/basic-auth", s.csrf, change(true))
}
