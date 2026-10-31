package backups

import (
	"strings"
	"testing"
	"time"
)

func TestPolicyValidationAndSchedule(t *testing.T) {
	p := SavePolicyRequest{Policy: Policy{Name: "政策", Schedule: "0 2 * * *", Compression: Compression{Format: "zip", Level: 5}, Retention: Retention{Databases: 7, Files: 7}, Databases: DatabaseSelection{Enabled: true, Mode: "all"}}, PasswordAction: "keep"}
	if err := ValidatePolicy(p); err != nil {
		t.Fatal(err)
	}
	for _, modify := range []func(*SavePolicyRequest){func(r *SavePolicyRequest) { r.Name = "" }, func(r *SavePolicyRequest) { r.Schedule = "@reboot" }, func(r *SavePolicyRequest) { r.Schedule = "* * * * * *" }, func(r *SavePolicyRequest) { r.Retention.Files = 0 }, func(r *SavePolicyRequest) { r.Compression.Format = "tar.gz" }, func(r *SavePolicyRequest) { r.Compression.Format = "7z"; r.Compression.Level = 2 }, func(r *SavePolicyRequest) { r.Password = "leak" }, func(r *SavePolicyRequest) { r.Databases.Mode = "selected" }, func(r *SavePolicyRequest) { r.Databases.Rules = []TableRule{{Database: "app", Mode: "include"}} }} {
		bad := p
		modify(&bad)
		if ValidatePolicy(bad) == nil {
			t.Fatal("invalid policy accepted", bad)
		}
	}
	preview, err := SchedulePreview("*/15 * * * *", time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC))
	if err != nil || len(preview.Times) != 5 {
		t.Fatal(preview, err)
	}
	if _, err := SchedulePreview("0 2 * * 7", time.Now()); err != nil {
		t.Fatal("Unix Sunday rejected", err)
	}
	for _, v := range []string{"61 * * * *", "0 0 30 2 *", "CRON_TZ=UTC * * * * *"} {
		if _, err := SchedulePreview(v, time.Now()); err == nil {
			t.Fatal(v)
		}
	}
	if ValidateFileSource(FileSource{Path: "/srv", Excludes: []string{"cache/", "**/node_modules/**"}}) != nil {
		t.Fatal("glob rejected")
	}
	if ValidateNames([]string{"a", "a"}) == nil || ValidateNames([]string{strings.Repeat("字", 65)}) == nil {
		t.Fatal("bad names accepted")
	}
}
