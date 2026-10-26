package server

import (
	"errors"
	"net/http"

	"github.com/azpanel/lnmp-gopanel/internal/screenshots"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"github.com/gin-gonic/gin"
)

func (s *Server) screenshotList(c *gin.Context) {
	if s.Screenshots == nil {
		c.JSON(200, screenshots.Snapshot{Items: map[string]screenshots.View{}, BrowserError: "截图状态存储不可用，请检查截图状态目录权限"})
		return
	}
	result := s.Screenshots.Latest()
	known := map[string]bool{}
	if s.Vhosts != nil {
		for _, site := range s.Vhosts.Latest().Sites {
			known[site.Name] = true
		}
	}
	for name := range result.Items {
		if !known[name] {
			delete(result.Items, name)
		}
	}
	c.JSON(200, result)
}
func (s *Server) screenshotSite(c *gin.Context) (string, bool) {
	if s.Screenshots == nil {
		fail(c, 503, "截图状态存储不可用，请检查截图状态目录权限")
		return "", false
	}
	name := c.Query("name")
	if !vhosts.ValidName(name) {
		fail(c, 400, "站点参数无效")
		return "", false
	}
	if s.Vhosts != nil {
		for _, site := range s.Vhosts.Latest().Sites {
			if site.Name == name {
				return name, true
			}
		}
	}
	fail(c, 404, "站点不存在，请先刷新虚拟主机列表")
	return "", false
}
func screenshotFailure(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, screenshots.ErrBusy) {
		status = http.StatusTooManyRequests
	}
	fail(c, status, err.Error())
}
func (s *Server) screenshotSave(c *gin.Context) {
	name, ok := s.screenshotSite(c)
	if !ok {
		return
	}
	var input screenshots.Input
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, 400, "截图设置无效或过长")
		return
	}
	result, err := s.Screenshots.Save(name, input)
	if err != nil {
		screenshotFailure(c, err)
		return
	}
	c.JSON(202, result)
}
func (s *Server) screenshotRefresh(c *gin.Context) {
	name, ok := s.screenshotSite(c)
	if !ok {
		return
	}
	result, err := s.Screenshots.Refresh(name)
	if err != nil {
		screenshotFailure(c, err)
		return
	}
	c.JSON(202, result)
}
func (s *Server) screenshotDelete(c *gin.Context) {
	name, ok := s.screenshotSite(c)
	if !ok {
		return
	}
	if err := s.Screenshots.Delete(name); err != nil {
		screenshotFailure(c, err)
		return
	}
	c.Status(204)
}
func (s *Server) screenshotImage(c *gin.Context) {
	name, ok := s.screenshotSite(c)
	if !ok {
		return
	}
	image, err := s.Screenshots.Image(name)
	if err != nil {
		fail(c, 404, "暂无可用截图")
		return
	}
	c.Data(200, "image/jpeg", image)
}
