package vhosts

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestBasicAuthValidation(t *testing.T) {
	for _, r := range []BasicAuthInput{
		{Name: "../site.conf", Username: "user", Password: "password"},
		{Name: "site.conf", Username: "user:name", Password: "password"},
		{Name: "site.conf", Username: "", Password: "password"},
		{Name: "site.conf", Username: "user", Password: "short"},
		{Name: "site.conf", Username: "user", Password: "password\n"},
		{Name: "site.conf", Username: "user", Password: strings.Repeat("中", 86)},
	} {
		if r.Validate() == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
	if err := (BasicAuthInput{Name: "site.conf", Username: "User.test-1_2", Password: "密码：long enough"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
func authTestPath(s Store, name string) string {
	return filepath.Join(s.BasicAuthDirectory(), BasicAuthKey(name)+"-"+strings.Repeat("a", 32)+".htpasswd")
}
func TestBasicAuthSurgicalRoundTripAndMarkers(t *testing.T) {
	s := testStore(t)
	name := "site.conf"
	file := authTestPath(s, name)
	for _, base := range []string{
		"# preserved\r\nserver { listen 80; root \"/path/{with} spaces\"; }\r\nserver{listen 443 ssl;}\r\n",
		"server { location ^~ /.well-known/acme-challenge/ { alias /tokens/; } location ~ \\.php$ { fastcgi_pass unix:/run/php.sock; } }",
		"server { set $v ${host}; location / { proxy_pass http://localhost:8080; } }",
	} {
		content, err := s.AttachBasicAuth(name, base, file)
		if err != nil {
			t.Fatal(err)
		}
		stripped, credential, err := s.StripBasicAuth(name, content)
		if err != nil || stripped != base || credential != file {
			t.Fatal("round trip did not preserve bytes", err)
		}
		if strings.Count(content, "auth_basic_user_file") != strings.Count(base, "server") {
			t.Fatal("not all server blocks protected")
		}
		if strings.Count(content, "auth_basic off;") != strings.Count(base, "server") {
			t.Fatal("challenge not exempted")
		}
		for _, bad := range []string{
			strings.Replace(content, "Restricted", "Changed", 1),
			strings.Replace(content, "auth_basic off;", "auth_basic on;", 1),
			strings.Replace(content, authEnd, "", 1),
			content + authBlock("auth_basic off;\n"),
			strings.Replace(content, file, authTestPath(s, "other.conf"), 1),
		} {
			if _, _, err := s.StripBasicAuth(name, bad); err == nil {
				t.Fatal("tampered marker accepted")
			}
		}
	}
	if _, err := s.AttachBasicAuth(name, "# no server\n", file); err == nil {
		t.Fatal("empty vhost accepted")
	}
	if _, err := s.AttachBasicAuth(name, "server { location ~ acme-challenge { root /tokens; } }", file); err == nil {
		t.Fatal("ambiguous challenge accepted")
	}
}
func TestBasicAuthIncludeInspection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	files := map[string]string{filepath.Join(s.Prefix, "conf", "shared.conf"): "location /private { auth_basic secret; }", filepath.Join(s.Prefix, "conf", "cycle.conf"): "include cycle.conf;"}
	read := func(path string) (string, error) {
		if data, ok := files[path]; ok {
			return data, nil
		}
		return "", errors.New("unreadable")
	}
	// Glob operates on actual paths; contents are supplied through a trusted reader.
	for p, data := range files {
		put(t, p, data)
	}
	for _, data := range []string{"server { auth_basic off; }", "server { include shared.conf; }", "server { location / { satisfy any; } }"} {
		conflict, err := s.InspectAuthDirectives(ctx, data, false, read)
		if err != nil || conflict == "" {
			t.Fatal("external auth not detected", err)
		}
	}
	for _, data := range []string{"server { include $host; }", "server { include missing.conf; }", "server { include cycle.conf; }", "server { return 200 ok; }", "server { rewrite_by_lua_file /root/script.lua; }", "server { content_by_lua_block { ngx.say('hello') } }"} {
		if _, err := s.InspectAuthDirectives(ctx, data, false, read); err == nil {
			t.Fatal("uncertain configuration accepted")
		}
	}
	if conflict, err := s.InspectAuthDirectives(ctx, "http { map $http_upgrade $connection_upgrade { default upgrade; '' close; } server { auth_basic other-site; } }", true, read); err != nil || conflict != "" {
		t.Fatal("another site caused conflict", err)
	}
	if conflict, err := s.InspectAuthDirectives(ctx, "http { auth_basic inherited; server {} }", true, read); err != nil || conflict == "" {
		t.Fatal("inherited auth missed", err)
	}
	for _, data := range []string{"server {}", "location ^~ /.well-known/acme-challenge/ {}"} {
		path := filepath.Join(s.Prefix, "conf", "shared.conf")
		put(t, path, data)
		files[path] = data
		if _, err := s.InspectAuthDirectives(ctx, "server { include shared.conf; }", false, read); err == nil {
			t.Fatal("shared structural configuration accepted")
		}
	}
}

type authManagerBackend struct {
	testBackend
	started, release chan struct{}
}

func (b *authManagerBackend) ReadVhostBasicAuth(context.Context, string) (BasicAuthState, error) {
	return BasicAuthState{}, nil
}
func (b *authManagerBackend) SetVhostBasicAuth(ctx context.Context, r BasicAuthInput) (BasicAuthState, error) {
	close(b.started)
	select {
	case <-b.release:
		return BasicAuthState{Name: r.Name, Status: "enabled"}, nil
	case <-ctx.Done():
		return BasicAuthState{}, ctx.Err()
	}
}
func (b *authManagerBackend) DisableVhostBasicAuth(context.Context, string) (BasicAuthState, error) {
	return BasicAuthState{}, nil
}
func TestBasicAuthManagerSerializesSiteOperations(t *testing.T) {
	b := &authManagerBackend{started: make(chan struct{}), release: make(chan struct{})}
	m := NewManager(context.Background(), b)
	done := make(chan error, 1)
	go func() {
		_, err := m.ChangeBasicAuth(BasicAuthInput{Name: "site.conf", Username: "user", Password: "test-password"}, false)
		done <- err
	}()
	<-b.started
	if _, err := m.Toggle("site.conf", false); err == nil {
		t.Fatal("parallel toggle allowed")
	}
	if _, err := m.ChangeBasicAuth(BasicAuthInput{Name: "site.conf"}, true); err == nil {
		t.Fatal("parallel auth mutation allowed")
	}
	close(b.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
