//go:build linux

package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func TestBackupPolicyCopyReencryptsAndChecksRevision(t *testing.T) {
	p := backupTestInstaller(t)
	s := backupPolicyStore{state: p.state}
	r := sampleBackupPolicy()
	r.PasswordAction = "replace"
	r.Password = "private-archive-password"
	source, err := s.save(r)
	if err != nil {
		t.Fatal(err)
	}
	copyRequest := backups.SavePolicyRequest{Policy: source, PasswordAction: "keep", CopyFrom: &backups.PolicyIdentity{ID: source.ID, Revision: source.Revision}}
	copyRequest.ID = ""
	copyRequest.Revision = 0
	copyRequest.Enabled = false
	copyRequest.Name = "停用副本"
	clone, err := s.save(copyRequest)
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID == source.ID || clone.Enabled || clone.Revision != 1 || !clone.Compression.HasPassword {
		t.Fatal(clone)
	}
	records, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Ciphertext == records[1].Ciphertext {
		t.Fatal("ciphertext was copied without re-encryption")
	}
	aead, err := s.aead()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := base64.StdEncoding.DecodeString(records[1].Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aead.Open(nil, encoded[:aead.NonceSize()], encoded[aead.NonceSize():], []byte(clone.ID))
	if err != nil || string(plain) != r.Password {
		t.Fatal("clone password cannot be decrypted")
	}
	if _, err = aead.Open(nil, encoded[:aead.NonceSize()], encoded[aead.NonceSize():], []byte(source.ID)); err == nil {
		t.Fatal("clone ciphertext is not bound to the new ID")
	}
	for _, action := range []string{"clear", "replace"} {
		q := copyRequest
		q.PasswordAction = action
		if action == "replace" {
			q.Password = "replacement"
		}
		got, err := s.save(q)
		if err != nil || got.Compression.HasPassword != (action == "replace") {
			t.Fatal(action, got, err)
		}
	}
	r.Policy = source
	r.PasswordAction = "keep"
	r.Password = ""
	r.Notes = "source changed"
	updated, err := s.save(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.save(copyRequest); !errors.Is(err, backups.ErrConflict) {
		t.Fatal("stale copy accepted", err)
	}
	if err = s.delete(backups.PolicyIdentity{ID: updated.ID, Revision: updated.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.save(copyRequest); !errors.Is(err, backups.ErrNotFound) {
		t.Fatal("deleted source accepted", err)
	}
	encodedResponse, _ := json.Marshal(clone)
	if strings.Contains(string(encodedResponse), "private-archive-password") || strings.Contains(string(encodedResponse), "ciphertext") {
		t.Fatal("secret leaked")
	}
}

func TestBackupPolicyDestinationsDraftsAndLegacy(t *testing.T) {
	p := backupTestInstaller(t)
	s := backupPolicyStore{state: p.state, accountHome: p.accountHome}
	r := sampleBackupPolicy()
	r.Destinations = nil
	if _, err := s.save(r); !errors.Is(err, backups.ErrInvalid) {
		t.Fatal("enabled destinationless policy accepted", err)
	}
	r.Enabled = false
	draft, err := s.save(r)
	if err != nil || draft.Destinations == nil || len(draft.Destinations) != 0 {
		t.Fatal(draft, err)
	}
	// Simulate an old record on disk, without rewriting it during reads.
	legacy, err := json.Marshal([]map[string]any{{"id": draft.ID, "name": "old", "enabled": true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(p.state, "policies.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := s.get(draft.ID)
	if err != nil || !got.Enabled || got.Destinations == nil {
		t.Fatal(got, err)
	}
	unchanged, _ := os.ReadFile(filepath.Join(p.state, "policies.json"))
	if string(unchanged) != string(legacy) {
		t.Fatal("legacy records rewritten during GET")
	}
	// Test account availability using isolated root-owned configuration.
	writeAccountFixture(t, p.accountHome, ".config/gdrive3/account.json", `{"current":"a@example.com"}`)
	writeAccountFixture(t, p.accountHome, ".config/gdrive3/a@example.com/tokens.json", "private")
	writeAccountFixture(t, p.accountHome, ".config/rclone/rclone.conf", "[archive]\ntype=s3\ntoken=private")
	r = sampleBackupPolicy()
	r.Destinations = []backups.Destination{{Type: "gdrive", Account: "a@example.com", FolderID: "root"}, {Type: "rclone", Remote: "archive", Path: "backups/web"}}
	if _, err = s.save(r); err != nil {
		t.Fatal(err)
	}
	r.Destinations[0].Account = "missing@example.com"
	if _, err = s.save(r); !errors.Is(err, backups.ErrInvalid) {
		t.Fatal("missing enabled account accepted", err)
	}
	r.Enabled = false
	if _, err = s.save(r); err != nil {
		t.Fatal("missing account must remain editable as a disabled policy", err)
	}
}

func TestBackupPathsOverlapAliasesAndUncreatedDestinations(t *testing.T) {
	root := backupTestInstaller(t).state
	site := filepath.Join(root, "site")
	child := filepath.Join(site, "public")
	sibling := filepath.Join(root, "site-other")
	for _, d := range []string{child, sibling} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(site, alias); err != nil {
		t.Fatal(err)
	}
	request := backups.PathsRequest{Files: []backups.FileSource{{Path: site}, {Path: child}, {Path: sibling}}, Destinations: []backups.Destination{{Type: "local", Path: filepath.Join(root, "new", "backup")}}}
	got, err := inspectBackupPaths(request)
	if err != nil || len(got.Errors) != 0 || len(got.Warnings) != 1 {
		t.Fatal(got, err)
	}
	if _, err = os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
		t.Fatal("path validation created directories")
	}
	for _, target := range []string{site, filepath.Join(alias, "new", "backup"), "/proc/new-backup"} {
		request.Destinations = []backups.Destination{{Type: "local", Path: target}}
		got, err = inspectBackupPaths(request)
		if err != nil || len(got.Errors) == 0 {
			t.Fatal(target, got, err)
		}
	}
	request.Files = []backups.FileSource{{Path: site}, {Path: alias}}
	request.Destinations = nil
	got, err = inspectBackupPaths(request)
	if err != nil || len(got.Errors) == 0 {
		t.Fatal("source alias duplication accepted", got, err)
	}
	outside := filepath.Join(root, "out")
	if err = os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	outsideAlias := filepath.Join(root, "out-alias")
	if err = os.Symlink(outside, outsideAlias); err != nil {
		t.Fatal(err)
	}
	request.Files = nil
	request.Destinations = []backups.Destination{{Type: "local", Path: outside}, {Type: "local", Path: outsideAlias}}
	got, err = inspectBackupPaths(request)
	if err != nil || len(got.Errors) == 0 {
		t.Fatal("aliased targets accepted", got, err)
	}
	file := filepath.Join(root, "file")
	if err = os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(root, "dangling")
	if err = os.Symlink(filepath.Join(root, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{file, filepath.Join(dangling, "sub")} {
		request.Destinations = []backups.Destination{{Type: "local", Path: target}}
		got, err = inspectBackupPaths(request)
		if err != nil || len(got.Errors) == 0 {
			t.Fatal(target, got, err)
		}
	}
}

func TestBackupRulePreviewCountsInheritanceAndPartialResults(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cache", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if err := os.WriteFile(filepath.Join(root, "cache", "nested", fmt.Sprintf("%d.log", i)), []byte("123"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "keep.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	source := backups.FileSource{Path: root, Excludes: []string{"cache/", "**/*.log", "none/**"}}
	got, err := inspectBackupFile(context.Background(), source)
	if err != nil || !got.Complete || got.OriginalBytes != 29 || got.IncludedBytes != 5 {
		t.Fatal(got, err)
	}
	if len(got.Rules) != 3 || got.Rules[0].Count != 8 || got.Rules[1].Count != 8 || got.Rules[2].Count != 0 || len(got.Rules[0].Samples) != 5 || !got.Rules[0].Truncated {
		t.Fatal(got.Rules)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = inspectBackupFile(ctx, source)
	if err != nil || got.Complete || got.Error == "" {
		t.Fatal("cancellation must yield partial results", got, err)
	}
}

func TestBackupRulePreviewSampleBudget(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, strings.Repeat("a", 180), strings.Repeat("b", 180))
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(nested, fmt.Sprintf("%d.txt", i)), []byte("one"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	patterns := make([]string, 200)
	for i := range patterns {
		patterns[i] = "**"
	}
	got, err := inspectBackupFile(context.Background(), backups.FileSource{Path: root, Excludes: patterns})
	if err != nil || !got.Complete || got.IncludedBytes != 0 {
		t.Fatal(err)
	}
	bytesUsed := 0
	truncated := false
	for _, rule := range got.Rules {
		if rule.Count != 5 {
			t.Fatal("sample budget must not stop counting")
		}
		for _, sample := range rule.Samples {
			encoded, _ := json.Marshal(sample)
			bytesUsed += len(encoded)
		}
		truncated = truncated || rule.Truncated
	}
	if bytesUsed > 128*1024 || !truncated {
		t.Fatal("sample budget not enforced", bytesUsed)
	}
	encoded, _ := json.Marshal(got)
	if len(encoded) > 1024*1024 {
		t.Fatal("oversized helper response")
	}
}
