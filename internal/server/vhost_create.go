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
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin"
)

func decodeVhostBody(c *gin.Context, out any) error {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("参数无效或请求过大")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("请求存在多余内容")
	}
	return nil
}
func (s *Server) registerVhostCreation(private *gin.RouterGroup) {
	if s.VhostJobs == nil {
		s.VhostJobs = vhosts.NewJobs(context.Background(), control.Helper{Path: s.Config.Helper}, func() {
			if s.Vhosts != nil {
				s.Vhosts.Invalidate()
			}
		})
	}
	if s.DNS == nil {
		s.DNS = &vhosts.DNSChecker{PublicIPs: s.Config.Vhosts.PublicIPs}
	}
	private.GET("/vhosts/options", func(c *gin.Context) {
		ctx, done := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer done()
		out, err := s.VhostJobs.Options(ctx)
		if err != nil {
			fail(c, 503, err.Error())
			return
		}
		c.JSON(200, out)
	})
	private.POST("/vhosts/check-dns", s.csrf, func(c *gin.Context) {
		var input struct {
			Domains []string `json:"domains"`
		}
		if err := decodeVhostBody(c, &input); err != nil {
			fail(c, 400, err.Error())
			return
		}
		out, err := s.DNS.Check(c.Request.Context(), input.Domains)
		if err != nil {
			fail(c, 400, err.Error())
			return
		}
		c.JSON(200, out)
	})
	start := func(retry bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			var input vhosts.CreateRequest
			if err := decodeVhostBody(c, &input); err != nil {
				fail(c, 400, err.Error())
				return
			}
			input.Retry = retry
			out, err := s.VhostJobs.Start(input)
			if err != nil {
				code := 400
				if errors.Is(err, vhosts.ErrJobBusy) {
					code = 409
				}
				fail(c, code, err.Error())
				return
			}
			c.JSON(202, out)
		}
	}
	private.POST("/vhosts", s.csrf, start(false))
	private.POST("/vhosts/ssl/retry", s.csrf, start(true))
	private.POST("/vhosts/ssl/renew/preview", s.csrf, func(c *gin.Context) {
		var input struct {
			Names []string `json:"names"`
		}
		if err := decodeVhostBody(c, &input); err != nil {
			fail(c, 400, err.Error())
			return
		}
		ctx, done := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer done()
		out, err := s.VhostJobs.PreviewRenewal(ctx, input.Names)
		if err != nil {
			fail(c, 400, err.Error())
			return
		}
		c.JSON(200, out)
	})
	private.POST("/vhosts/ssl/renew", s.csrf, func(c *gin.Context) {
		var input vhosts.RenewRequest
		if err := decodeVhostBody(c, &input); err != nil {
			fail(c, 400, err.Error())
			return
		}
		out, err := s.VhostJobs.StartRenewal(input)
		if err != nil {
			code := 400
			if errors.Is(err, vhosts.ErrJobBusy) {
				code = 409
			}
			fail(c, code, err.Error())
			return
		}
		c.JSON(202, out)
	})
	private.GET("/vhosts/jobs", func(c *gin.Context) { c.JSON(200, s.VhostJobs.List()) })
	private.GET("/vhosts/jobs/:id", func(c *gin.Context) {
		out, ok := s.VhostJobs.Get(c.Param("id"))
		if !ok {
			fail(c, 404, "任务不存在；面板重启后请刷新站点确认实际状态")
			return
		}
		c.JSON(200, out)
	})
	private.GET("/vhosts/jobs/:id/events", s.vhostJobStream)
}

func (s *Server) vhostJobStream(c *gin.Context) {
	after, _ := strconv.ParseUint(c.GetHeader("Last-Event-ID"), 10, 64)
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		fail(c, 429, "实时连接数已达上限")
		return
	}
	update, ok := s.VhostJobs.Events(c.Param("id"), 0, after)
	if !ok {
		fail(c, 404, "任务不存在")
		return
	}
	session, _ := s.session(c)
	expiry := time.NewTimer(time.Until(session.Expires))
	defer expiry.Stop()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-store")
	c.Header("X-Accel-Buffering", "no")
	rc := http.NewResponseController(c.Writer)
	defer func() { _ = rc.SetWriteDeadline(time.Time{}) }()
	send := func(event string, id uint64, data any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		value := ""
		if id > 0 {
			value = strconv.FormatUint(id, 10)
		}
		c.Render(-1, sse.Event{Event: event, Id: value, Data: data})
		return rc.Flush() == nil
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	// Keep revocation checks as frequent as before without polling/copying Jobs.
	authCheck := time.NewTicker(300 * time.Millisecond)
	defer authCheck.Stop()
	var nextUpdate time.Time
	for {
		if _, ok := s.session(c); !ok {
			send("auth_expired", 0, gin.H{})
			return
		}
		if update.Changed {
			job := update.Job
			if update.Snapshot {
				if !send("snapshot", job.Lines[len(job.Lines)-1].ID, job) {
					return
				}
				after = job.Lines[len(job.Lines)-1].ID
			} else {
				for _, line := range job.Lines {
					if line.ID > after {
						if !send("log", line.ID, line) {
							return
						}
						after = line.ID
					}
				}
				job.Lines = []vhosts.JobLine{}
				if !send("status", 0, job) {
					return
				}
			}
			if job.Status != "running" {
				send("complete", 0, gin.H{})
				return
			}
			nextUpdate = time.Now().Add(300 * time.Millisecond)
		}
		update.Job = vhosts.Job{}
		update.Changed = false
		select {
		case <-c.Request.Context().Done():
			return
		case <-expiry.C:
			send("auth_expired", 0, gin.H{})
			return
		case <-authCheck.C:
		case <-update.Notify:
			// Coalesce bursts while retaining the log window in Jobs. There is
			// no per-client event queue and idle streams copy no task data.
			timer := time.NewTimer(max(time.Until(nextUpdate), 0))
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
			update, ok = s.VhostJobs.Events(c.Param("id"), update.Version, after)
			if !ok {
				return
			}
		case <-heartbeat.C:
			if !send("heartbeat", 0, gin.H{}) {
				return
			}
		}
	}
}
