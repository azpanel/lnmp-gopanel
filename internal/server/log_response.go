package server

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/gin-gonic/gin"
)

type logReadTiming struct {
	id                     string
	started                time.Time
	read, geo, encode, zip time.Duration
	write                  time.Duration
	rows, raw, sent        int
	compressed             bool
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (t *logReadTiming) headers(c *gin.Context) {
	c.Header("X-Log-Request-ID", t.id)
	c.Header("Server-Timing", fmt.Sprintf("read;dur=%.3f, geo;dur=%.3f, encode;dur=%.3f, compress;dur=%.3f, total;dur=%.3f",
		milliseconds(t.read), milliseconds(t.geo), milliseconds(t.encode), milliseconds(t.zip), milliseconds(time.Since(t.started))))
}

func (t *logReadTiming) record(c *gin.Context, mode string) {
	total := time.Since(t.started)
	level := slog.LevelDebug
	if mode == "tail" || total >= 500*time.Millisecond || c.Writer.Status() >= 400 {
		level = slog.LevelInfo
	}
	// Only timings and sizes: never include log text, rules, cursors or credentials.
	slog.Log(c.Request.Context(), level, "日志读取耗时", "request_id", t.id, "mode", mode,
		"status", c.Writer.Status(), "read_ms", milliseconds(t.read), "geo_ms", milliseconds(t.geo),
		"encode_ms", milliseconds(t.encode), "compress_ms", milliseconds(t.zip), "write_ms", milliseconds(t.write),
		"total_ms", milliseconds(total), "rows", t.rows, "json_bytes", t.raw, "response_bytes", t.sent, "gzip", t.compressed)
}

// An explicit gzip;q=0 takes precedence over a wildcard. Missing, malformed or
// unsupported encodings fall back to the existing uncompressed response.
func acceptsGzip(header string) bool {
	wildcard := false
	for part := range strings.SplitSeq(header, ",") {
		params := strings.Split(part, ";")
		coding := strings.TrimSpace(params[0])
		quality := 1.0
		for _, param := range params[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
				q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || !(q >= 0 && q <= 1) {
					quality = 0
				} else {
					quality = q
				}
			}
		}
		if strings.EqualFold(coding, "gzip") {
			return quality > 0
		}
		if coding == "*" {
			wildcard = quality > 0
		}
	}
	return wildcard
}

func writeLogWindow(c *gin.Context, out logs.Window, timing *logReadTiming) {
	timing.rows = len(out.Lines)
	started := time.Now()
	data, err := json.Marshal(out)
	timing.encode = time.Since(started)
	if err != nil {
		timing.headers(c)
		fail(c, http.StatusInternalServerError, "日志响应编码失败")
		return
	}
	timing.raw = len(data)
	c.Writer.Header().Add("Vary", "Accept-Encoding")
	if len(data) >= 1024 && acceptsGzip(strings.Join(c.Request.Header.Values("Accept-Encoding"), ",")) {
		started = time.Now()
		var compressed bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
		_, writeErr := writer.Write(data)
		closeErr := writer.Close()
		if writeErr == nil && closeErr == nil && compressed.Len() < len(data) {
			data = compressed.Bytes()
			timing.compressed = true
			c.Header("Content-Encoding", "gzip")
		}
		timing.zip = time.Since(started)
	}
	timing.sent = len(data)
	timing.headers(c)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	started = time.Now()
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	timing.write = time.Since(started)
}

func newLogReadTiming() *logReadTiming {
	return &logReadTiming{id: rand.Text(), started: time.Now()}
}
