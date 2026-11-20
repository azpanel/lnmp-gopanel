package control

import (
	"errors"
	"strings"
	"testing"
)

func TestPHPConfigValidation(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"revision":"bad","fields":{"memory_limit":"128M"}}`,
		`{"revision":"REV","fields":{"unknown":"1"}}`,
		`{"revision":"REV","fields":{"memory_limit":"128M\nextension=bad"}}`,
		`{"revision":"REV","fields":{"memory_limit":"0"}}`,
		`{"revision":"REV","fields":{"max_execution_time":"-1"}}`,
		`{"revision":"REV","fields":{"date.timezone":"Invalid/Zone"}}`,
		`{"revision":"REV","fields":{"display_errors":"stderr"}}`,
		`{"revision":"REV","disable_functions":["exec;system"]}`,
		`{"revision":"REV","disable_functions":[""]}`,
		`{"revision":"REV","fields":{"memory_limit":"128M"},"path":"/etc/passwd"}`,
		`{"revision":"REV","fields":{"memory_limit":"128M"}} {}`,
		`{"revision":"REV","fields":{"memory_limit":"128M"}}` + strings.Repeat(" ", PHPConfigRequestLimit),
	} {
		body = strings.ReplaceAll(body, "REV", strings.Repeat("a", 64))
		if _, err := DecodePHPConfigRequest(strings.NewReader(body)); !errors.Is(err, ErrPHPConfigInvalid) {
			t.Fatalf("accepted %s: %v", body[:min(len(body), 200)], err)
		}
	}
	for _, body := range []string{
		`{"revision":"REV","fields":{"memory_limit":"-1","max_input_time":"-1","max_execution_time":"0","date.timezone":"Asia/Shanghai","display_errors":"Off","post_max_size":"0","upload_max_filesize":"2G"}}`,
		`{"revision":"REV","disable_functions":[]}`,
	} {
		if _, err := DecodePHPConfigRequest(strings.NewReader(strings.ReplaceAll(body, "REV", strings.Repeat("a", 64)))); err != nil {
			t.Fatal(err)
		}
	}
	names, err := NormalizePHPFunctions([]string{"EXEC", "shell_exec", " exec "})
	if err != nil || strings.Join(names, ",") != "EXEC,exec,shell_exec" {
		t.Fatal(names, err)
	}
}
