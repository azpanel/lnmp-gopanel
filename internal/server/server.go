package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/buildinfo"
	"github.com/azpanel/lnmp-gopanel/internal/components"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/cron"
	"github.com/azpanel/lnmp-gopanel/internal/docker"
	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/azpanel/lnmp-gopanel/internal/metrics"
	"github.com/azpanel/lnmp-gopanel/internal/releases"
	"github.com/azpanel/lnmp-gopanel/internal/screenshots"
	"github.com/azpanel/lnmp-gopanel/internal/storage"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"github.com/gin-gonic/gin"
)

type Server struct {
	Installation        lnmpinstall.Backend
	ServicesFile        string
	installationMu      sync.Mutex
	installationApplied string
	Config              config.Config
	Sessions            *auth.Store
	Metrics             *metrics.Collector
	Services            *control.Manager
	Components          *components.Manager
	Docker              *docker.Manager
	LoginLog            *logs.LoginLog
	Logs                logs.Reader
	CustomLogs          *logs.Registry
	CustomLogsError     string
	Cron                cron.Reader
	Releases            *releases.Manager
	Storage             *storage.Manager
	Vhosts              *vhosts.Manager
	VhostJobs           *vhosts.Jobs
	DNS                 *vhosts.DNSChecker
	Screenshots         *screenshots.Manager
	Host                metrics.Host
	Assets              fs.FS
	limiter             auth.Limiter
	streams             chan struct{}
	logReads            chan struct{}
	Account             *auth.Account
	accountOnce         sync.Once
	authMu              sync.Mutex
	securityLimiter     auth.Limiter
}

