package lnmpinstall

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	array := func(name string, n int) string {
		v := []string{}
		for i := 1; i <= n; i++ {
			v = append(v, fmt.Sprintf("'Version %d'", i))
		}
		return name + "=(" + strings.Join(v, " ") + ")\n"
	}
	php := []string{"5.2.17", "5.3.29", "5.4.45", "5.5.38", "5.6.40", "7.0.33", "7.1.33", "7.2.34", "7.3.33", "7.4.33", "8.0.30", "8.1.34", "8.2.35", "8.3.36", "8.4.27", "8.5.12"}
	for i := range php {
		php[i] = "'PHP " + php[i] + "'"
	}
	main := array("DB_Info", 13) + "PHP_Info=(" + strings.Join(php, " ") + ")\n" + `DBSelect="11"
PHPSelect="14"
SelectMalloc="1"
Database_Selection PHP_Selection MemoryAllocator_Selection Default_Security_Selection LNMP_Auto DB_Root_Password InstallInnodb`
	main += "\nDispaly_Selection()\n{\nDatabase_Selection\nPHP_Selection\nMemoryAllocator_Selection\nDefault_Security_Selection || exit 1\n}\n"
	o, err := ParseOptions(main, "Dispaly_Selection LNMP_Stack", "Default_Security_Selection()\n{\nPhpMyAdmin_Auth_Password Default_Server_IP\n}\nPrepare_Default_Security()", strings.Repeat("a", 40), "amd64", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func validRequest(t *testing.T) (Request, Options) {
	o := testOptions(t)
	return Request{ID: "11111111-1111-4111-8111-111111111111", Revision: o.Revision, Database: 11, Binary: true, DatabasePassword: "Good-Pass#2026", InnoDB: true, PHP: 14, Allocator: 1, Protection: "random", IPv4: o.IPv4, Modules: []string{"vts", "geoip2", "fancyindex", "substitutions_filter"}}, o
}
func TestCatalogAndValidation(t *testing.T) {
	r, o := validRequest(t)
	if len(o.Databases) != 14 || len(o.PHP) != 4 || o.DefaultDatabase != 11 || o.DefaultPHP != 14 || o.DefaultAllocator != 1 {
		t.Fatalf("wrong catalog: %+v", o)
	}
	if err := r.Validate(o); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Request){
		"unknown module":           func(r *Request) { r.Modules = []string{"shell"} },
		"duplicate":                func(r *Request) { r.Modules = []string{"geoip2", "geoip2"} },
		"missing selection":        func(r *Request) { r.Modules = nil },
		"revision drift":           func(r *Request) { r.Revision = strings.Repeat("b", 40) },
		"path traversal":           func(r *Request) { r.ID = "../../root" },
		"SQL quote":                func(r *Request) { r.DatabasePassword = "Good-Pass'2026" },
		"config newline":           func(r *Request) { r.DatabasePassword = "Good-Pass\n2026" },
		"invalid PHP":              func(r *Request) { r.PHP = 99 },
		"PHP52 without db":         func(r *Request) { r.PHP = 1; r.Database = 0; r.Binary = false },
		"basic missing credential": func(r *Request) { r.Protection = "basic" },
		"invalid ip":               func(r *Request) { r.IPv4 = "0.0.0.0" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := r
			mutate(&copy)
			if copy.Validate(o) == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	r.Modules = []string{}
	r.Database = 0
	r.Binary = false
	r.DatabasePassword = ""
	if err := r.Validate(o); err != nil {
		t.Fatal(err)
	}
	r.Protection = "basic"
	r.AuthUser = "admin"
	r.AuthPassword = "test-Auth-2026"
	if err := r.Validate(o); err != nil {
		t.Fatal(err)
	}
}
func TestBinaryArchitecture(t *testing.T) {
	for _, c := range []struct {
		db   int
		arch string
		want bool
	}{{11, "amd64", true}, {1, "amd64", false}, {9, "386", false}, {8, "386", true}, {11, "arm64", true}, {4, "arm64", false}, {5, "arm64", true}, {11, "arm", false}} {
		if got := BinaryAllowed(c.db, c.arch); got != c.want {
			t.Fatalf("%+v: %v", c, got)
		}
	}
}
func TestCatalogRejectsUnknownProtocol(t *testing.T) {
	if _, err := ParseOptions("DB_Info=($(evil))", "", "", strings.Repeat("a", 40), "amd64", ""); err == nil {
		t.Fatal("executable shell accepted")
	}
}

func TestMinimumPHPAndPersistedCatalog(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
	}{
		{"PHP 7.4.33", false}, {"PHP 8.0.99", false}, {"PHP 8.1.33", false},
		{"PHP 8.1.34", false}, {"PHP 8.1.35", true}, {"PHP 8.2.0", true}, {"PHP 8.10.0", true},
		{"PHP 8.5.0RC1", false}, {"PHP 8.3", false}, {"Version 14", false},
	} {
		if SupportedPHP(tc.name) != tc.allowed {
			t.Fatal(tc)
		}
	}
	r, o := validRequest(t)
	o.PHP = append(o.PHP, Choice{12, "PHP 8.1.34"})
	r.PHP = 12
	if r.Validate(o) == nil {
		t.Fatal("persisted catalog bypassed minimum PHP")
	}
	o.DefaultPHP = 12
	if err := RestrictPHP(&o); err != nil || o.DefaultPHP != 16 || len(o.PHP) != 4 || o.PHP[0].ID != 13 {
		t.Fatal(o, err)
	}
	o.PHP = []Choice{{1, "PHP 5.2.17"}}
	if RestrictPHP(&o) == nil {
		t.Fatal("empty supported catalog accepted")
	}
}

func TestJobStartTimeProtocolCompatibility(t *testing.T) {
	var legacy Job
	if err := json.Unmarshal([]byte(`{"id":"old","status":"succeeded","created_at":"2026-10-29T08:00:00Z","updated_at":"2026-10-29T10:10:00Z"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.StartedAt != nil {
		t.Fatal("legacy job acquired a guessed start time")
	}
	data, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(data), "started_at") {
		t.Fatal(string(data), err)
	}
	startedAt := time.Date(2026, 10, 29, 10, 0, 0, 123000000, time.UTC)
	legacy.StartedAt = &startedAt
	data, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var restored Job
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.StartedAt == nil || !restored.StartedAt.Equal(startedAt) {
		t.Fatal("start time did not survive JSON round trip", string(data))
	}
}
