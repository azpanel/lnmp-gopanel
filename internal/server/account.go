package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net/http"

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
func (s *Server) securityReply(c *gin.Context, err error) authReply {
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
		return authReply{status: status, message: "认证状态不可用，操作未完成，请检查认证状态目录并重启面板"}
	}
	return authReply{status: status, message: err.Error()}
}
func authBody(c *gin.Context, out any) bool {
	if c.ContentType() != "application/json" {
		fail(c, 415, "请使用 JSON 请求")
		return false
	}
	// Read to EOF before decoding so even trailing whitespace counts toward the
	// limit, including requests without Content-Length (chunked HTTP bodies).
	if c.Request.ContentLength > 4096 {
		fail(c, 413, "认证请求超过 4 KiB")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, 413, "认证请求超过 4 KiB")
		} else {
			fail(c, 400, "认证参数无效")
		}
		return false
	}
	data = bytes.Trim(data, " \t\r\n")
	if len(data) == 0 || data[0] != '{' {
		fail(c, 400, "认证参数无效")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
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
	event := logs.LoginEvent{Username: s.Config.Username, Stage: "totp"}
	defer s.auditLoginFailure(c, &event)
	if s.sourceLimited(c, &s.totpLimiter) {
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
	reply := s.loginTransaction(c, &s.totpWork, func() authReply {
		if err := s.account().FinishLogin(req.Challenge, req.Code, req.Recovery); err != nil {
			return s.securityReply(c, err)
		}
		return authReply{status: 200, body: s.loginSession(c)}
	})
	reply.write(c)
	if reply.status == 200 {
		s.loginSuccess(c, "totp")
	}
}
func (s *Server) rotateSession(c *gin.Context) gin.H {
	s.Sessions.DeleteAll()
	id, v := s.Sessions.Create()
	s.cookie(c, id, int(auth.TTL.Seconds()))
	return gin.H{"username": s.Config.Username, "csrf_token": v.CSRF, "expires_at": v.Expires}
}

type securityCredentials struct {
	Current  string `json:"current_password"`
	Code     string `json:"code"`
	Recovery string `json:"recovery_code"`
}

func (s *Server) registerAccountSecurity(private *gin.RouterGroup) {
	private.GET("/auth/security", func(c *gin.Context) { c.JSON(200, s.account().Security()) })
	type passwordInput struct {
		securityCredentials
		Password string `json:"new_password"`
	}
	private.POST("/auth/password", s.csrf, accountHandler(s, func(c *gin.Context, req *passwordInput) authReply {
		if err := s.account().ChangePassword(req.Current, req.Password, req.Code, req.Recovery); err != nil {
			return s.securityReply(c, err)
		}
		s.Sessions.DeleteAll()
		s.cookie(c, "", -1)
		return authReply{status: 204, log: "管理员修改登录密码，已撤销全部会话"}
	}))
	type setupInput struct {
		Password string `json:"current_password"`
	}
	private.POST("/auth/totp/setup", s.csrf, accountHandler(s, func(c *gin.Context, req *setupInput) authReply {
		id, _ := c.Cookie(auth.CookieName)
		setup, err := s.account().StartSetup(id, req.Password)
		if err != nil {
			return s.securityReply(c, err)
		}
		return authReply{render: func(c *gin.Context) { renderSetup(c, setup) }}
	}))
	type confirmInput struct {
		ID   string `json:"setup_id"`
		Code string `json:"code"`
	}
	private.POST("/auth/totp/confirm", s.csrf, accountHandler(s, func(c *gin.Context, req *confirmInput) authReply {
		id, _ := c.Cookie(auth.CookieName)
		codes, err := s.account().ConfirmSetup(id, req.ID, req.Code)
		if err != nil {
			return s.securityReply(c, err)
		}
		return authReply{status: 200, body: gin.H{"recovery_codes": codes, "session": s.rotateSession(c)}, log: "管理员启用 TOTP"}
	}))
	private.DELETE("/auth/totp", s.csrf, accountHandler(s, func(c *gin.Context, req *securityCredentials) authReply {
		if err := s.account().DisableTOTP(req.Current, req.Code, req.Recovery); err != nil {
			return s.securityReply(c, err)
		}
		return authReply{status: 200, body: gin.H{"session": s.rotateSession(c)}, log: "管理员关闭 TOTP"}
	}))
	private.POST("/auth/totp/recovery-codes", s.csrf, accountHandler(s, func(c *gin.Context, req *securityCredentials) authReply {
		codes, err := s.account().RegenerateCodes(req.Current, req.Code, req.Recovery)
		if err != nil {
			return s.securityReply(c, err)
		}
		return authReply{status: 200, body: gin.H{"recovery_codes": codes, "session": s.rotateSession(c)}, log: "管理员重新生成 TOTP 恢复码"}
	}))
	private.DELETE("/auth/totp/setup", s.csrf, func(c *gin.Context) {
		if s.limited(c, &s.securityLimiter) {
			return
		}
		s.authTransaction(c, true, func() authReply {
			id, _ := c.Cookie(auth.CookieName)
			s.account().DiscardSetup(id)
			return authReply{status: 204}
		}).write(c)
	})
}

func renderSetup(c *gin.Context, setup auth.Setup) {
	key, err := otp.NewKeyFromURL(setup.URL)
	if err == nil {
		img, imageErr := key.Image(240, 240)
		err = imageErr
		if err == nil {
			var buf bytes.Buffer
			err = png.Encode(&buf, img)
			if err == nil {
				c.JSON(200, gin.H{"setup_id": setup.ID, "secret": setup.Secret, "qr_code": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), "expires_at": setup.Expires})
				return
			}
		}
	}
	fail(c, 503, "认证状态不可用，操作未完成，请重试")
}
