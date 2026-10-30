package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFrontendCompressionNegotiationAndRanges(t *testing.T) {
	plain := []byte(strings.Repeat("console.log('panel');\n", 200))
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, _ = writer.Write(plain)
	_ = writer.Close()
	s, _ := testServer(t)
	s.Assets = fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("<html>panel</html>")},
		"assets/app.js":    &fstest.MapFile{Data: plain},
		"assets/app.js.gz": &fstest.MapFile{Data: buffer.Bytes()},
		"assets/app.css":   &fstest.MapFile{Data: plain}, // Missing precompression falls back.
	}
	h := s.Handler()
	for _, tc := range []struct {
		method, file, encoding, byteRange string
		compressed                        bool
		status                            int
	}{
		{"GET", "app.js", "gzip", "", true, 200},
		{"HEAD", "app.js", "gzip", "", true, 200},
		{"GET", "app.js", "br, GZIP;Q=0.5", "", true, 200},
		{"GET", "app.js", "*;q=1", "", true, 200},
		{"GET", "app.js", "gzip;q=0, *;q=1", "", false, 200},
		{"GET", "app.js", "gzip;q=NaN", "", false, 200},
		{"GET", "app.js", "br", "", false, 200},
		{"GET", "app.js", "", "", false, 200},
		{"GET", "app.css", "gzip", "", false, 200},
		{"GET", "app.js", "gzip", "bytes=0-15", false, 206},
		{"GET", "app.js", "gzip", "bytes=99999-", false, 416},
	} {
		t.Run(tc.method+tc.file+tc.encoding+tc.byteRange, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/assets/"+tc.file, nil)
			r.Header.Set("Accept-Encoding", tc.encoding)
			if tc.byteRange != "" {
				r.Header.Set("Range", tc.byteRange)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || (w.Header().Get("Content-Encoding") == "gzip") != tc.compressed {
				t.Fatalf("status=%d encoding=%q", w.Code, w.Header().Get("Content-Encoding"))
			}
			if tc.status >= 400 {
				return
			}
			if w.Header().Get("Vary") != "Accept-Encoding" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
				t.Fatal("negotiated representation is not cached safely", w.Header())
			}
			mime := "javascript"
			if tc.file == "app.css" {
				mime = "text/css"
			}
			if !strings.Contains(w.Header().Get("Content-Type"), mime) {
				t.Fatal(w.Header())
			}
			want := plain
			if tc.byteRange != "" {
				want = plain[:16]
			}
			if tc.method == "HEAD" {
				if w.Body.Len() != 0 || w.Header().Get("Content-Length") != strconv.Itoa(buffer.Len()) {
					t.Fatal(w.Header())
				}
				return
			}
			var data []byte
			if tc.compressed {
				reader, err := gzip.NewReader(w.Body)
				if err != nil {
					t.Fatal(err)
				}
				data, err = io.ReadAll(reader)
				_ = reader.Close()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				data = w.Body.Bytes()
			}
			if !bytes.Equal(data, want) {
				t.Fatal("negotiation changed resource contents")
			}
		})
	}
	for _, name := range []string{"/", "/services", "/api/v1/auth/session", "/assets/missing.js"} {
		r := httptest.NewRequest("GET", name, nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Header().Get("Content-Encoding") != "" {
			t.Fatalf("unexpected gzip: %s", name)
		}
	}
}
