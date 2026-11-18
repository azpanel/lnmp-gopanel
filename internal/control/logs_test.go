package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/logs"
)

// Preserve the previous decoder as a compatibility and allocation baseline.
func legacyLogResponse(data []byte) (logs.Window, error) {
	var out logs.Window
	decoder := json.NewDecoder(bytes.NewReader(data))
	var envelope logEnvelope[json.RawMessage]
	if decoder.Decode(&envelope) != nil {
		return out, errors.New("日志辅助程序返回无效数据")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return out, errors.New("日志辅助程序返回无效数据")
	}
	if envelope.Message != "" {
		switch envelope.Code {
		case "changed":
			return out, logs.ErrChanged
		case "invalid":
			return out, errors.New(logs.ErrInvalid.Error() + "：" + strings.TrimPrefix(envelope.Message, logs.ErrInvalid.Error()+"："))
		default:
			return out, errors.New(envelope.Message)
		}
	}
	if json.Unmarshal(envelope.Data, &out) != nil {
		return out, errors.New("日志辅助程序返回无效数据")
	}
	return out, nil
}

func TestDecodeLogResponseCompatibility(t *testing.T) {
	for _, raw := range []string{
		`{"data":{"lines":[],"start":"0","end":"1"}}`,
		`{"data":null}`, `null`, `{}`, `[]`, ``,
		`{"data":{}} {}`, `{"data":{}} null`, `{"data":{}} garbage`,
		`{"data":{}} `, `{"data":`, `{"data":42}`, `{"data":{"lines":42}}`,
		`{"data":{},"message":"轮转","code":"changed"}`,
		`{"data":42,"message":"轮转","code":"changed"}`,
		`{"message":"日志请求无效：规则错误","code":"invalid"}`,
		`{"message":"规则错误","code":"invalid"}`,
		`{"message":"读取失败"}`, `{"data":{},"message":42}`,
		`{"data":42,"data":{}}`, `{"data":{},"data":42}`,
		`{"data":{"start":"old"},"data":{"end":"new"}}`,
		`{"data":{"start":"old"},"data":null}`,
	} {
		t.Run(raw, func(t *testing.T) {
			want, oldErr := legacyLogResponse([]byte(raw))
			got, err := decodeLogResponse[logs.Window]([]byte(raw))
			if (err == nil) != (oldErr == nil) || (err != nil && err.Error() != oldErr.Error()) {
				t.Fatalf("error=%v, previous=%v", err, oldErr)
			}
			if err == nil && !reflect.DeepEqual(got, want) {
				t.Fatalf("result=%+v, previous=%+v", got, want)
			}
		})
	}
	for _, tc := range []struct {
		code string
		err  error
	}{{"changed", logs.ErrChanged}, {"invalid", logs.ErrInvalid}} {
		_, err := decodeLogResponse[logs.Window]([]byte(`{"message":"error","code":"` + tc.code + `"}`))
		if !errors.Is(err, tc.err) {
			t.Fatalf("lost error identity: %v", err)
		}
	}
}

func TestLogCommandBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, script, wantError string
		cancel                  bool
		timeout                 time.Duration
	}{
		{name: "valid", script: `printf '{"data":{"lines":[],"start":"0","end":"1"}}'`},
		{name: "stdout limit", script: "head -c 8388609 /dev/zero", wantError: "过大"},
		{name: "stderr limit", script: "head -c 16385 /dev/zero >&2", wantError: "过大"},
		{name: "failed process", script: `printf '{"data":{}}'; exit 1`, wantError: "不可用"},
		{name: "trailing json", script: `printf '{"data":{}} {}'`, wantError: "无效数据"},
		{name: "cancelled", script: "sleep 5", cancel: true, wantError: "已取消"},
		{name: "timeout", script: `printf '{"data":{}}'; sleep 5`, timeout: 30 * time.Millisecond, wantError: "超时"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			duration := tc.timeout
			if duration == 0 {
				duration = 5 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			if tc.cancel {
				cancel()
			}
			cmd := exec.CommandContext(ctx, "/bin/sh", "-c", tc.script)
			configureProcess(cmd)
			out, err := runLogCommand[logs.Window](ctx, cmd)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q, got %v", tc.wantError, err)
				}
			} else if err != nil || out.Lines == nil || out.Start != "0" || out.End != "1" {
				t.Fatalf("window=%+v, error=%v", out, err)
			}
		})
	}
}

func BenchmarkLogResponse(b *testing.B) {
	window := logs.Window{Lines: make([]logs.Line, 1000), Start: "0", End: "256000"}
	for i := range window.Lines {
		window.Lines[i] = logs.Line{Offset: "256", Text: strings.Repeat("access log ", 24)}
	}
	data, err := json.Marshal(logEnvelope[logs.Window]{Data: window})
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		read func([]byte) (logs.Window, error)
	}{{"previous", legacyLogResponse}, {"direct", decodeLogResponse[logs.Window]}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				out, err := tc.read(data)
				if err != nil || len(out.Lines) != len(window.Lines) {
					b.Fatal("invalid response", err)
				}
			}
		})
	}
}
