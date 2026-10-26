package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	cronlib "github.com/robfig/cron/v3"
)

type Preview struct {
	Line     int         `json:"line"`
	Schedule string      `json:"schedule"`
	Timezone string      `json:"timezone"`
	Times    []time.Time `json:"times"`
	Message  string      `json:"message,omitempty"`
}

// NextRuns calculates strictly future occurrences without registering or running
// a job. Environment declarations only affect the lines that follow them.
func NextRuns(view Snapshot, task Task, now time.Time) Preview {
	result := Preview{Line: task.Line, Schedule: task.Schedule, Times: []time.Time{}}
	if task.Schedule == "@reboot" {
		result.Message = "系统启动时执行，无法预测执行时间。"
		return result
	}
	location := time.Local
	zone := ""
	for i, line := range strings.Split(view.Raw, "\n") {
		if i+1 >= task.Line {
			break
		}
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(name) != "CRON_TZ" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		zone = value
	}
	// Only the last declaration applies; empty CRON_TZ restores the default.
	if zone != "" {
		var err error
		location, err = time.LoadLocation(zone)
		if err != nil {
			result.Message = "CRON_TZ 时区无效，无法计算执行时间。"
			return result
		}
	}
	result.Timezone = location.String()
	if location == time.Local {
		result.Timezone = "服务器时区 · " + now.In(location).Format("MST (UTC-07:00)")
	}
	expression := task.Schedule
	fields := strings.Fields(expression)
	if len(fields) == 5 {
		weekday, err := normalizeSunday(fields[4])
		if err != nil {
			result.Message = "表达式无效，无法计算执行时间。"
			return result
		}
		fields[4] = weekday
		expression = strings.Join(fields, " ")
	}
	schedule, err := cronlib.ParseStandard(expression)
	if err != nil {
		result.Message = "表达式无效或暂不支持，无法计算执行时间。"
		return result
	}
	current := now.In(location)
	for range 5 {
		current = schedule.Next(current)
		if current.IsZero() {
			result.Message = "未来五年内没有匹配的执行时间。"
			break
		}
		result.Times = append(result.Times, current.In(location))
	}
	return result
}

// Unix cron accepts both 0 and 7 for Sunday; robfig/cron only accepts 0.
// Expand segments involving 7 while keeping wildcard segments intact, since
// the library uses wildcards to implement day-of-month/day-of-week OR semantics.
func normalizeSunday(field string) (string, error) {
	parts := strings.Split(strings.ToUpper(field), ",")
	names := strings.NewReplacer("SUN", "0", "MON", "1", "TUE", "2", "WED", "3", "THU", "4", "FRI", "5", "SAT", "6")
	for i, part := range parts {
		if !strings.Contains(part, "7") || strings.HasPrefix(part, "*") {
			continue
		}
		part = names.Replace(part)
		span, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		var err error
		if hasStep {
			step, err = strconv.Atoi(stepText)
			if err != nil || step <= 0 {
				return "", fmt.Errorf("invalid weekday step")
			}
		}
		startText, endText, hasEnd := strings.Cut(span, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < 0 || start > 7 {
			return "", fmt.Errorf("invalid weekday")
		}
		end := start
		if hasEnd {
			end, err = strconv.Atoi(endText)
		} else if hasStep {
			end = 7
		}
		if err != nil || end < start || end > 7 {
			return "", fmt.Errorf("invalid weekday range")
		}
		values := []string{}
		for value := start; value <= end; {
			values = append(values, strconv.Itoa(value%7))
			if step > end-value {
				break
			}
			value += step
		}
		parts[i] = strings.Join(values, ",")
	}
	return strings.Join(parts, ","), nil
}
