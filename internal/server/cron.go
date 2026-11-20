package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/cron"
	"github.com/gin-gonic/gin"
)

func (s *Server) cronToggle(c *gin.Context) {
	var input cron.ToggleRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Validate() != nil {
		fail(c, 400, cron.ErrInvalid.Error())
		return
	}
	writer, ok := s.Cron.(interface {
		ToggleCron(context.Context, cron.ToggleRequest) (cron.Snapshot, error)
	})
	if !ok {
		fail(c, 503, "定时任务开关未配置，请升级辅助程序及 sudoers 权限")
		return
	}
	view, err := writer.ToggleCron(c.Request.Context(), input)
	if err != nil {
		status := 503
		if errors.Is(err, cron.ErrInvalid) {
			status = 400
		} else if errors.Is(err, cron.ErrConflict) || errors.Is(err, control.ErrBusy) {
			status = 409
		}
		fail(c, status, err.Error())
		return
	}
	c.JSON(200, view)
}

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
			if !task.Enabled {
				c.JSON(200, cron.Preview{Line: task.Line, Schedule: task.Schedule, Times: []time.Time{}, Message: "任务已关闭，不会执行。"})
				return
			}
			c.JSON(200, cron.NextRuns(view, task, time.Now()))
			return
		}
	}
	fail(c, 404, "任务配置已变化，请刷新列表")
}
