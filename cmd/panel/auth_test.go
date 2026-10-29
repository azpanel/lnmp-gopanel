package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

func TestOfflineTOTPRecovery(t *testing.T) {
	dir := t.TempDir()
	hash, _ := bcrypt.GenerateFromPassword([]byte("legacy-password-123"), bcrypt.MinCost)
	c := config.Config{Username: "admin", PasswordHash: string(hash), Auth: config.AuthConfig{StateDir: filepath.Join(dir, "auth")}, TLS: config.TLSConfig{CertFile: "/tls/cert", KeyFile: "/tls/key"}}
	data, _ := yaml.Marshal(c)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := auth.OpenAccount(c.Auth.StateDir, c.Username, c.PasswordHash)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := a.StartSetup("session", "legacy-password-123")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	if _, err = a.ConfirmSetup("session", setup.ID, code); err != nil {
		t.Fatal(err)
	}
	args := []string{"disable-totp", "--config", path}
	if configureAuth(args) == nil {
		t.Fatal("offline recovery modified a running account")
	}
	a.Close()
	if err = configureAuth(args); err != nil {
		t.Fatal(err)
	}
	restored, err := auth.OpenAccount(c.Auth.StateDir, c.Username, c.PasswordHash)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.Security().TOTPEnabled || restored.Security().RecoveryRemaining != 0 {
		t.Fatal("TOTP was not removed")
	}
	if challenge, err := restored.StartLogin("legacy-password-123"); err != nil || challenge != "" {
		t.Fatal("offline recovery changed the password")
	}
	if configureAuth([]string{"unknown"}) == nil {
		t.Fatal("unexpected auth subcommand accepted")
	}
}

func TestOfflinePasswordResetCLI(t *testing.T) {
	dir := t.TempDir()
	hash, _ := bcrypt.GenerateFromPassword([]byte("legacy-password-123"), bcrypt.MinCost)
	c := config.Config{Username: "operator", PasswordHash: string(hash), Auth: config.AuthConfig{StateDir: filepath.Join(dir, "auth")}, TLS: config.TLSConfig{CertFile: "/tls/cert", KeyFile: "/tls/key"}}
	data, _ := yaml.Marshal(c)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	call := func(password string) error {
		t.Helper()
		input, err := os.CreateTemp(dir, "input-*")
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		defer os.Remove(input.Name())
		if _, err := input.WriteString(password); err != nil {
			t.Fatal(err)
		}
		if _, err := input.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		previous := os.Stdin
		os.Stdin = input
		defer func() { os.Stdin = previous }()
		return configureAuth([]string{"reset-password", "--config", path, "--password-stdin"})
	}
	password := "fH8!zW2_pN6+vK4=qR9#sD7"
	a, err := auth.OpenAccount(c.Auth.StateDir, c.Username, c.PasswordHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := call(password + "\n"); err == nil {
		t.Fatal("reset running account")
	}
	a.Close()
	if err := call("weak\n"); err == nil {
		t.Fatal("weak password accepted")
	}
	if err := call(password + "\r\n"); err != nil {
		t.Fatal(err)
	}
	restored, err := auth.OpenAccount(c.Auth.StateDir, c.Username, c.PasswordHash)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := restored.StartLogin("legacy-password-123"); err == nil {
		t.Fatal("bootstrap password survived")
	}
	if challenge, err := restored.StartLogin(password); err != nil || challenge != "" {
		t.Fatal(challenge, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatal("configuration changed", err)
	}
	if configureAuth([]string{"reset-password", "--config", path, "unexpected"}) == nil {
		t.Fatal("accepted extra argument")
	}
}
