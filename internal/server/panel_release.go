package server

import (
	"context"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/buildinfo"
	"github.com/azpanel/lnmp-gopanel/internal/releases"
	"github.com/gin-gonic/gin"
)

func (s *Server) panelRelease(c *gin.Context) {
	source := s.PanelReleases
	if source == nil {
		source = releases.NewFetcher()
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	release, err := source.FetchPanel(ctx, buildinfo.Version)
	if err != nil {
		fail(c, 502, "面板更新检查失败："+err.Error())
		return
	}
	c.JSON(200, release)
}
