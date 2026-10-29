package lnmpinstall

import (
	"fmt"
	"strings"
	"testing"
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
	main := array("DB_Info", 13) + array("PHP_Info", 16) + `DBSelect="11"
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
	if len(o.Databases) != 14 || len(o.PHP) != 16 || o.DefaultDatabase != 11 || o.DefaultPHP != 14 || o.DefaultAllocator != 1 {
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