func New(c config.Config, m *metrics.Collector, svc *control.Manager, assets fs.FS) *Server {
	return &Server{Components: components.NewManager(context.Background(), control.Helper{Path: c.Helper}), Config: c, Sessions: auth.New(), Metrics: m, Services: svc, Logs: control.Helper{Path: c.Helper}, logReads: make(chan struct{}, 4), Cron: control.Helper{Path: c.Helper}, Docker: docker.NewManager(context.Background(), control.Helper{Path: c.Helper}), Releases: releases.NewManager(context.Background(), releases.NewFetcher()), Host: metrics.HostInfo(context.Background()), Storage: storage.NewManager(context.Background(), control.Helper{Path: c.Helper}), Vhosts: vhosts.NewManager(context.Background(), control.Helper{Path: c.Helper}), Assets: assets, streams: make(chan struct{}, 32)}
}
func fail(c *gin.Context, status int, msg string) { c.AbortWithStatusJSON(status, gin.H{"error": msg}) }
func (s *Server) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	_ = r.SetTrustedProxies(nil)
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if s.Config.SecureCookie {
			c.Header("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-store")
			limit := int64(4096)
			if c.Request.Method == http.MethodPost && (c.Request.URL.Path == "/api/v1/vhosts" || c.Request.URL.Path == "/api/v1/vhosts/ssl/retry" || c.Request.URL.Path == "/api/v1/vhosts/check-dns") {
				limit = 32 * 1024
			}
			if c.Request.Method == http.MethodPost && (c.Request.URL.Path == "/api/v1/vhosts/ssl/renew" || c.Request.URL.Path == "/api/v1/vhosts/ssl/renew/preview") {
				limit = 1024 * 1024
			}
			if c.Request.Method == http.MethodPut && c.Request.URL.Path == "/api/v1/vhosts/screenshot" {
				limit = 128 * 1024
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	})
	api := r.Group("/api/v1")
	api.POST("/auth/login", s.origin, s.login)
	api.POST("/auth/login/totp", s.origin, s.loginTOTP)
	private := api.Group("")
	private.Use(s.requireAuth)
	private.GET("/build", func(c *gin.Context) { c.JSON(200, buildinfo.Current()) })
	private.GET("/auth/session", func(c *gin.Context) {
		v, _ := s.session(c)
		c.JSON(200, gin.H{"username": s.Config.Username, "csrf_token": v.CSRF, "expires_at": v.Expires})
	})
	private.POST("/auth/logout", s.csrf, func(c *gin.Context) {
		s.authMu.Lock()
		defer s.authMu.Unlock()
		id, _ := c.Cookie(auth.CookieName)
		s.account().DiscardSetup(id)
		s.Sessions.Delete(id)
		s.cookie(c, "", -1)
		c.Status(204)
	})
	s.registerAccountSecurity(private)
	s.registerLNMPInstallation(private)
	private.GET("/host", func(c *gin.Context) { c.JSON(200, s.Host) })
	private.GET("/metrics", func(c *gin.Context) {
		v := s.Metrics.Latest()
		if v.Timestamp.IsZero() {
			fail(c, 503, "等待首次采集")
			return
		}
		c.JSON(200, v)
	})
	private.GET("/metrics/stream", s.stream)
	private.GET("/components", func(c *gin.Context) { s.componentSnapshot(c, false) })
	private.POST("/components/refresh", s.csrf, func(c *gin.Context) { s.componentSnapshot(c, true) })
	private.GET("/services", func(c *gin.Context) { c.JSON(200, s.Services.List()) })
	private.GET("/services/releases", func(c *gin.Context) { s.serviceReleases(c, false) })
	private.POST("/services/releases/refresh", s.csrf, func(c *gin.Context) { s.serviceReleases(c, true) })
	private.POST("/services/:name/actions/:action", s.csrf, s.action)
	private.GET("/services/:name/management-repair", s.managementRepairPreview)
	private.POST("/services/:name/management-repair", s.csrf, s.managementRepair)
	private.GET("/docker", func(c *gin.Context) { s.dockerSnapshot(c, false) })
	private.GET("/cron", func(c *gin.Context) {
		if s.Cron == nil {
			fail(c, 503, "定时任务读取未配置")
			return
		}
		result, err := s.Cron.Cron(c.Request.Context())
		if err != nil {
			fail(c, 503, err.Error())
			return
		}
		c.JSON(200, result)
	})
	private.GET("/cron/preview", s.cronPreview)
	private.GET("/logs", s.logList)
	private.POST("/logs/read", s.csrf, s.logRead)
	private.POST("/logs/custom", s.csrf, s.logAdd)
	private.DELETE("/logs/custom", s.csrf, s.logRemove)
	private.POST("/docker/refresh", s.csrf, func(c *gin.Context) { s.dockerSnapshot(c, true) })
	private.GET("/storage/mounts", func(c *gin.Context) {
		result, err := storage.Mounts(c.Request.Context())
		if err != nil {
			fail(c, 503, err.Error())
			return
		}
		c.JSON(200, result)
	})
	private.GET("/storage/scan", func(c *gin.Context) {
		if s.Storage == nil {
			fail(c, 503, "文件扫描未配置")
			return
		}
		c.JSON(200, s.Storage.Latest())
	})
	private.POST("/storage/scan", s.csrf, func(c *gin.Context) {
		if s.Storage == nil {
			fail(c, 503, "文件扫描未配置")
			return
		}
		c.JSON(202, s.Storage.Start())
	})
	private.GET("/vhosts", func(c *gin.Context) {
		if s.Vhosts == nil {
			fail(c, 503, "站点管理未配置")
			return
		}
		c.JSON(200, s.Vhosts.Latest())
	})
	s.registerVhostCreation(private)
	s.registerVhostBasicAuth(private)
	s.registerVhostFormat(private)
	private.GET("/vhosts/count", func(c *gin.Context) {
		if s.Vhosts == nil {
			fail(c, 503, "站点管理未配置")
			return
		}
		count, err := s.Vhosts.Count(c.Request.Context())
		if err != nil {
			fail(c, 503, err.Error())
			return
		}
		c.JSON(200, gin.H{"count": count})
	})
	private.POST("/vhosts/refresh", s.csrf, func(c *gin.Context) {
		if s.Vhosts == nil {
			fail(c, 503, "站点管理未配置")
			return
		}
		c.JSON(202, s.Vhosts.Refresh())
	})
	private.GET("/vhosts/config", func(c *gin.Context) {
		if s.Vhosts == nil {
			fail(c, 503, "站点管理未配置")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		view, e := s.Vhosts.Read(ctx, c.Query("name"))
		if e != nil {
			fail(c, 400, e.Error())
			return
		}
		c.JSON(200, view)
	})
	private.POST("/vhosts/toggle", s.csrf, func(c *gin.Context) {
		if s.Vhosts == nil {
			fail(c, 503, "站点管理未配置")
			return
		}
		var input struct {
			Name    string `json:"name"`
			Enabled *bool  `json:"enabled"`
		}
		if e := c.ShouldBindJSON(&input); e != nil || input.Enabled == nil || !vhosts.ValidName(input.Name) {
			fail(c, 400, "站点参数无效")
			return
		}
		result, e := s.Vhosts.Toggle(input.Name, *input.Enabled)
		if e != nil {
			fail(c, 502, e.Error())
			return
		}
		slog.Info("vhost operation", "name", input.Name, "enabled", *input.Enabled)
		c.JSON(200, result)
	})
	private.GET("/vhosts/screenshots", s.screenshotList)
	private.PUT("/vhosts/screenshot", s.csrf, s.screenshotSave)
	private.POST("/vhosts/screenshot/refresh", s.csrf, s.screenshotRefresh)
	private.DELETE("/vhosts/screenshot", s.csrf, s.screenshotDelete)
	private.GET("/vhosts/screenshot/image", s.screenshotImage)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			fail(c, 404, "接口不存在")
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			c.Status(405)
			return
		}
		name := strings.TrimPrefix(c.Request.URL.Path, "/")
		if !fs.ValidPath(name) || name == "." {
			name = "index.html"
		}
		data, err := fs.ReadFile(s.Assets, name)
		if err != nil {
			if strings.HasPrefix(name, "assets/") || strings.Contains(name, ".") {
				c.Status(404)
				return
			}
			data, err = fs.ReadFile(s.Assets, "index.html")
			name = "index.html"
		}
		if err != nil {
			c.String(503, "前端尚未构建，请运行 make build")
			return
		}
		if name == "index.html" {
			c.Header("Cache-Control", "no-store")
		} else if strings.HasPrefix(name, "assets/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Header("Cache-Control", "no-cache")
		}
		http.ServeContent(c.Writer, c.Request, name, time.Time{}, strings.NewReader(string(data)))
	})
	return r
}
func (s *Server) dockerSnapshot(c *gin.Context, force bool) {
	if s.Docker == nil {
		fail(c, 503, "Docker 查询未配置")
		return
	}
	status := http.StatusOK
	if force {
		status = http.StatusAccepted
	}
	c.JSON(status, s.Docker.Latest(force))
}
func (s *Server) componentSnapshot(c *gin.Context, force bool) {
	if s.Components == nil {
		fail(c, 503, "组件查询未配置")
		return
	}
	status := http.StatusOK
	if force {
		status = http.StatusAccepted
	}
	c.JSON(status, s.Components.Latest(force))
}
func (s *Server) serviceReleases(c *gin.Context, force bool) {
	if s.Releases == nil {
		fail(c, 503, "版本查询未配置")
		return
	}
	names := components.Names()
	for _, service := range s.Services.List() {
		names = append(names, service.Name)
	}
	status := http.StatusOK
	if force {
		status = http.StatusAccepted
	}
	c.JSON(status, s.Releases.Latest(names, force))
}
func (s *Server) session(c *gin.Context) (auth.Session, bool) {
	id, err := c.Cookie(auth.CookieName)
	if err != nil {
		return auth.Session{}, false
	}
	return s.Sessions.Get(id)
}
func (s *Server) requireAuth(c *gin.Context) {
	if _, ok := s.session(c); !ok {
		fail(c, 401, "请先登录")
		return
	}
	c.Next()
}
func (s *Server) origin(c *gin.Context) {
	origin := c.GetHeader("Origin")
	if origin != "" {
		expected := s.Config.PublicOrigin
		if expected == "" {
			scheme := "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
			expected = scheme + "://" + c.Request.Host
		}
		u, err := url.Parse(origin)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSuffix(origin, "/") != expected {
			fail(c, 403, "请求来源不匹配")
			return
		}
	}
	c.Next()
}
func (s *Server) csrf(c *gin.Context) {
	v, ok := s.session(c)
	if !ok || !auth.Equal(c.GetHeader("X-CSRF-Token"), v.CSRF) {
		fail(c, 403, "请求校验失败，请刷新页面")
		return
	}
	s.origin(c)
}
func (s *Server) cookie(c *gin.Context, id string, age int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: auth.CookieName, Value: id, Path: "/", MaxAge: age, HttpOnly: true, Secure: s.Config.SecureCookie, SameSite: http.SameSiteStrictMode})
}
func (s *Server) login(c *gin.Context) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	event := logs.LoginEvent{Stage: "password"}
	defer s.auditLoginFailure(c, &event)
	if !s.limiter.Allow() {
		c.Header("Retry-After", "60")
		fail(c, 429, "尝试次数过多，请稍后再试")
		return
	}
	if c.ContentType() != "application/json" {
		fail(c, 415, "请使用 JSON 请求")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	err := c.ShouldBindJSON(&req)
	event.Username = req.Username
	if err != nil || len(req.Password) > 72 || len(req.Username) > 128 {
		fail(c, 400, "登录参数无效")
		return
	}
	// Verify the password even for an incorrect username to avoid a timing oracle.
	challenge, err := s.account().StartLogin(req.Password)
	if errors.Is(err, auth.ErrProof) {
		event.Password = &req.Password
	}
	if !auth.Equal(req.Username, s.Config.Username) || errors.Is(err, auth.ErrProof) {
		fail(c, 401, "用户名或密码错误")
		return
	}
	if err != nil {
		s.securityError(c, err)
		return
	}
	if challenge != "" {
		c.JSON(200, gin.H{"totp_required": true, "challenge_token": challenge, "expires_in": int(auth.PendingTTL.Seconds())})
		return
	}
	s.loginSession(c)
}
func (s *Server) loginSession(c *gin.Context) {
	if old, e := c.Cookie(auth.CookieName); e == nil {
		s.Sessions.Delete(old)
	}
	id, v := s.Sessions.Create()
	s.cookie(c, id, int(auth.TTL.Seconds()))
	c.JSON(200, gin.H{"username": s.Config.Username, "csrf_token": v.CSRF, "expires_at": v.Expires})
	stage := "password"
	if c.Request.URL.Path == "/api/v1/auth/login/totp" {
		stage = "totp"
	}
	s.writeLoginEvent(c, logs.LoginEvent{Username: s.Config.Username, Stage: stage, Result: "success", Reason: "authenticated"})
	slog.Info("administrator logged in")
}
func (s *Server) action(c *gin.Context) {
	r, err := s.Services.Action(c.Request.Context(), c.Param("name"), c.Param("action"))
	if err != nil {
		status := 502
		if errors.Is(err, control.ErrBusy) || errors.Is(err, control.ErrManagementConflict) {
			status = 409
		} else if errors.Is(err, control.ErrUnsupported) {
			status = 400
		}
		fail(c, status, err.Error())
		return
	}
	c.JSON(200, r)
}
func (s *Server) stream(c *gin.Context) {
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		fail(c, 429, "实时连接数已达上限")
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
	send := func(event string, data any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		c.SSEvent(event, data)
		return rc.Flush() == nil
	}
	if !send("connected", gin.H{"interval_ms": 2000}) {
		return
	}
	ch, unsubscribe := s.Metrics.Subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-expiry.C:
			send("auth_expired", gin.H{})
			return
		case sample := <-ch:
			if _, ok := s.session(c); !ok {
				send("auth_expired", gin.H{})
				return
			}
			data, err := json.Marshal(sample)
			if err != nil {
				return
			}
			if !send("metrics", string(data)) {
				return
			}
		case <-heartbeat.C:
			if _, ok := s.session(c); !ok {
				send("auth_expired", gin.H{})
				return
			}
			if !send("heartbeat", gin.H{"timestamp": time.Now()}) {
				return
			}
		}
	}
}
