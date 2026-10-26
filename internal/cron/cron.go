// Package cron describes the read-only root crontab view.
package cron

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const Timeout = 5 * time.Second

type Task struct {
	Line     int    `json:"line"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

type Snapshot struct {
	Tasks     []Task    `json:"tasks"`
	Raw       string    `json:"raw"`
	Warnings  []string  `json:"warnings"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Reader interface {
	Cron(context.Context) (Snapshot, error)
}

var environment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=`)
var numericField = regexp.MustCompile(`^[0-9*/,\-]+$`)

// Parse preserves commands verbatim, including shell whitespace, % and comments.
// It recognizes cron field syntax without attempting to predict execution times.
func Parse(raw string) Snapshot {
	view := Snapshot{Tasks: []Task{}, Raw: raw, Warnings: []string{}, UpdatedAt: time.Now()}
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimLeft(strings.TrimSuffix(line, "\r"), " \t")
		if line == "" || strings.HasPrefix(line, "#") || environment.MatchString(line) {
			continue
		}
		first, rest := token(line)
		schedule, command, valid := first, rest, true
		if strings.HasPrefix(first, "@") {
			switch first {
			case "@reboot", "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly":
			default:
				valid = false
			}
		} else {
			fields := []string{first}
			for len(fields) < 5 {
				var field string
				field, rest = token(rest)
				fields = append(fields, field)
			}
			for index, field := range fields {
				if index == 3 {
					field = replaceNames(field, "JAN FEB MAR APR MAY JUN JUL AUG SEP OCT NOV DEC")
				}
				if index == 4 {
					field = replaceNames(field, "SUN MON TUE WED THU FRI SAT")
				}
				valid = valid && numericField.MatchString(field)
			}
			schedule = strings.Join(fields, " ")
			command = rest
		}
		if !valid || strings.TrimSpace(command) == "" {
			view.Warnings = append(view.Warnings, fmt.Sprintf("第 %d 行未识别为定时任务，请查看 crontab 原文。", i+1))
			continue
		}
		view.Tasks = append(view.Tasks, Task{Line: i + 1, Schedule: schedule, Command: command})
	}
	return view
}

func token(line string) (string, string) {
	line = strings.TrimLeft(line, " \t\r")
	if i := strings.IndexAny(line, " \t\r"); i >= 0 {
		return line[:i], strings.TrimLeft(line[i:], " \t\r")
	}
	return line, ""
}

func replaceNames(field, names string) string {
	field = strings.ToUpper(field)
	for _, name := range strings.Fields(names) {
		field = strings.ReplaceAll(field, name, "1")
	}
	return field
}
