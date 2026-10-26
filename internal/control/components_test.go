package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestComponentVersionParsing(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{"python3", "Python 3.13.5\n", "3.13.5"},
		{"python3", "Python 3.15.0rc2", "3.15.0rc2"},
		{"go", "go version go1.27.3 linux/amd64", "1.27.3"},
		{"go", "go version go1.28 linux/amd64", "1.28"},
		{"node", "v22.23.4", "22.23.4"},
		{"codex", "codex-cli 0.164.0", "0.164.0"},
		{"node", "npm 11.0.0", ""},
		{"codex", "codex 0.164.0", ""},
		{"sshd", "OpenSSH 9.0.0", ""},
	} {
		got, err := parseComponentVersion(tc.name, tc.output)
		if got != tc.want || (err == nil) != (tc.want != "") {
			t.Fatalf("%s: %q %v", tc.name, got, err)
		}
	}
}
func TestNVMDefaultResolution(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"v22.9.0", "v22.23.4", "v24.1.0", "v25.0.0-rc1"} {
		if err := os.MkdirAll(filepath.Join(root, "versions/node", v, "bin"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, alias := range []string{"alias", "alias/lts"} {
		if err := os.MkdirAll(filepath.Join(root, alias), 0755); err != nil {
			t.Fatal(err)
		}
	}
	trust := func(string) error { return nil }
	for _, tc := range []struct{ alias, want string }{
		{"22", "v22.23.4"}, {"v22.9.0", "v22.9.0"}, {"22.23", "v22.23.4"}, {"node", "v24.1.0"}, {"stable", "v24.1.0"}, {"lts/*", "v22.23.4"}, {"lts/jod", "v22.23.4"}, {"default", ""}, {"../../evil", ""}, {"23", ""}, {"22; touch /tmp/pwned", ""},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			for path, data := range map[string]string{"alias/default": tc.alias, "alias/lts/*": "lts/jod", "alias/lts/jod": "v22.23.4"} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := nvmDefaultBin(root, trust)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("accepted %q", tc.alias)
				}
				return
			}
			if err != nil || got != filepath.Join(root, "versions/node", tc.want, "bin") {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
	if _, err := nvmDefaultBin(root, func(string) error { return errors.New("unsafe") }); err == nil {
		t.Fatal("untrusted alias accepted")
	}
	if got, err := nvmDefaultBin(t.TempDir(), trust); got != "" || err != nil {
		t.Fatalf("missing NVM: %q %v", got, err)
	}
}
func TestNVMRejectsInvalidAliasFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alias/default"), 0755); err != nil {
		t.Fatal(err)
	}
	trust := func(string) error { return nil }
	if _, err := nvmDefaultBin(root, trust); err == nil {
		t.Fatal("directory accepted as alias")
	}
	if err := os.Remove(filepath.Join(root, "alias/default")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alias/default"), make([]byte, 257), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := nvmDefaultBin(root, trust); err == nil {
		t.Fatal("oversized alias accepted")
	}
}

func TestComponentProbeCommandsAndFailures(t *testing.T) {
	safe := func(string) error { return nil }
	for _, tc := range []struct {
		name, output string
		args         []string
	}{
		{"python3", "Python 3.13.5", []string{"--version"}},
		{"go", "go version go1.27.3 linux/amd64", []string{"version"}},
		{"node", "v22.23.4", []string{"--version"}},
		{"codex", "codex-cli 0.164.0", []string{"--version"}},
	} {
		item := probeComponent(context.Background(), tc.name, "/fixed/bin/"+tc.name, func(ctx context.Context, program string, args ...string) (string, error) {
			if program != "/fixed/bin/"+tc.name || !reflect.DeepEqual(args, tc.args) {
				t.Fatal(program, args)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("missing timeout")
			}
			return tc.output, nil
		}, safe)
		if item.Status != "ready" || item.Version == "" {
			t.Fatal(item)
		}
	}
	never := func(context.Context, string, ...string) (string, error) {
		t.Fatal("unexpected execution")
		return "", nil
	}
	if item := probeComponent(context.Background(), "node", "", never, safe); item.Status != "missing" {
		t.Fatal(item)
	}
	if item := probeComponent(context.Background(), "node", "/unsafe/node", never, func(string) error { return errors.New("unsafe") }); item.Status != "failed" {
		t.Fatal(item)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	item := probeComponent(ctx, "node", "/fixed/node", func(ctx context.Context, _ string, _ ...string) (string, error) { <-ctx.Done(); return "", ctx.Err() }, safe)
	if item.Status != "failed" || item.Error == "" {
		t.Fatal(item)
	}
}
func TestCodexRejectsUntrustedInterpreter(t *testing.T) {
	dir := t.TempDir()
	node := filepath.Join(dir, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	item := probeComponent(context.Background(), "codex", filepath.Join(dir, "codex"), func(context.Context, string, ...string) (string, error) {
		t.Fatal("executed with untrusted interpreter")
		return "", nil
	}, func(path string) error {
		if path == node {
			return errors.New("unsafe")
		}
		return nil
	})
	if item.Status != "failed" || item.Error != "Node.js 解释器权限不安全" {
		t.Fatal(item)
	}
}

func TestComponentCommandNVMInterpreter(t *testing.T) {
	dir := t.TempDir()
	// An env-based launcher must use its adjacent Node, independent of the panel PATH.
	for name, script := range map[string]string{"node": "#!/bin/sh\nprintf 'codex-cli 0.164.0\\n'\n", "codex": "#!/usr/bin/env node\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	output, err := componentCommand(context.Background(), filepath.Join(dir, "codex"), "--version")
	if err != nil || output != "codex-cli 0.164.0" {
		t.Fatal(output, err)
	}
}
