package metricssetup

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"gopkg.in/yaml.v3"
)

func TestNginxIncludeUsesSyntaxPositions(t *testing.T) {
	// Braces inside a quoted regular expression are not block boundaries.
	original := []byte("# server { fake }\nevents {}\nhttp {\n server { location ~ \"x{2}\" { return 404; } }\n}\n")
	updated, err := addMetricsInclude(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte("include "+managedConfig+";")) || !bytes.Contains(updated, []byte(`location ~ "x{2}"`)) {
		t.Fatal("unrelated configuration changed")
	}
	second, err := addMetricsInclude(updated)
	if err != nil || !bytes.Equal(second, updated) {
		t.Fatal("include is not idempotent", err)
	}
	for _, bad := range []string{"events {}", "http {} http {}", "http { include " + managedConfig + "; include " + managedConfig + "; }", "http {"} {
		if _, err = addMetricsInclude([]byte(bad)); err == nil {
			t.Fatal("ambiguous configuration accepted", bad)
		}
	}
}
func TestNginxRoutesAreLocalAndOnlySelected(t *testing.T) {
	data, err := renderNginx(18080, true, "/fpm-status", "/tmp/php-cgi.sock")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"listen 127.0.0.1:18080;", "location = /nginx_status", "location = /fpm-status", "fastcgi_pass unix:/tmp/php-cgi.sock;", "fastcgi_param QUERY_STRING $query_string;", "location / { return 404; }"} {
		if !strings.Contains(string(data), expected) {
			t.Fatal("missing", expected)
		}
	}
	single, err := renderNginx(18080, false, "/fpm-status", "127.0.0.1:9000")
	if err != nil || strings.Contains(string(single), "stub_status") {
		t.Fatal("unselected nginx route enabled", err)
	}
	for _, bad := range []struct {
		port         int
		path, listen string
	}{{80, "/status", "/socket"}, {65536, "/status", "/socket"}, {18080, "/nginx_status", "/socket"}, {18080, "/status;return 200", "/socket"}, {18080, "/status", "/tmp/socket;evil"}, {18080, "/status", "0.0.0.0:9000"}, {18080, "/status", "$dynamic"}} {
		if _, err = renderNginx(bad.port, true, bad.path, bad.listen); err == nil {
			t.Fatal("unsafe automatic configuration accepted", bad)
		}
	}
}
func TestPanelPatchPreservesValuesCommentsAndUnselectedConnections(t *testing.T) {
	original := []byte("# existing settings\nusername: original\npassword_hash: unchanged\nmetrics:\n  state_dir: /keep/state # preserve\n  nginx_status_url: http://127.0.0.1:8000/old\n  php_fpm_status_url: http://127.0.0.1:8001/keep\n  mysql:\n    socket: /old/socket\n    user: old_user\n    password: 'old#secret'\n")
	next := config.MetricsConfig{NginxStatusURL: "http://127.0.0.1:18080/nginx_status", MySQL: config.MetricsMySQLConfig{Socket: "/tmp/mysql.sock", User: "monitor", Password: "quote'\\\"#复杂密码"}}
	data, err := patchPanel(original, next, map[string]bool{"nginx": true, "database": true})
	if err != nil {
		t.Fatal(err)
	}
	var parsed config.Config
	if yaml.Unmarshal(data, &parsed) != nil {
		t.Fatal("invalid YAML generated")
	}
	if parsed.Username != "original" || parsed.PasswordHash != "unchanged" || parsed.Metrics.StateDir != "/keep/state" || parsed.Metrics.PHPFPMStatusURL != "http://127.0.0.1:8001/keep" || parsed.Metrics.MySQL != next.MySQL {
		t.Fatal("settings changed or password corrupted")
	}
	if !bytes.Contains(data, []byte("# existing settings")) || !bytes.Contains(data, []byte("# preserve")) {
		t.Fatal("comments lost")
	}
	for _, bad := range []string{"[]", "null", "metrics: alias", "metrics:\n  mysql: invalid\n"} {
		if _, err = patchPanel([]byte(bad), next, map[string]bool{"database": true}); err == nil {
			t.Fatal("ambiguous YAML accepted", bad)
		}
	}
}

func TestGeneratedNginxWithLocalParser(t *testing.T) {
	binary := os.Getenv("GOPANEL_TEST_NGINX")
	if binary == "" {
		t.Skip("optional nginx syntax validation against a temporary configuration")
	}
	generated, err := renderNginx(18080, true, "/fpm-status", "/tmp/php-cgi.sock")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "nginx.conf")
	content := "error_log stderr;\npid " + filepath.Join(dir, "nginx.pid") + ";\nevents {}\nhttp {\n" + string(generated) + "}\n"
	if err = os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-t", "-p", dir+"/", "-c", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated configuration rejected: %v %s", err, output)
	}
}
