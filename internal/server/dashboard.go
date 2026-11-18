package server

import (
	"github.com/azpanel/lnmp-gopanel/internal/metrics"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerDashboard(private *gin.RouterGroup) {
	private.GET("/dashboard/traffic", func(c *gin.Context) {
		period := c.DefaultQuery("period", "day")
		if period != "day" && period != "hour" {
			fail(c, 400, "period 仅支持 day 或 hour")
			return
		}
		if s.Traffic == nil {
			fail(c, 503, "网站流量统计不可用")
			return
		}
		data, err := s.Traffic.Read(c.Request.Context(), period)
		if err != nil {
			fail(c, 503, err.Error())
			return
		}
		c.JSON(200, data)
	})
	private.GET("/dashboard/widgets", func(c *gin.Context) {
		if s.Widgets == nil {
			fail(c, 503, "状态指标配置不可用，请检查 metrics.state_dir 后重启面板")
			return
		}
		c.JSON(200, s.Widgets.Get())
	})
	private.PUT("/dashboard/widgets", s.csrf, func(c *gin.Context) {
		if s.Widgets == nil {
			fail(c, 503, "状态指标配置不可用，请检查 metrics.state_dir 后重启面板")
			return
		}
		var req metrics.Widgets
		if !authBody(c, &req) {
			return
		}
		if metrics.ValidateWidgets(req) != nil {
			fail(c, 400, metrics.ErrWidgetsInput.Error())
			return
		}
		if err := s.Widgets.Save(req, s.Metrics.SetEnabled); err != nil {
			fail(c, 503, "状态指标配置保存失败，请检查状态目录及文件权限")
			return
		}
		c.JSON(200, s.Widgets.Get())
	})
}
