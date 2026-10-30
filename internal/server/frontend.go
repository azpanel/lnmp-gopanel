package server

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) serveFrontend(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		fail(c, 404, "接口不存在")
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(c.Request.URL.Path, "/")
	if !fs.ValidPath(name) || name == "." {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.Assets, name)
	if err != nil {
		if strings.HasPrefix(name, "assets/") || strings.Contains(name, ".") {
			c.Status(http.StatusNotFound)
			return
		}
		data, err = fs.ReadFile(s.Assets, "index.html")
		name = "index.html"
	}
	if err != nil {
		c.String(http.StatusServiceUnavailable, "前端尚未构建，请运行 make build")
		return
	}
	if name == "index.html" {
		c.Header("Cache-Control", "no-store")
	} else if strings.HasPrefix(name, "assets/") {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	// Only build-generated JS/CSS representations are compressed. Range requests
	// keep their existing identity byte offsets; APIs and SSE never enter here.
	extension := path.Ext(name)
	if strings.HasPrefix(name, "assets/") && (extension == ".js" || extension == ".css") {
		c.Writer.Header().Add("Vary", "Accept-Encoding")
		if c.Request.Header.Get("Range") == "" && acceptsGzip(strings.Join(c.Request.Header.Values("Accept-Encoding"), ",")) {
			if compressed, err := fs.ReadFile(s.Assets, name+".gz"); err == nil && len(compressed) < len(data) {
				data = compressed
				c.Header("Content-Encoding", "gzip")
				// ServeContent does not calculate lengths for encoded responses.
				c.Header("Content-Length", strconv.Itoa(len(data)))
			}
		}
	}
	if contentType := mime.TypeByExtension(extension); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(data))
}
