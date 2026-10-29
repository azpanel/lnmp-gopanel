package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"log/slog"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
)

func (s *Server) account() *auth.Account {
	s.accountOnce.Do(func() {
		if s.Account == nil {
			s.Account = auth.NewAccount(s.Config.Username, s.Config.PasswordHash)
		}
	})
	return s.Account
}
func (s *Server) securityError(c *gin.Context, err error) {
	status := 503
	switch {
	case errors.Is(err, auth.ErrProof):
		status = 403
	case errors.Is(err, auth.ErrInput):
		status = 400
	case errors.Is(err, auth.ErrConflict):
		status = 409
	case errors.Is(err, auth.ErrChallenge):
		status = 401
	}
	if status == 503 {
		if errors.Is(err, auth.ErrStorage) {
			s.Sessions.DeleteAll()
			s.cookie(c, "", -1)
		}
		fail(c, status, "认证状态不可用，操作未完成，请检查认证状态目录并重启面板")
		return
	}
	fail(c, status, err.Error())
}
func authBody(c *gin.Context, out any) bool {
	if c.ContentType() != "application/json" {
		fail(c, 415, "请使用 JSON 请求")
		return false
	}
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		fail(c, 400, "认证参数无效")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(c, 400, "认证参数无效")
		return false
	}
	return true
}
func (s *Server) limited(c *gin.Context, l *auth.Limiter) bool {
	if !l.Allow() {
		c.Header("Retry-After", "60")
		fail(c, 429, "尝试次数过多，请稍后再试")
		return true
	}
	return false
}
func (s *Server) loginTOTP(c *gin.Context) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	event := logs.LoginEvent{Username: s.Config.Username, Stage: "totp"}
	defer s.auditLoginFailure(c, &event)
	if s.limited(c, &s.limiter) {
		return
	}
	var req struct {
		Challenge string `json:"challenge_token"`
		Code      string `json:"code"`
		Recovery  string `json:"recovery_code"`
	}
	if !authBody(c, &req) {
		return
	}
	if err := s.account().FinishLogin(req.Challenge, req.Code, req.Recovery); err != nil {
		s.securityError(c, err)
		return
	}
	s.loginSession(c)
}
func (s *Server) rotateSession(c *gin.Context) gin.H {
	s.Sessions.DeleteAll()
	id, v := s.Sessions.Create()
	s.cookie(c, id, int(auth.TTL.Seconds()))
	return gin.H{"username": s.Config.Username, "csrf_token": v.CSRF, "expires_at": v.Expires}
}
func (s *Server) registerAccountSecurity(private *gin.RouterGroup) {
	private.GET("/auth/security", func(c *gin.Context) { c.JSON(200, s.account().Security()) })
	// Recheck the session and CSRF under the same lock as credential mutation and
	// login. A request queued before revocation must not modify newer credentials.
	guarded := func(handle gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			s.authMu.Lock()
			defer s.authMu.Unlock()
			v, ok := s.session(c)
			if !ok {
				fail(c, 401, "请先登录")
				return
			}
			if !auth.Equal(c.GetHeader("X-CSRF-Token"), v.CSRF) {
				fail(c, 403, "请求校验失败，请刷新页面")
				return
			}
			if s.limited(c, &s.securityLimiter) {
				return
			}
			handle(c)
		}
	}
	type credentials struct {
		Current  string `json:"current_password"`
		Code     string `json:"code"`
		Recovery string `json:"recovery_code"`
	}
	private.POST("/auth/password", s.csrf, guarded(func(c *gin.Context) {
		var req struct {
			credentials
			Password string `json:"new_password"`
		}
		if !authBody(c, &req) {
			return
		}
		if err := s.account().ChangePassword(req.Current, req.Password, req.Code, req.Recovery); err != nil {
			s.securityError(c, err)
			return
		}
		s.Sessions.DeleteAll()
		s.cookie(c, "", -1)
		slog.Info("管理员修改登录密码，已撤销全部会话")
		c.Status(204)
	}))
	private.POST("/auth/totp/setup", s.csrf, guarded(func(c *gin.Context) {
		var req struct {
			Password string `json:"current_password"`
		}
		if !authBody(c, &req) {
			return
		}
		id, _ := c.Cookie(auth.CookieName)
		setup, err := s.account().StartSetup(id, req.Password)
		if err != nil {
			s.securityError(c, err)
			return
		}
		key, err := otp.NewKeyFromURL(setup.URL)
		if err != nil {
			s.securityError(c, err)
			return
		}
		img, err := key.Image(240, 240)
		if err != nil {
			s.securityError(c, err)
			return
		}
		var buf bytes.Buffer
		if err = png.Encode(&buf, img); err != nil {
			s.securityError(c, err)
			return
		}
		c.JSON(200, gin.H{"setup_id": setup.ID, "secret": setup.Secret, "qr_code": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), "expires_at": setup.Expires})
	}))
	private.POST("/auth/totp/confirm", s.csrf, guarded(func(c *gin.Context) {
		var req struct {
			ID   string `json:"setup_id"`
			Code string `json:"code"`
		}
		if !authBody(c, &req) {
			return
		}
		id, _ := c.Cookie(auth.CookieName)
		codes, err := s.account().ConfirmSetup(id, req.ID, req.Code)
		if err != nil {
			s.securityError(c, err)
			return
		}
		session := s.rotateSession(c)
		slog.Info("管理员启用 TOTP")
		c.JSON(200, gin.H{"recovery_codes": codes, "session": session})
	}))
	private.DELETE("/auth/totp", s.csrf, guarded(func(c *gin.Context) {
		var req credentials
		if !authBody(c, &req) {
			return
		}
		if err := s.account().DisableTOTP(req.Current, req.Code, req.Recovery); err != nil {
			s.securityError(c, err)
			return
		}
		session := s.rotateSession(c)
		slog.Info("管理员关闭 TOTP")
		c.JSON(200, gin.H{"session": session})
	}))
	private.POST("/auth/totp/recovery-codes", s.csrf, guarded(func(c *gin.Context) {
		var req credentials
		if !authBody(c, &req) {
			return
		}
		codes, err := s.account().RegenerateCodes(req.Current, req.Code, req.Recovery)
		if err != nil {
			s.securityError(c, err)
			return
		}
		session := s.rotateSession(c)
		slog.Info("管理员重新生成 TOTP 恢复码")
		c.JSON(200, gin.H{"recovery_codes": codes, "session": session})
	}))
	private.DELETE("/auth/totp/setup", s.csrf, guarded(func(c *gin.Context) { id, _ := c.Cookie(auth.CookieName); s.account().DiscardSetup(id); c.Status(204) }))
}
