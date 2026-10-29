package metrics

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestWidgetsPersistAndOnlyApplyAfterSave(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := OpenWidgetStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Get().Enabled) != 0 {
		t.Fatal("new installations must keep optional widgets hidden")
	}
	var applied []string
	apply := func(ids []string) { applied = ids }
	for _, ids := range [][]string{{"database", "load"}, {}} {
		if err = store.Save(Widgets{Enabled: ids}, apply); err != nil {
			t.Fatal(err)
		}
		restored, e := OpenWidgetStore(dir)
		if e != nil || !reflect.DeepEqual(restored.Get(), store.Get()) || !reflect.DeepEqual(applied, store.Get().Enabled) {
			t.Fatal("state or collector selection not restored", e)
		}
	}
	info, _ := os.Stat(filepath.Join(dir, "dashboard-widgets.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("state is not private")
	}
	for _, ids := range [][]string{nil, {"load", "load"}, {"cpu"}} {
		if store.Save(Widgets{Enabled: ids}, apply) == nil || len(store.Get().Enabled) != 0 {
			t.Fatal("invalid selection accepted")
		}
	}
	if err = os.Remove(filepath.Join(dir, "dashboard-widgets.json")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "dashboard-widgets.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if store.Save(Widgets{Enabled: []string{"load"}}, apply) == nil || len(store.Get().Enabled) != 0 || len(applied) != 0 {
		t.Fatal("failed write changed effective state")
	}
}
func TestWidgetsRejectDamagedAndLinkedState(t *testing.T) {
	for _, data := range []string{`null`, `{}`, `{"enabled":null}`, `{"enabled":["load","load"]}`, `{"enabled":["exec"]}`, `{"enabled":[]} {}`, `{"enabled":[],"unknown":1}`, `{"enabled":[]}` + strings.Repeat(" ", 4097)} {
		dir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "dashboard-widgets.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenWidgetStore(dir); err == nil {
			t.Fatalf("accepted %s", data)
		}
		actual, _ := os.ReadFile(path)
		if string(actual) != data {
			t.Fatal("damaged file overwritten")
		}
	}
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(outside, []byte(`{"enabled":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "dashboard-widgets.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWidgetStore(dir); err == nil {
		t.Fatal("linked state accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWidgetStore(link); err == nil {
		t.Fatal("linked directory accepted")
	}
}
func TestWidgetConcurrentSavesApplyInFileOrder(t *testing.T) {
	store, err := OpenWidgetStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	var applied []string
	var wg sync.WaitGroup
	for _, id := range WidgetIDs {
		wg.Go(func() {
			if e := store.Save(Widgets{Enabled: []string{id}}, func(ids []string) { applied = ids }); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	restored, err := OpenWidgetStore(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(applied, restored.Get().Enabled) {
		t.Fatal("collector state disagrees with last persisted write")
	}
}
