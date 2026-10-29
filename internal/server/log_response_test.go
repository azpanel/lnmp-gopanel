package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
)

func TestLogResponseCompressionAndTiming(t *testing.T) {
	s, _ := testServer(t)
	text := strings.Repeat("114.114.114.114 evaluation 日志\n", 300)
	reader := &stubLogs{lines: []logs.Line{{Offset: "0", Text: text}}}
	s.Logs = reader
	s.logReads = make(chan struct{}, 1)
	id, session := s.Sessions.Create()
	h := s.Handler()
	for _, tc := range []struct {
		encoding string
		gzip     bool
	}{
		{"", false}, {"br", false}, {"xgzip", false}, {"gzip", true},
		{"br, gzip;q=0.5", true}, {"GZIP; Q=1", true}, {"*;q=1", true},
		{"gzip;q=0, *;q=1", false}, {"*;q=1, gzip;q=0", false},
		{"gzip;q=invalid", false}, {"gzip;q=NaN", false}, {"gzip;q=2", false},
	} {
		t.Run(tc.encoding, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/v1/logs/read", strings.NewReader(`{"path":"/home/wwwlogs/a.log","mode":"tail","rules":[]}`))
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: id})
			r.Header.Set("X-CSRF-Token", session.CSRF)
			r.Header.Set("Accept-Encoding", tc.encoding)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Vary") != "Accept-Encoding" {
				t.Fatalf("unexpected response: %d %v", w.Code, w.Header())
			}
			if (w.Header().Get("Content-Encoding") == "gzip") != tc.gzip {
				t.Fatalf("wrong encoding: %v", w.Header())
			}
			if w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
				t.Fatal("response size does not match Content-Length")
			}
			var data io.Reader = w.Body
			if tc.gzip {
				if w.Body.Len() >= len(text)/2 {
					t.Fatal("response was not meaningfully compressed")
				}
				decoded, err := gzip.NewReader(w.Body)
				if err != nil {
					t.Fatal(err)
				}
				defer decoded.Close()
				data = decoded
			}
			var out logs.Window
			if err := json.NewDecoder(data).Decode(&out); err != nil || len(out.Lines) != 1 || out.Lines[0].Text != text || out.Lines[0].IPLocation == "" {
				t.Fatalf("log contents or enrichment changed: %v", err)
			}
			if w.Header().Get("X-Log-Request-ID") == "" {
				t.Fatal("missing correlation ID")
			}
			metrics := strings.Split(w.Header().Get("Server-Timing"), ", ")
			if len(metrics) != 5 {
				t.Fatal("missing timing stages", metrics)
			}
			for _, metric := range metrics {
				_, value, ok := strings.Cut(metric, ";dur=")
				duration, err := strconv.ParseFloat(value, 64)
				if !ok || err != nil || duration < 0 {
					t.Fatal("invalid timing", metric)
				}
			}
		})
	}
	reader.lines = []logs.Line{{Offset: "0", Text: "tiny response"}}
	r := httptest.NewRequest("POST", "/api/v1/logs/read", strings.NewReader(`{"path":"/home/wwwlogs/a.log","mode":"after","end":"invalid"}`))
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: id})
	r.Header.Set("X-CSRF-Token", session.CSRF)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || w.Header().Get("Content-Encoding") != "" {
		t.Fatal("invalid requests must still be rejected without compression")
	}
	r = httptest.NewRequest("POST", "/api/v1/logs/read", strings.NewReader(`{"path":"/home/wwwlogs/a.log","mode":"tail","rules":[]}`))
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: id})
	r.Header.Set("X-CSRF-Token", session.CSRF)
	r.Header.Set("Accept-Encoding", "gzip")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "" {
		t.Fatal("tiny responses should avoid compression overhead")
	}
}

func TestLargeLogTailResponseAndHistory(t *testing.T) {
	s, _ := testServer(t)
	s.Logs = logs.Store{}
	s.logReads = make(chan struct{}, 1)
	var err error
	s.CustomLogs, err = logs.OpenRegistry(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "large.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	const size = int64(1024 * 1024 * 1024)
	line := strings.Repeat("evaluation ", 11) + "------\n"
	content := []byte(strings.Repeat(line, int((logs.BatchBytes+logs.TailBytes+logs.LineBytes)/int64(len(line)))))
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte(strings.Repeat(line, 4096/len(line))), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(content, size-int64(len(content))); err != nil {
		t.Fatal(err)
	}
	if err := s.CustomLogs.Add(path); err != nil {
		t.Fatal(err)
	}
	id, session := s.Sessions.Create()
	h := s.Handler()
	read := func(input logs.Request, compressed bool) (logs.Window, int) {
		t.Helper()
		input.Path = path
		body, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", "/api/v1/logs/read", strings.NewReader(string(body)))
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: id})
		r.Header.Set("X-CSRF-Token", session.CSRF)
		if compressed {
			r.Header.Set("Accept-Encoding", "gzip")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("read failed: %d %s", w.Code, w.Body.String())
		}
		responseBytes := w.Body.Len()
		var data io.Reader = w.Body
		if compressed {
			if w.Header().Get("Content-Encoding") != "gzip" {
				t.Fatal("large response was not compressed")
			}
			decoded, err := gzip.NewReader(w.Body)
			if err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			data = decoded
		}
		var out logs.Window
		if err := json.NewDecoder(data).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out, responseBytes
	}
	tail, compressedBytes := read(logs.Request{Mode: "tail"}, true)
	plain, plainBytes := read(logs.Request{Mode: "tail"}, false)
	from, _ := strconv.ParseInt(tail.From, 10, 64)
	if tail.Size != size || size-from != logs.TailBytes || tail.HasAfter || !tail.HasBefore || len(tail.Lines) == 0 {
		t.Fatal("large file did not use a bounded tail window")
	}
	if plain.From != tail.From || plain.To != tail.To || len(plain.Lines) != len(tail.Lines) || compressedBytes >= plainBytes/2 {
		t.Fatal("compression changed the window or did not reduce transfer size")
	}
	history, _ := read(logs.Request{Mode: "before", Start: tail.Start}, true)
	historyFrom, _ := strconv.ParseInt(history.From, 10, 64)
	if history.To != tail.From || from-historyFrom != logs.BatchBytes {
		t.Fatal("large-file history lost its 256 KiB batch or has a gap")
	}
	t.Logf("1 GiB file: tail=%d bytes, history=%d bytes, JSON=%d bytes, gzip=%d bytes", size-from, from-historyFrom, plainBytes, compressedBytes)
}
