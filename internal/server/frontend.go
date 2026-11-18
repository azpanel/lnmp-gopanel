package server

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type frontendAsset struct {
	io.ReadSeeker
	closer io.Closer
	size   int64
}

func (a *frontendAsset) Close() error {
	if a.closer != nil {
		return a.closer.Close()
	}
	return nil
}

func openFrontendAsset(assets fs.FS, name string) (*frontendAsset, error) {
	file, err := assets.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		_ = file.Close()
		if err == nil {
			err = &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
		}
		return nil, err
	}
	if seeker, ok := file.(io.ReadSeeker); ok {
		return &frontendAsset{ReadSeeker: seeker, closer: file, size: info.Size()}, nil
	}
	// embed.FS and os.DirFS support seeking. Keep compatibility with other FS
	// implementations without making production requests copy the entire asset.
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return nil, err
	}
	return &frontendAsset{ReadSeeker: bytes.NewReader(data), size: int64(len(data))}, nil
}

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
	asset, err := openFrontendAsset(s.Assets, name)
	if err != nil {
		if strings.HasPrefix(name, "assets/") || strings.Contains(name, ".") {
			c.Status(http.StatusNotFound)
			return
		}
		asset, err = openFrontendAsset(s.Assets, "index.html")
		name = "index.html"
	}
	if err != nil {
		c.String(http.StatusServiceUnavailable, "前端尚未构建，请运行 make build")
		return
	}
	defer asset.Close()
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
			if compressed, err := openFrontendAsset(s.Assets, name+".gz"); err == nil {
				if compressed.size < asset.size {
					defer compressed.Close()
					asset = compressed
					c.Header("Content-Encoding", "gzip")
					// ServeContent does not calculate lengths for encoded responses.
					c.Header("Content-Length", strconv.FormatInt(asset.size, 10))
				} else {
					_ = compressed.Close()
				}
			}
		}
	}
	if contentType := mime.TypeByExtension(extension); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, asset)
}
