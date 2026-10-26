package server

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/components"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
)

type componentBackend struct {
	calls atomic.Int32
	done  chan struct{}
}

func (b *componentBackend) Components(ctx context.Context) (components.Snapshot, error) {
	b.calls.Add(1)
	select {
	case <-b.done:
	case <-ctx.Done():
		return components.Snapshot{}, ctx.Err()
	}
	items := []components.Item{}
	for _, name := range components.Names() {
		items = append(items, components.Item{Name: name, Status: "ready", Version: "1.2.3"})
	}
	return components.Snapshot{Status: "ready", Items: items}, nil
}
func TestComponentAPIAuthCSRFAndAsyncWithoutServices(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &componentBackend{done: make(chan struct{})}
	s.Components = components.NewManager(ctx, b)
	s.Services = control.New([]config.Service{}, &stubExecutor{})
	h := s.Handler()
	for _, method := range []string{"GET", "POST"} {
		path := "/api/v1/components"
		if method == "POST" {
			path += "/refresh"
		}
		if w := request(h, method, path, "", nil, "", ""); w.Code != 401 {
			t.Fatal("components exposed without auth")
		}
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", "/api/v1/components/refresh", "", cookie, "", ""); w.Code != 403 {
		t.Fatal("refresh bypassed CSRF")
	}
	if b.calls.Load() != 0 {
		t.Fatal("unauthorized collection")
	}
	for _, method := range []string{"GET", "POST"} {
		path, want := "/api/v1/components", 200
		if method == "POST" {
			path += "/refresh"
			want = 202
		}
		w := request(h, method, path, "", cookie, session.CSRF, "")
		if w.Code != want || !strings.Contains(w.Body.String(), `"status":"loading"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	close(b.done)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		w := request(h, "GET", "/api/v1/components", "", cookie, "", "")
		if strings.Count(w.Body.String(), `"status":"ready"`) == 5 {
			if b.calls.Load() != 1 || !strings.Contains(w.Body.String(), `"name":"codex"`) {
				t.Fatal(w.Body.String(), b.calls.Load())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("collection did not complete")
}
