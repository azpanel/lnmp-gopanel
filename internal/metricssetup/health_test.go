package metricssetup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestPanelHealthUsesConfiguredHTTPSListenerWithoutCredentials(t *testing.T) {
	calls := make(chan struct{}, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/session" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected health request")
		}
		select {
		case calls <- struct{}{}:
		default:
		}
		http.Error(w, "请先登录", http.StatusUnauthorized)
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "https://")
	hashed, _ := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "username: admin\npassword_hash: " + string(hashed) + "\nlisten: " + address + "\ntls:\n  cert_file: /unused/cert\n  key_file: /unused/key\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	e := systemEnvironment()
	e.active = func(context.Context, string) bool { return true }
	if err := e.panelCheck(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatal("listener was not checked")
	}
	e.active = func(context.Context, string) bool { return false }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := e.panelCheck(ctx, path); err == nil {
		t.Fatal("HTTP response without a live panel accepted")
	}
}
