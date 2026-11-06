//go:build linux

package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func artifactPolicy(t *testing.T, e backupExecutor, d backups.Destination) (backups.Policy, backups.ArtifactQuery) {
	t.Helper()
	r := sampleBackupPolicy()
	r.Enabled = false
	r.Destinations = []backups.Destination{d}
	p, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	return p, backups.ArtifactQuery{ID: p.ID, Revision: p.Revision, DestinationIndex: &index}
}
func artifactName(id, category, date string, n int) string {
	return fmt.Sprintf("%s-%s-%s-%032x.zip", id, category, date, n)
}
func writeArtifactFixture(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestBackupArtifactsLocalLiveScopeAndValidation(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	dir := filepath.Join(e.store.state, "destination")
	p, q := artifactPolicy(t, e, backups.Destination{Type: "local", Path: dir})
	result, err := e.listArtifacts(context.Background(), q)
	if err != nil || len(result.Items) != 0 || result.Warning == "" {
		t.Fatal("missing directory", result, err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first := artifactName(p.ID, "files", "20261106T040000Z", 1)
	latest := artifactName(p.ID, "databases", "20261106T050000Z", 2)
	for _, name := range []string{first, latest, artifactName(strings.Repeat("f", 32), "files", "20261106T050000Z", 3), first + ".partial", artifactName(p.ID, "files", "20261399T050000Z", 4), "unrelated.txt"} {
		writeArtifactFixture(t, dir, name)
	}
	linkName := artifactName(p.ID, "files", "20261106T050000Z", 5)
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, linkName)); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, artifactName(p.ID, "files", "20261106T050000Z", 6))
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	writeArtifactFixture(t, nested, first)
	result, err = e.listArtifacts(context.Background(), q)
	if err != nil || len(result.Items) != 2 || result.Items[0].Name != latest || *result.Items[0].Bytes != 7 || result.Truncated {
		t.Fatal(result, err)
	}
	if err := os.Remove(filepath.Join(dir, latest)); err != nil {
		t.Fatal(err)
	}
	result, err = e.listArtifacts(context.Background(), q)
	if err != nil || len(result.Items) != 1 || result.Items[0].Name != first {
		t.Fatal("external deletion not reflected", result, err)
	}
	if _, err := os.Stat(filepath.Join(e.store.state, "artifacts")); !os.IsNotExist(err) {
		t.Fatal("query unexpectedly needs artifact index", err)
	}
	for _, bad := range []backups.ArtifactQuery{{ID: p.ID, Revision: p.Revision}, {ID: "../bad", Revision: 1, DestinationIndex: q.DestinationIndex}, {ID: p.ID, Revision: 0, DestinationIndex: q.DestinationIndex}} {
		if _, err := e.listArtifacts(context.Background(), bad); !errors.Is(err, backups.ErrInvalid) {
			t.Fatal("invalid request accepted", err)
		}
	}
	bad := q
	index := 1
	bad.DestinationIndex = &index
	if _, err := e.listArtifacts(context.Background(), bad); !errors.Is(err, backups.ErrInvalid) {
		t.Fatal("unconfigured destination accepted", err)
	}
	bad = q
	bad.Revision++
	if _, err := e.listArtifacts(context.Background(), bad); !errors.Is(err, backups.ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
}
func TestBackupArtifactsLocalLimit(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	dir := filepath.Join(e.store.state, "destination")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p, q := artifactPolicy(t, e, backups.Destination{Type: "local", Path: dir})
	for i := 0; i < 1001; i++ {
		writeArtifactFixture(t, dir, artifactName(p.ID, "files", "20261106T040000Z", i))
	}
	result, err := e.listArtifacts(context.Background(), q)
	if err != nil || len(result.Items) != 1000 || !result.Truncated || result.Warning == "" {
		t.Fatal("listing not bounded", len(result.Items), result.Truncated, err)
	}
}
func TestBackupArtifactsRcloneRealLocal(t *testing.T) {
	if backupTool("rclone") == "" {
		t.Skip("rclone unavailable")
	}
	e, _, _ := backupExecutionFixture(t)
	writeAccountFixture(t, e.accountHome(), ".config/rclone/rclone.conf", "[isolated]\ntype=local\n")
	dir := filepath.Join(e.store.state, "remote")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	p, q := artifactPolicy(t, e, backups.Destination{Type: "rclone", Remote: "isolated", Path: dir})
	name := artifactName(p.ID, "files", "20261106T040000Z", 1)
	writeArtifactFixture(t, dir, name)
	writeArtifactFixture(t, filepath.Join(dir, "nested"), artifactName(p.ID, "files", "20261106T040000Z", 2))
	writeArtifactFixture(t, dir, "unrelated.txt")
	result, err := e.listArtifacts(context.Background(), q)
	if err != nil || len(result.Items) != 1 || result.Items[0].Object != name || *result.Items[0].Bytes != 7 {
		t.Fatal(result, err)
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	result, err = e.listArtifacts(context.Background(), q)
	if err != nil || len(result.Items) != 0 {
		t.Fatal(result, err)
	}
}
func TestBackupArtifactsParsers(t *testing.T) {
	id := strings.Repeat("a", 32)
	name := artifactName(id, "files", "20261106T040000Z", 1)
	result := backups.ArtifactList{}
	data := fmt.Sprintf(`[{"Name":%q,"Path":%q,"Size":42},{"Name":%q,"Path":%q,"Size":-1,"IsDir":true},{"Name":%q,"Path":%q,"IsLink":true}]`, name, name, name, name, name, name)
	truncated, err := parseRcloneArtifacts(strings.NewReader(data), id, &result)
	if err != nil || truncated || len(result.Items) != 1 || *result.Items[0].Bytes != 42 {
		t.Fatal(result, err)
	}
	for _, bad := range []string{"", "[", "[] {}", `["secret"]`} {
		if _, err := parseRcloneArtifacts(strings.NewReader(bad), id, &backups.ArtifactList{}); err == nil {
			t.Fatal("malformed output accepted", bad)
		}
	}
	result = backups.ArtifactList{}
	row := func(object string) string { return object + "\t" + name + "\tregular\t1.2 MiB\t2026-11-06 04:00:00\n" }
	truncated, err = parseGdriveArtifacts(strings.NewReader(row("id1")+row("id2")), id, &result)
	if err != nil || truncated || len(result.Items) != 2 || result.Items[0].Bytes != nil || result.Items[0].SizeDisplay != "1.2 MiB" {
		t.Fatal(result, err)
	}
	result = backups.ArtifactList{}
	truncated, err = parseGdriveArtifacts(strings.NewReader(strings.Repeat(row("id"), 1001)), id, &result)
	if err != nil || !truncated || len(result.Items) != 1000 {
		t.Fatal("Drive limit", err)
	}
	if _, err := parseGdriveArtifacts(strings.NewReader("secret\n"), id, &backups.ArtifactList{}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("raw output leak", err)
	}
}
func TestBackupArtifactsGdriveIsolationBusyAndFailures(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	account := "fixture"
	for _, file := range []string{"tokens.json", "secret.json"} {
		writeAccountFixture(t, e.accountHome(), ".config/gdrive3/"+account+"/"+file, `{"private":"never-return"}`)
	}
	d := backups.Destination{Type: "gdrive", Account: account, FolderID: "folder_123"}
	p, q := artifactPolicy(t, e, d)
	name := artifactName(p.ID, "files", "20261106T040000Z", 1)
	calls := filepath.Join(e.store.state, "calls")
	tool := filepath.Join(e.store.state, "gdrive")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> '%s'\ncat \"$HOME/.config/gdrive3/account.json\" > '%s.account'\nif [ \"$2\" = info ]; then printf 'Mime: application/vnd.google-apps.folder\\n'; else printf 'id_123\\t%s\\tregular\\t1.2 MiB\\t2026-11-06 04:00:00\\n'; fi\n", calls, calls, name)
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	e.tools = func(string) string { return tool }
	result, err := e.listArtifacts(context.Background(), q)
	if err != nil || len(result.Items) != 1 || result.Items[0].Object != "id_123" {
		t.Fatal(result, err)
	}
	args, _ := os.ReadFile(calls)
	for _, required := range []string{"info\nfolder_123", "'folder_123' in parents", p.ID + "-", "--max\n1001", "--skip-header", "--full-name"} {
		if !strings.Contains(string(args), required) {
			t.Fatal("query scope missing", required)
		}
	}
	selected, _ := os.ReadFile(calls + ".account")
	if !strings.Contains(string(selected), account) {
		t.Fatal("account isolation missing")
	}
	entries, err := os.ReadDir(filepath.Join(e.store.state, "queries"))
	if err != nil || len(entries) != 0 {
		t.Fatal("query workspace leaked", err)
	}
	if _, err := os.Stat(filepath.Join(e.store.state, "run-index.json")); !os.IsNotExist(err) {
		t.Fatal("view created an execution", err)
	}
	unlock, err := e.lockGdriveAccount(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.listArtifacts(context.Background(), q)
	unlock()
	if err == nil || !strings.Contains(err.Error(), "正忙") {
		t.Fatal("busy account not reported", err)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho never-return >&2\nexit 3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.listArtifacts(context.Background(), q); err == nil || strings.Contains(err.Error(), "never-return") {
		t.Fatal("authorization failure hidden or leaked", err)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := e.listArtifacts(ctx, q); err == nil {
		t.Fatal("cancelled query succeeded")
	}
	entries, err = os.ReadDir(filepath.Join(e.store.state, "queries"))
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled query leaked workspace", err)
	}
	lockCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	unlock, err = e.lockGdriveAccount(lockCtx, account)
	if err != nil {
		t.Fatal("cancelled query retained account lock", err)
	}
	unlock()
}
func TestBackupArtifactCommandLimitsCancellationAndFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runBackupArtifactCommand(ctx, "/bin/sh", backupCommandEnvironment(), []string{"-c", "exec sleep 10"}, func(reader io.Reader) (bool, error) { _, err := io.Copy(io.Discard, reader); return false, err })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout ignored", err)
	}
	truncated, err := runBackupArtifactCommand(context.Background(), "/bin/sh", backupCommandEnvironment(), []string{"-c", "head -c 9000000 /dev/zero"}, func(reader io.Reader) (bool, error) { _, err := io.Copy(io.Discard, reader); return false, err })
	if err != nil || !truncated {
		t.Fatal("output not bounded", truncated, err)
	}
	_, err = runBackupArtifactCommand(context.Background(), "/bin/sh", backupCommandEnvironment(), []string{"-c", "echo private-credential >&2; exit 4"}, func(reader io.Reader) (bool, error) { _, err := io.Copy(io.Discard, reader); return false, err })
	if err == nil || strings.Contains(err.Error(), "private-credential") {
		t.Fatal("failure treated as empty list", err)
	}
}
