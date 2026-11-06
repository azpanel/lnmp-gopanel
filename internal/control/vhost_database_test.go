package control

import (
	"os"
	"testing"
)

func TestLocalDatabaseCredentials(t *testing.T) {
	const systemConfig = "[client]\npassword = fixture$&password\nsocket=/fixture/mysql.sock\n[mysqld]\npassword=ignored\n"
	tests := []struct {
		name      string
		system    string
		root      string
		temporary string
		want      string
		rejected  string
		readErr   error
	}{
		{name: "system credentials", system: systemConfig, want: "fixture$&password"},
		{name: "root overrides system", system: systemConfig, root: "[client]\npassword=root-fixture\n", want: "root-fixture"},
		{name: "root without password preserves system", system: systemConfig, root: "[client]\nuser=root\n", want: "fixture$&password"},
		{name: "empty root password clears system", system: systemConfig, root: "[client]\npassword=\n"},
		{name: "quoted empty root password clears system", system: systemConfig, root: "[client]\npassword=\"\"\n"},
		{name: "temporary password overrides files", system: systemConfig, root: "[client]\nuser=other\npassword=root-fixture\n", temporary: "temporary-fixture", want: "temporary-fixture"},
		{name: "temporary password overrides empty root password", system: systemConfig, root: "[client]\npassword=\n", temporary: "temporary-fixture", want: "temporary-fixture"},
		{name: "mysqld password ignored", system: "[client]\nsocket=/fixture/mysql.sock\n[mysqld]\npassword=ignored\n"},
		{name: "empty system password", system: "[client]\npassword=\n"},
		{name: "no password configured"},
		{name: "unreadable system skipped", system: systemConfig, rejected: "/etc/my.cnf", readErr: os.ErrPermission},
		{name: "rejected root preserves system", system: systemConfig, root: "[client]\npassword=ignored\n", rejected: "/root/.my.cnf", readErr: os.ErrPermission, want: "fixture$&password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{
				"/etc/my.cnf":       tt.system,
				"/etc/mysql/my.cnf": "[client]\nuser=ignored\npassword=ignored\nsocket=/fixture/fallback.sock\n",
				"/root/.my.cnf":     tt.root,
			}
			config := localDatabaseConfigWithReader(tt.temporary, func(path string, limit int64) ([]byte, error) {
				if limit != 64*1024 {
					t.Fatal("configuration read limit changed")
				}
				content, ok := files[path]
				if !ok {
					t.Fatalf("unexpected configuration path: %s", path)
				}
				if path == tt.rejected {
					return []byte(content), tt.readErr
				}
				if content == "" {
					return nil, os.ErrNotExist
				}
				return []byte(content), nil
			})
			if config.Passwd != tt.want {
				t.Fatal("unexpected password precedence")
			}
			if config.User != "root" || config.Net != "unix" || config.Addr != "/fixture/fallback.sock" {
				t.Fatal("unexpected local connection settings")
			}
		})
	}
}
