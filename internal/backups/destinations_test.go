package backups

import "testing"

func TestDestinationValidation(t *testing.T) {
	valid := []Destination{{Type: "local", Path: "/var/backups"}, {Type: "gdrive", Account: "backup@example.com", FolderID: "root"}, {Type: "rclone", Remote: "archive", Path: "web/backups"}}
	if err := ValidateDestinations(valid, true); err != nil {
		t.Fatal(err)
	}
	if ValidateDestinations(nil, true) == nil || ValidateDestinations(nil, false) != nil {
		t.Fatal("draft destination requirement")
	}
	for _, bad := range []Destination{{Type: "local", Path: "relative"}, {Type: "local", Path: "/a/../b"}, {Type: "local", Path: "/backup", Account: "unexpected"}, {Type: "gdrive", Account: "a", FolderID: "https://example.com"}, {Type: "rclone", Remote: "a", Path: "../b"}, {Type: "rclone", Remote: "a", Path: "other:path"}, {Type: "unknown"}} {
		if ValidateDestination(bad) == nil {
			t.Fatal("invalid destination accepted", bad)
		}
	}
	if ValidateDestinations(append(valid, valid[0]), true) == nil {
		t.Fatal("duplicate target accepted")
	}
	if ValidateDestinations([]Destination{{Type: "rclone", Remote: "a", Path: ""}, {Type: "rclone", Remote: "a", Path: "."}}, true) == nil {
		t.Fatal("root alias accepted")
	}
	if ValidateDestinations([]Destination{{Type: "rclone", Remote: "a", Path: "web"}, {Type: "rclone", Remote: "a", Path: "/web"}}, true) != nil {
		t.Fatal("distinct absolute and relative paths were merged")
	}
}
