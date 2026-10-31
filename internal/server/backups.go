package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerBackupComponents(r *gin.RouterGroup) {
	r.GET("/backups/components", s.backupComponents)
	r.POST("/backups/components/install", s.csrf, s.installBackupComponents)
}
func backupFailure(c *gin.Context, err error) {
	status := 502
	if errors.Is(err, backups.ErrBusy) {
		status = 409
	}
	if errors.Is(err, backups.ErrInvalid) {
		status = 400
	}
	fail(c, status, err.Error())
}
func (s *Server) backupComponents(c *gin.Context) {
	if s.Backups == nil {
		fail(c, 503, "备份组件查询未配置")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	result, err := s.Backups.BackupComponents(ctx)
	if err != nil {
		backupFailure(c, err)
		return
	}
	c.JSON(200, result)
}
func (s *Server) installBackupComponents(c *gin.Context) {
	var input backups.Request
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&input) != nil || d.Decode(&extra) != io.EOF || input.Validate() != nil {
		fail(c, 400, backups.ErrInvalid.Error())
		return
	}
	if s.Backups == nil {
		fail(c, 503, "备份组件安装未配置")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	result, err := s.Backups.InstallBackupComponents(ctx, input)
	if err != nil {
		backupFailure(c, err)
		return
	}
	c.JSON(202, result)
}
