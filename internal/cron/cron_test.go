package cron

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	raw := "# backup\n\nSHELL=/bin/bash\n MAILTO = \"root@example.com\"\n\t*/5\t0-23 * JAN,MAR MON-FRI\t/usr/local/bin/backup  'with spaces' %data # literal  \n@reboot /bin/start\n# @daily disabled\n@daily\nunknown entry\n@unknown echo nope\n"
	view := Parse(raw)
	if view.Raw != raw || view.UpdatedAt.IsZero() || len(view.Tasks) != 2 || len(view.Warnings) != 3 {
		t.Fatalf("unexpected parse: %+v", view)
	}
	if task := view.Tasks[0]; task.Line != 5 || task.Schedule != "*/5 0-23 * JAN,MAR MON-FRI" || task.Command != "/usr/local/bin/backup  'with spaces' %data # literal  " {
		t.Fatalf("command not preserved: %+v", task)
	}
	if task := view.Tasks[1]; task.Line != 6 || task.Schedule != "@reboot" || task.Command != "/bin/start" {
		t.Fatal(task)
	}
	for i, line := range []string{"8", "9", "10"} {
		if !strings.Contains(view.Warnings[i], "第 "+line+" 行") {
			t.Fatal(view.Warnings)
		}
	}
}

func TestEmptyAndEnvironment(t *testing.T) {
	for _, raw := range []string{"", "\n\t\n", "# comment\nPATH=/usr/bin\nCRON_TZ=Asia/Shanghai\n# * * * * * disabled"} {
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
