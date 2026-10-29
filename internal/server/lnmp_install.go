package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin"
)

func installFailure(c *gin.Context, err error) {
	code := 400
	if errors.Is(err, lnmpinstall.ErrConflict) {
		code = 409
	}
	fail(c, code, err.Error())
}
func (s *Server) installationSnapshot(ctx context.Context) (lnmpinstall.Snapshot, error) {
	v, err := s.Installation.InstallInspect(ctx)
	if err == nil && v.Job != nil && v.Job.Status == "succeeded" && s.Services != nil {
		s.installationMu.Lock()
		defer s.installationMu.Unlock()
		if s.installationApplied == v.Job.ID {
			return v, nil
		}
		path := s.ServicesFile
		if path == "" {
			path = "/etc/lnmp-gopanel/services.yaml"
		}
		if services, e := config.LoadServices(path); e == nil {
			s.Services.AddServices(services)
			s.installationApplied = v.Job.ID
		}
		if s.Components != nil {
			s.Components.Latest(true)
		}
		if s.Vhosts != nil {
			s.Vhosts.Invalidate()
		}
	}
	return v, err
}
func (s *Server) registerLNMPInstallation(private *gin.RouterGroup) {
	if s.Installation == nil {
		s.Installation = control.Helper{Path: s.Config.Helper}
	}
	private.GET("/lnmp-installation", func(c *gin.Context) {
		ctx, done := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer done()
		v, err := s.installationSnapshot(ctx)
		if err != nil {
			fail(c, 503, err.Error())
			return
		}
		c.JSON(200, v)
	})
	private.POST("/lnmp-installation/prepare", s.csrf, func(c *gin.Context) {
		var r struct {
			ID string `json:"id"`
		}
		if err := decodeVhostBody(c, &r); err != nil {
			installFailure(c, err)
			return
		}
		if !lnmpinstall.ValidID(r.ID) {
			fail(c, 400, "安装任务标识无效")
			return
		}
		ctx, done := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer done()
		v, err := s.Installation.InstallPrepare(ctx, r.ID)
		if err != nil {
			installFailure(c, err)
			return
		}
		c.JSON(202, v)
	})
	private.POST("/lnmp-installation/jobs/:id/start", s.csrf, func(c *gin.Context) {
		var r lnmpinstall.Request
		if err := decodeVhostBody(c, &r); err != nil {
			installFailure(c, err)
			return
		}
		if r.ID != c.Param("id") || !lnmpinstall.ValidID(r.ID) {
			fail(c, 400, "安装任务标识无效")
			return
		}
		ctx, done := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer done()
		v, err := s.Installation.InstallStart(ctx, r)
		if err != nil {
			installFailure(c, err)
			return
		}
		c.JSON(202, v)
	})
	private.GET("/lnmp-installation/jobs/:id", func(c *gin.Context) {
		ctx, done := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer done()
		v, err := s.installationSnapshot(ctx)
		if err != nil {
			fail(c, 503, err.Error())
			return
		}
		if v.Job == nil || v.Job.ID != c.Param("id") {
			fail(c, 404, "当前安装任务不存在")
			return
		}
		c.JSON(200, v.Job)
	})
	private.GET("/lnmp-installation/jobs/:id/events", s.installationEvents)
	private.GET("/lnmp-installation/jobs/:id/logs", s.installationLogs)
}
func (s *Server) installationLogs(c *gin.Context) {
	if !lnmpinstall.ValidID(c.Param("id")) {
		fail(c, 400, "安装任务标识无效")
		return
	}
	after := int64(0)
	if raw := c.Query("after"); raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			fail(c, 400, "日志游标无效")
			return
		}
	}
	tail := c.Query("tail") == "1"
	if (c.Query("tail") != "" && !tail) || (tail && (after != 0 || c.Query("download") == "1")) {
		fail(c, 400, "日志读取选项无效")
		return
	}
	if c.Query("download") != "1" {
		ctx, done := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer done()
		v, err := s.Installation.InstallRead(ctx, lnmpinstall.ReadRequest{ID: c.Param("id"), After: after, Tail: tail})
		if err != nil {
			installFailure(c, err)
			return
		}
		c.JSON(200, v)
		return
	}
	select {
	case s.logReads <- struct{}{}:
		defer func() { <-s.logReads }()
	default:
		fail(c, 429, "日志下载连接数已达上限")
		return
	}
	session, _ := s.session(c)
	rc := http.NewResponseController(c.Writer)
	defer rc.SetWriteDeadline(time.Time{})
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="lnmp-install-`+c.Param("id")+`.log"`)
	for {
		if time.Now().After(session.Expires) {
			return
		}
		if _, ok := s.session(c); !ok {
			return
		}
		ctx, done := context.WithTimeout(c.Request.Context(), 20*time.Second)
		page, err := s.Installation.InstallRead(ctx, lnmpinstall.ReadRequest{ID: c.Param("id"), After: after})
		done()
		if err != nil {
			if after == 0 {
				fail(c, 400, err.Error())
			}
			return
		}
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		for _, line := range page.Lines {
			if _, err = fmt.Fprintf(c.Writer, "%s [%s] %s\n", line.Time.Format(time.RFC3339), line.Phase, line.Text); err != nil {
				return
			}
		}
		if rc.Flush() != nil {
			return
		}
		if !page.More || page.Cursor <= after {
			return
		}
		after = page.Cursor
	}
}
func (s *Server) installationEvents(c *gin.Context) {
	id := c.Param("id")
	if !lnmpinstall.ValidID(id) {
		fail(c, 400, "安装任务标识无效")
		return
	}
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		fail(c, 429, "实时连接数已达上限")
		return
	}
	after := int64(0)
	raw := c.GetHeader("Last-Event-ID")
	if raw == "" {
		raw = c.Query("after")
	}
	if raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			fail(c, 400, "日志游标无效")
			return
		}
	}
	session, _ := s.session(c)
	expiry := time.NewTimer(time.Until(session.Expires))
	defer expiry.Stop()
	rc := http.NewResponseController(c.Writer)
	defer rc.SetWriteDeadline(time.Time{})
	c.Header("Content-Type", "text/event-stream")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Cache-Control", "no-cache, no-store")
	send := func(event string, id int64, v any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		seq := ""
		if id > 0 {
			seq = strconv.FormatInt(id, 10)
		}
		c.Render(-1, sse.Event{Event: event, Id: seq, Data: v})
		return rc.Flush() == nil
	}
	for {
		if _, ok := s.session(c); !ok {
			send("auth_expired", 0, gin.H{})
			return
		}
		ctx, done := context.WithTimeout(c.Request.Context(), 20*time.Second)
		snapshot, err := s.installationSnapshot(ctx)
		if err != nil {
			done()
			send("failure", 0, gin.H{"error": err.Error()})
			return
		}
		if snapshot.Job == nil || snapshot.Job.ID != id {
			done()
			send("failure", 0, gin.H{"error": "当前任务已变化"})
			return
		}
		page, err := s.Installation.InstallRead(ctx, lnmpinstall.ReadRequest{ID: id, After: after})
		done()
		if err != nil {
			send("failure", 0, gin.H{"error": err.Error()})
			return
		}
		if !send("status", 0, snapshot.Job) {
			return
		}
		for _, line := range page.Lines {
			if !send("log", line.ID, line) {
				return
			}
		}
		after = page.Cursor
		if !page.More && !lnmpinstall.Active(snapshot.Job.Status) {
			send("complete", 0, gin.H{})
			return
		}
		if page.More {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-c.Request.Context().Done():
			timer.Stop()
			return
		case <-expiry.C:
			timer.Stop()
			send("auth_expired", 0, gin.H{})
			return
		case <-timer.C:
		}
	}
}
