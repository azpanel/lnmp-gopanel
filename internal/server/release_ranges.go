package server

import (
	"errors"

	"github.com/azpanel/lnmp-gopanel/internal/releases"
	"github.com/gin-gonic/gin"
)

func (s *Server) saveReleaseRange(c *gin.Context) {
	if s.Releases == nil {
		fail(c, 503, "版本查询未配置")
		return
	}
	name := c.Param("name")
	configured := false
	for _, service := range s.Services.List() {
		if service.Name == name {
			configured = true
			break
		}
	}
	if !configured || !releases.RangeService(name) {
		fail(c, 400, releases.ErrRangeInput.Error())
		return
	}
	var req struct {
		Range *string `json:"range"`
	}
	if !authBody(c, &req) {
		return
	}
	if req.Range == nil {
		fail(c, 400, releases.ErrRangeInput.Error())
		return
	}
	result, err := s.Releases.SaveRange(name, *req.Range)
	if err != nil {
		status := 503
		if errors.Is(err, releases.ErrRangeInput) {
			status = 400
		}
		fail(c, status, err.Error())
		return
	}
	c.JSON(202, result)
}
