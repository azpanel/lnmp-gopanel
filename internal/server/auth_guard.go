package server

import (
	"context"
	"log/slog"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/gin-gonic/gin"
)

// authGate is a zero-value-safe, cancellable lock. Separate nonblocking gates
// bound anonymous work before it can wait on the credential transaction gate.
type authGate struct {
	once sync.Once
	slot chan struct{}
}

func (g *authGate) init() { g.once.Do(func() { g.slot = make(chan struct{}, 1) }) }
func (g *authGate) lock(ctx context.Context) bool {
	g.init()
	if ctx.Err() != nil {
		return false
	}
	select {
	case g.slot <- struct{}{}:
		if ctx.Err() != nil {
			g.unlock()
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}
func (g *authGate) tryLock() bool {
	g.init()
	select {
	case g.slot <- struct{}{}:
		return true
	default:
		return false
	}
}
func (g *authGate) unlock() { <-g.slot }

// Replies are rendered only after all transaction and execution gates release.
// Cookie headers may be prepared in a transaction; no network I/O occurs there.
type authReply struct {
	status  int
	body    any
	message string
	log     string
	render  func(*gin.Context)
}

func (r authReply) write(c *gin.Context) {
	if r.render != nil {
		r.render(c)
	} else if r.status >= 400 {
		fail(c, r.status, r.message)
	} else if r.body != nil {
		c.JSON(r.status, r.body)
	} else if r.status != 0 {
		c.Status(r.status)
	}
	if r.log != "" {
		slog.Info(r.log)
	}
}

func (s *Server) authTransaction(c *gin.Context, private bool, action func() authReply) authReply {
	if !s.authMu.lock(c.Request.Context()) {
		return authReply{}
	}
	defer s.authMu.unlock()
	if private {
		v, ok := s.session(c)
		if !ok {
			return authReply{status: 401, message: "请先登录"}
		}
		if !auth.Equal(c.GetHeader("X-CSRF-Token"), v.CSRF) {
			return authReply{status: 403, message: "请求校验失败，请刷新页面"}
		}
	}
	return action()
}

func (s *Server) loginTransaction(c *gin.Context, gate *authGate, action func() authReply) authReply {
	if c.Request.Context().Err() != nil {
		return authReply{}
	}
	if !gate.tryLock() {
		c.Header("Retry-After", "1")
		return authReply{status: 429, message: "认证繁忙，请稍后再试"}
	}
	defer gate.unlock()
	return s.authTransaction(c, false, action)
}

func (s *Server) sourceLimited(c *gin.Context, limiter *auth.SourceLimiter) bool {
	// RemoteIP reads the direct peer only, independently of proxy headers.
	source := "unknown"
	if ip, err := netip.ParseAddr(c.RemoteIP()); err == nil {
		source = ip.Unmap().WithZone("").String()
	}
	allowed, retry := limiter.Allow(source)
	if allowed {
		return false
	}
	seconds := max(1, int((retry+time.Second-1)/time.Second))
	c.Header("Retry-After", strconv.Itoa(seconds))
	fail(c, 429, "尝试次数过多，请稍后再试")
	return true
}

// Decode before locking, then recheck the session/CSRF inside the transaction.
func accountHandler[T any](s *Server, action func(*gin.Context, *T) authReply) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.limited(c, &s.securityLimiter) {
			return
		}
		var input T
		if !authBody(c, &input) {
			return
		}
		s.authTransaction(c, true, func() authReply { return action(c, &input) }).write(c)
	}
}
