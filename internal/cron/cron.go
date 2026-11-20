// Package cron describes root crontab tasks and their enabled state.
package cron

import (
	"context"
	"crypto/sha256"
	"errors"
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
	Enabled  bool   `json:"enabled"`
}

type Snapshot struct {
	Tasks     []Task    `json:"tasks"`
	Raw       string    `json:"raw"`
	Warnings  []string  `json:"warnings"`
	UpdatedAt time.Time `json:"updated_at"`
	Revision  string    `json:"revision"`
}

type Reader interface {
	Cron(context.Context) (Snapshot, error)
}

type ToggleRequest struct {
	Line     int    `json:"line"`
	Revision string `json:"revision"`
	Enabled  *bool  `json:"enabled"`
}

var ErrInvalid = errors.New("定时任务参数无效")
var ErrConflict = errors.New("任务配置已变化，请刷新列表后重试")

func (r ToggleRequest) Validate() error {
	if r.Line <= 0 || r.Enabled == nil || len(r.Revision) != 64 {
		return ErrInvalid
	}
	for _, c := range r.Revision {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return ErrInvalid
		}
	}
	return nil
}

// Toggle changes only the comment marker of an existing task. Checking the
// complete revision prevents stale line numbers from changing another task.
func Toggle(view Snapshot, request ToggleRequest) (string, error) {
	if err := request.Validate(); err != nil {
		return "", err
	}
	if request.Revision != view.Revision {
		return "", ErrConflict
	}
	for _, task := range view.Tasks {
		if task.Line != request.Line {
			continue
		}
		if task.Enabled == *request.Enabled {
			return view.Raw, nil
		}
		lines := strings.Split(view.Raw, "\n")
		line := lines[task.Line-1]
		offset := len(line) - len(strings.TrimLeft(line, " \t"))
		if *request.Enabled {
			lines[task.Line-1] = line[:offset] + strings.TrimPrefix(line[offset:], "#")
		} else {
			lines[task.Line-1] = line[:offset] + "#" + line[offset:]
		}
		return strings.Join(lines, "\n"), nil
	}
	return "", ErrConflict
}

var environment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=`)
var numericField = regexp.MustCompile(`^[0-9*/,\-]+$`)

// Parse preserves commands verbatim, including shell whitespace, % and comments.
// It recognizes cron field syntax without attempting to predict execution times.
func Parse(raw string) Snapshot {
	view := Snapshot{Tasks: []Task{}, Raw: raw, Warnings: []string{}, UpdatedAt: time.Now(), Revision: fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))}
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimLeft(strings.TrimSuffix(line, "\r"), " \t")
		enabled := !strings.HasPrefix(line, "#")
		if !enabled {
			line = strings.TrimLeft(strings.TrimPrefix(line, "#"), " \t")
		}
		if line == "" || environment.MatchString(line) {
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
			if !enabled {
				continue
			}
			view.Warnings = append(view.Warnings, fmt.Sprintf("第 %d 行未识别为定时任务，请查看 crontab 原文。", i+1))
			continue
		}
		view.Tasks = append(view.Tasks, Task{Line: i + 1, Schedule: schedule, Command: command, Enabled: enabled})
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
