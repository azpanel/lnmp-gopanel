package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

type trackedFrontendFS struct {
	fs.FS
	opened []*trackedFrontendFile
}

type trackedFrontendFile struct {
	fs.File
	name   string
	read   int
	closed bool
}

func (f *trackedFrontendFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.read += n
	return n, err
}

func (f *trackedFrontendFile) Seek(offset int64, whence int) (int64, error) {
	return f.File.(io.Seeker).Seek(offset, whence)
}

func (f *trackedFrontendFile) Close() error {
	f.closed = true
	return f.File.Close()
}

func (f *trackedFrontendFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	tracked := &trackedFrontendFile{File: file, name: name}
	f.opened = append(f.opened, tracked)
	return tracked, nil
}

func TestFrontendStreamsAndClosesFiles(t *testing.T) {
	plain := []byte(strings.Repeat("console.log('panel');\n", 40000))
	for _, tc := range []struct {
		method, encoding string
		compressed       []byte
		wantPlainRead    bool
	}{
		{"GET", "gzip", []byte("compressed"), false},
		{"HEAD", "gzip", []byte("compressed"), false},
		{"GET", "identity", []byte("compressed"), true},
		{"HEAD", "identity", []byte("compressed"), false},
		{"GET", "gzip", plain, true}, // Do not use a larger representation.
	} {
		t.Run(tc.method+tc.encoding+strconv.Itoa(len(tc.compressed)), func(t *testing.T) {
			assets := &trackedFrontendFS{FS: fstest.MapFS{
				"assets/app.js":    &fstest.MapFile{Data: plain},
				"assets/app.js.gz": &fstest.MapFile{Data: tc.compressed},
			}}
			s := &Server{Assets: assets}
			r := httptest.NewRequest(tc.method, "/assets/app.js", nil)
			r.Header.Set("Accept-Encoding", tc.encoding)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			for _, file := range assets.opened {
				if !file.closed {
					t.Fatalf("file not closed: %s", file.name)
				}
				if file.name == "assets/app.js" && (file.read > 0) != tc.wantPlainRead {
					t.Fatalf("unexpected original file read: %d bytes", file.read)
				}
				if tc.method == "HEAD" && file.read != 0 {
					t.Fatalf("HEAD read %d bytes from %s", file.read, file.name)
				}
			}
		})
	}
}

type nonSeekingFrontendFS struct{ fs.FS }

func (f nonSeekingFrontendFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return struct{ fs.File }{file}, nil
}

func TestFrontendFilesystemCompatibility(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("panel"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, assets := range []fs.FS{
		os.DirFS(dir),
		nonSeekingFrontendFS{FS: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("panel")}}},
	} {
		s := &Server{Assets: assets}
		for _, path := range []string{"/", "/services"} {
			w := request(s.Handler(), "GET", path, "", nil, "", "")
			if w.Code != 200 || w.Body.String() != "panel" {
				t.Fatalf("%T %s: status=%d body=%q", assets, path, w.Code, w.Body.String())
			}
		}
	}
	s := &Server{Assets: fstest.MapFS{}}
	if w := request(s.Handler(), "GET", "/services", "", nil, "", ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

type frontendBenchmarkWriter struct{ header http.Header }

func (w frontendBenchmarkWriter) Header() http.Header       { return w.header }
func (frontendBenchmarkWriter) WriteHeader(int)             {}
func (frontendBenchmarkWriter) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkFrontendAsset(b *testing.B) {
	plain := []byte(strings.Repeat("console.log('panel');\n", 40000))
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(plain)
	_ = writer.Close()
	s := &Server{Assets: fstest.MapFS{
		"assets/app.js":    &fstest.MapFile{Data: plain},
		"assets/app.js.gz": &fstest.MapFile{Data: compressed.Bytes()},
	}}
	h := s.Handler()
	for _, encoding := range []string{"identity", "gzip"} {
		b.Run(encoding, func(b *testing.B) {
			r := httptest.NewRequest("GET", "/assets/app.js", nil)
			r.Header.Set("Accept-Encoding", encoding)
			w := frontendBenchmarkWriter{header: make(http.Header)}
			b.ReportAllocs()
			b.SetBytes(int64(len(plain)))
			b.ResetTimer()
			for b.Loop() {
				clear(w.header)
				h.ServeHTTP(w, r)
			}
		})
	}
}

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
