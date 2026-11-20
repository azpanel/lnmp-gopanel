package server

import (
	"errors"
	"net/http"

	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/gin-gonic/gin"
)

func (s *Server) phpConfig(c *gin.Context) {
	if s.Services == nil {
		fail(c, 503, "PHP 配置管理未配置")
		return
	}
	result, err := s.Services.PHPConfig(c.Request.Context())
	if err != nil {
		fail(c, 503, err.Error())
		return
	}
	c.JSON(200, result)
}
func (s *Server) updatePHPConfig(c *gin.Context) {
	r, err := control.DecodePHPConfigRequest(http.MaxBytesReader(c.Writer, c.Request.Body, control.PHPConfigRequestLimit))
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	if s.Services == nil {
		fail(c, 503, "PHP 配置管理未配置")
		return
	}
	result, err := s.Services.UpdatePHPConfig(c.Request.Context(), r)
	if err != nil {
		status := 409
		if errors.Is(err, control.ErrPHPConfigInvalid) || errors.Is(err, control.ErrUnsupported) {
			status = 400
		}
		fail(c, status, err.Error())
		return
	}
	c.JSON(200, result)
}
