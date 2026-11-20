package cron

import (
	"errors"
	"strings"
	"testing"
)

func TestTogglePreservesCrontabAndRejectsStaleRevision(t *testing.T) {
	raw := "# backup\r\nSHELL=/bin/bash\r\n\t0 2 * * *\t/root/backup  %data # literal  \r\n# @daily /root/disabled\r\n## @hourly /root/comment\r\n# invalid task\r\n"
	view := Parse(raw)
	if len(view.Tasks) != 2 || len(view.Warnings) != 0 || !view.Tasks[0].Enabled || view.Tasks[1].Enabled {
		t.Fatalf("unexpected tasks: %+v", view)
	}
	enabled := false
	request := ToggleRequest{Line: 3, Revision: view.Revision, Enabled: &enabled}
	disabled, err := Toggle(view, request)
	if err != nil || disabled != strings.Replace(raw, "\t0 2", "\t#0 2", 1) {
		t.Fatalf("disabled=%q err=%v", disabled, err)
	}
	updated := Parse(disabled)
	if len(updated.Tasks) != 2 || updated.Tasks[0].Enabled {
		t.Fatalf("disabled task lost: %+v", updated)
	}
	if _, err := Toggle(updated, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	request.Revision = updated.Revision
	if same, err := Toggle(updated, request); err != nil || same != disabled {
		t.Fatal("repeated disabling must not add another comment marker")
	}
	enabled = true
	restored, err := Toggle(updated, request)
	if err != nil || restored != raw {
		t.Fatalf("restore changed content: %q %v", restored, err)
	}
	request.Line = 4
	request.Revision = view.Revision
	if restored, err := Toggle(view, request); err != nil || restored != strings.Replace(raw, "# @daily", " @daily", 1) {
		t.Fatalf("cannot enable existing comment: %q %v", restored, err)
	}
	for _, line := range []int{1, 2, 5, 6, 99} {
		request.Line = line
		if _, err := Toggle(view, request); !errors.Is(err, ErrConflict) {
			t.Fatalf("non-task line %d accepted: %v", line, err)
		}
	}
	request.Enabled = nil
	if _, err := Toggle(view, request); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing enabled state accepted")
	}
}

func TestParse(t *testing.T) {
	raw := "# backup\n\nSHELL=/bin/bash\n MAILTO = \"root@example.com\"\n\t*/5\t0-23 * JAN,MAR MON-FRI\t/usr/local/bin/backup  'with spaces' %data # literal  \n@reboot /bin/start\n# @daily disabled\n@daily\nunknown entry\n@unknown echo nope\n"
	view := Parse(raw)
	if view.Raw != raw || view.UpdatedAt.IsZero() || len(view.Tasks) != 3 || len(view.Warnings) != 3 {
		t.Fatalf("unexpected parse: %+v", view)
	}
	if task := view.Tasks[0]; task.Line != 5 || task.Schedule != "*/5 0-23 * JAN,MAR MON-FRI" || task.Command != "/usr/local/bin/backup  'with spaces' %data # literal  " {
		t.Fatalf("command not preserved: %+v", task)
	}
	if task := view.Tasks[1]; task.Line != 6 || task.Schedule != "@reboot" || task.Command != "/bin/start" || !task.Enabled {
		t.Fatal(task)
	}
	for i, line := range []string{"8", "9", "10"} {
		if !strings.Contains(view.Warnings[i], "第 "+line+" 行") {
			t.Fatal(view.Warnings)
		}
	}
}

func TestEmptyAndEnvironment(t *testing.T) {
	for _, raw := range []string{"", "\n\t\n", "# comment\nPATH=/usr/bin\nCRON_TZ=Asia/Shanghai\n# ordinary comment"} {
		view := Parse(raw)
		if view.Tasks == nil || view.Warnings == nil || len(view.Tasks) != 0 || len(view.Warnings) != 0 || view.Raw != raw {
			t.Fatalf("unexpected empty parse: %+v", view)
		}
	}
}

func TestSchedulesAndLineEndings(t *testing.T) {
	for _, schedule := range []string{"* * * * *", "0 1 * * 0,6", "0 0 1 */2 *", "@reboot", "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly"} {
		view := Parse(schedule + "\tprintf 'hello\\n'\r\n")
		if len(view.Tasks) != 1 || len(view.Warnings) != 0 || view.Tasks[0].Command != "printf 'hello\\n'" {
			t.Fatalf("%s: %+v", schedule, view)
		}
	}
}
