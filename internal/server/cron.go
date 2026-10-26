package server

import (
	"strconv"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/cron"
	"github.com/gin-gonic/gin"
)

func (s *Server) cronPreview(c *gin.Context) {
	line, err := strconv.Atoi(c.Query("line"))
	if err != nil || line <= 0 {
		fail(c, 400, "任务行号无效")
		return
	}
	if s.Cron == nil {
		fail(c, 503, "定时任务读取未配置")
		return
	}
	view, err := s.Cron.Cron(c.Request.Context())
	if err != nil {
		fail(c, 503, err.Error())
		return
	}
	for _, task := range view.Tasks {
		if task.Line == line {
			c.JSON(200, cron.NextRuns(view, task, time.Now()))
			return
		}
	}
	fail(c, 404, "任务配置已变化，请刷新列表")
}
