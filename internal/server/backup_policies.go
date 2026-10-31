package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"github.com/gin-gonic/gin"
)

func (s *Server) registerBackupPolicies(r *gin.RouterGroup) {
	r.GET("/backups/policies", func(c *gin.Context) { s.policyCall(c, "list", struct{}{}, 20*time.Second) })
	r.GET("/backups/policies/:id", func(c *gin.Context) {
		s.policyCall(c, "get", backups.PolicyIdentity{ID: c.Param("id")}, 20*time.Second)
	})
	r.POST("/backups/policies", s.csrf, s.saveBackupPolicy)
	r.PUT("/backups/policies/:id", s.csrf, s.saveBackupPolicy)
	r.DELETE("/backups/policies/:id", s.csrf, func(c *gin.Context) {
		var input backups.PolicyIdentity
		if !policyBody(c, &input) {
			return
		}
		input.ID = c.Param("id")
		s.policyCall(c, "delete", input, 20*time.Second)
	})
	r.POST("/backups/schedule/preview", s.csrf, func(c *gin.Context) {
		var input struct {
			Schedule string `json:"schedule"`
		}
		if !policyBody(c, &input) {
			return
		}
		result, err := backups.SchedulePreview(input.Schedule, time.Now())
		if err != nil {
			policyFailure(c, err)
			return
		}
		c.JSON(200, result)
	})
	for path, action := range map[string]string{"/backups/databases/inspect": "databases", "/backups/databases/tables": "tables"} {
		r.POST(path, s.csrf, func(c *gin.Context) {
			var input backups.DatabaseInspectRequest
			if !policyBody(c, &input) {
				return
			}
			s.policyCall(c, action, input, 18*time.Second)
		})
	}
	r.POST("/backups/files/inspect", s.csrf, func(c *gin.Context) {
		var input backups.FileSource
		if !policyBody(c, &input) {
			return
		}
		if err := backups.ValidateFileSource(input); err != nil {
			policyFailure(c, err)
			return
		}
		select {
		case s.backupScanSlots <- struct{}{}:
			defer func() { <-s.backupScanSlots }()
		default:
			fail(c, 429, "正在统计其他路径，请稍后重试")
			return
		}
		s.policyCall(c, "files", input, 60*time.Second)
	})
}
func policyBody(c *gin.Context, input any) bool {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(input) != nil || d.Decode(&extra) != io.EOF {
		fail(c, 400, "备份政策请求参数无效")
		return false
	}
	return true
}
func policyFailure(c *gin.Context, err error) {
	status := 502
	switch {
	case errors.Is(err, backups.ErrInvalid):
		status = 400
	case errors.Is(err, backups.ErrNotFound):
		status = 404
	case errors.Is(err, backups.ErrConflict):
		status = 409
	}
	fail(c, status, err.Error())
}
func (s *Server) policyCall(c *gin.Context, action string, input any, timeout time.Duration) {
	if s.BackupPolicies == nil {
		fail(c, 503, "备份政策辅助程序未配置")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout+2*time.Second)
	defer cancel()
	// Allow the metadata scanner to finish its partial-result response before
	// forcibly reaping the helper at the transport deadline.
	if action == "files" {
		input = struct {
			backups.FileSource
			TimeoutSeconds int `json:"timeout_seconds"`
		}{input.(backups.FileSource), int(timeout.Seconds())}
	}
	result, err := s.BackupPolicies.BackupPolicy(ctx, action, input)
	if err != nil {
		policyFailure(c, err)
		return
	}
	status := http.StatusOK
	if action == "save" && c.Request.Method == http.MethodPost {
		status = http.StatusCreated
	}
	c.Data(status, "application/json; charset=utf-8", result)
}
func (s *Server) saveBackupPolicy(c *gin.Context) {
	var input backups.SavePolicyRequest
	if !policyBody(c, &input) {
		return
	}
	if c.Request.Method == http.MethodPost {
		if input.ID != "" {
			fail(c, 400, "新增政策不能指定标识")
			return
		}
	} else {
		if input.ID != "" && input.ID != c.Param("id") {
			fail(c, 400, "政策标识不一致")
			return
		}
		input.ID = c.Param("id")
	}
	if err := backups.ValidatePolicy(input); err != nil {
		policyFailure(c, err)
		return
	}
	s.policyCall(c, "save", input, 20*time.Second)
}
