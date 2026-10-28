package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func TestFormatVhostSavesSafelyWithoutReloading(t *testing.T) {
	for _, scenario := range []string{"normal", "disabled", "auth", "managed", "state failure", "check failure", "stale", "concurrent edit", "syntax", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			p, create, locked := provisionFixture(t)
			if err := create.Validate(); err != nil {
				t.Fatal(err)
			}
			name := create.Name
			path := filepath.Join(p.store.Directory, name)
			original := "# keep\nserver { listen 80; location / { return 200 \"literal; # {}\"; } }\n"
			base := original
			credential := ""
			if scenario == "auth" {
				credential = filepath.Join(p.store.BasicAuthDirectory(), vhosts.BasicAuthKey(name)+"-"+strings.Repeat("a", 32)+".htpasswd")
				var err error
				original, err = p.store.AttachBasicAuth(name, original, credential)
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "syntax" {
				original = "server {"
			}
			if scenario == "disabled" {
				path += ".disabled"
			}
			if err := os.WriteFile(path, []byte(original), 0640); err != nil {
				t.Fatal(err)
			}
			view, err := p.store.Read(name)
			if err != nil {
				t.Fatal(err)
			}
			input := vhosts.FormatInput{Name: name, Revision: view.Revision}
			if scenario == "managed" || scenario == "state failure" {
				if err := p.saveManaged(managedSite{Request: create, Hash: configHash([]byte(base))}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "state failure" {
				p.trust = func(candidate string) error {
					if candidate == p.stateDir {
						return errors.New("cannot update managed state")
					}
					return nil
				}
			}
			if scenario == "stale" {
				input.Revision = strings.Repeat("0", 64)
			}
			if scenario == "symlink" {
				external := filepath.Join(p.store.Prefix, "outside.conf")
				os.Rename(path, external)
				os.Symlink(external, path)
			}
			if scenario == "concurrent edit" {
				p.trust = func(candidate string) error {
					if candidate == path {
						original += "# changed externally\n"
						return os.WriteFile(path, []byte(original), 0640)
					}
					return nil
				}
			}
			checks := 0
			p.run = func(_ context.Context, _ string, args ...string) (string, error) {
				if !*locked || len(args) != 1 || args[0] != "-t" {
					t.Fatal("format must check under the lock without reloading", args)
				}
				checks++
				if scenario == "check failure" {
					return "", errors.New("invalid")
				}
				return "", nil
			}
			result, err := p.format(context.Background(), input, "/trusted/nginx")
			success := scenario == "normal" || scenario == "disabled" || scenario == "auth" || scenario == "managed"
			current, _ := os.ReadFile(path)
			if !success {
				if err == nil || string(current) != original {
					t.Fatal("failed formatting modified the file", result, err)
				}
				return
			}
			if err != nil || !result.Changed || checks != 1 || result.Content != string(current) || result.Revision == input.Revision {
				t.Fatal(result, err)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0640 || filepath.Base(path) != result.Filename {
				t.Fatal("permissions or enabled state changed")
			}
			newBase, auth, err := p.store.StripBasicAuth(name, result.Content)
			if err != nil || auth != credential {
				t.Fatal("format broke authentication management", err)
			}
			if scenario == "managed" {
				state, err := p.loadManaged(name)
				if err != nil || state.Hash != configHash([]byte(newBase)) {
					t.Fatal("format broke managed site ownership", err)
				}
			}
			input.Revision = result.Revision
			again, err := p.format(context.Background(), input, "/trusted/nginx")
			if err != nil || again.Changed || checks != 1 {
				t.Fatal("repeated formatting rewrote or checked the file", again, err)
			}
		})
	}
}
