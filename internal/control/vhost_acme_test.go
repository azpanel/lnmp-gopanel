package control

import (
	"context"
	"errors"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestACMEClientDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name, rootKind, localKind, untrusted, want string
	}{
		{"root installation", "executable", "missing", "", "root"},
		{"LNMP installation", "missing", "executable", "", "local"},
		{"prefer existing root installation", "executable", "executable", "", "root"},
		{"skip unsafe root installation", "executable", "executable", "root", "local"},
		{"skip nonexecutable file", "file", "executable", "", "local"},
		{"skip directory", "directory", "executable", "", "local"},
		{"reject missing installations", "missing", "missing", "", ""},
		{"reject unsafe installation", "missing", "executable", "local", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			homes := map[string]string{"root": filepath.Join(dir, "root/.acme.sh"), "local": filepath.Join(dir, "usr/local/acme.sh")}
			for label, kind := range map[string]string{"root": tc.rootKind, "local": tc.localKind} {
				if err := os.MkdirAll(homes[label], 0755); err != nil {
					t.Fatal(err)
				}
				program := filepath.Join(homes[label], "acme.sh")
				switch kind {
				case "executable", "file":
					mode := os.FileMode(0755)
					if kind == "file" {
						mode = 0644
					}
					if err := os.WriteFile(program, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(program, 0755); err != nil {
						t.Fatal(err)
					}
				}
			}
			trust := func(path string) error {
				if tc.untrusted != "" && path == filepath.Join(homes[tc.untrusted], "acme.sh") {
					return errors.New("untrusted")
				}
				return nil
			}
			client, err := findACMEClient([]string{homes["root"], homes["local"]}, trust)
			if tc.want == "" {
				if err == nil {
					t.Fatal("accepted unavailable installation", client.home)
				}
			} else if err != nil || client.home != homes[tc.want] {
				t.Fatal(client.home, err)
			}
		})
	}
}

func TestACMEClientUsesSameInstallationForCommandsAndRenewal(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root/.acme.sh")
	local := filepath.Join(dir, "usr/local's/acme.sh")
	if err := os.MkdirAll(local, 0755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "commands")
	script := "#!/bin/sh\nprintf '%s\\n' \"$0\" \"$@\" > " + shellQuote(log) + "\nprintf 'finished\\n'\n"
	if err := os.WriteFile(filepath.Join(local, "acme.sh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	client, err := findACMEClient([]string{root, local}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	// A new higher-priority client must not change an in-flight operation.
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "acme.sh"), []byte("#!/bin/sh\nexit 99\n"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"--register-account", "--issue", "--install-cert"} {
		args := append(client.baseArgs("letsencrypt"), action)
		var output []string
		if err := client.stream(context.Background(), args, func(line string) { output = append(output, line) }); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(log)
		want := []string{filepath.Join(local, "acme.sh"), "--home", local, "--cert-home", filepath.Join(local, "gopanel-certs"), "--server", "letsencrypt", action}
		if err != nil || !reflect.DeepEqual(strings.Split(strings.TrimSpace(string(data)), "\n"), want) || !reflect.DeepEqual(output, []string{"finished"}) {
			t.Fatal(string(data), output, err)
		}
	}
	cron := client.renewalCron()
	_, command, ok := strings.Cut(cron, "17 3 * * * root ")
	if !ok {
		t.Fatal(cron)
	}
	if output, err := exec.Command("/bin/sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	data, err := os.ReadFile(log)
	want := []string{filepath.Join(local, "acme.sh"), "--cron", "--home", local, "--cert-home", filepath.Join(local, "gopanel-certs")}
	if err != nil || !reflect.DeepEqual(strings.Split(strings.TrimSpace(string(data)), "\n"), want) {
		t.Fatal(string(data), err)
	}
	// Recheck trust at every invocation, rather than only during discovery.
	client.trust = func(string) error { return errors.New("permissions changed") }
	if err := client.stream(context.Background(), []string{"--issue"}, func(string) { t.Fatal("unsafe client executed") }); err == nil {
		t.Fatal("executed client after trust changed")
	}
}

func TestForcedRenewalUsesExplicitCAAndIsolatedHome(t *testing.T) {
	home := t.TempDir()
	log := filepath.Join(home, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(log) + "\nprintf 'signed order\\n'\n"
	if err := os.WriteFile(filepath.Join(home, "acme.sh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"zerossl", "letsencrypt"} {
		client := acmeClient{home: home, certDirectory: filepath.Join(home, "isolated-attempt"), trust: func(string) error { return nil }}
		r := vhosts.CreateRequest{Domain: "example.com", Aliases: []string{"www.example.com"}, SSL: vhosts.SSLInput{Mode: provider}}
		var lines []string
		if err := client.issue(context.Background(), r, true, func(line string) { lines = append(lines, line) }); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(strings.TrimSpace(string(data)), "\n")
		want := []string{"--home", home, "--cert-home", client.certDirectory, "--server", provider, "--issue", "--webroot", vhosts.ChallengeDirectory, "--keylength", "ec-256", "-d", "example.com", "-d", "www.example.com", "--force"}
		if !reflect.DeepEqual(args, want) || len(lines) != 1 {
			t.Fatal(args, lines)
		}
		if !strings.Contains(client.renewalCron(), shellQuote(client.certDirectory)) {
			t.Fatal("cron lost isolated cert home")
		}
	}
}
