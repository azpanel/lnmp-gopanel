package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/logs"
)

type logEnvelope[T any] struct {
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Decode directly from the bounded command buffer without retaining a second
// copy of data as json.RawMessage. Defer data errors until the envelope's error
// code is checked, and reset for duplicate data fields as RawMessage did.
type logData[T any] struct {
	value   T
	err     error
	present bool
}

func (d *logData[T]) UnmarshalJSON(data []byte) error {
	var value T
	d.err = json.Unmarshal(data, &value)
	d.value, d.present = value, true
	return nil
}

func decodeLogResponse[T any](data []byte) (T, error) {
	var zero T
	var envelope logEnvelope[logData[T]]
	if json.Unmarshal(data, &envelope) != nil {
		return zero, errors.New("日志辅助程序返回无效数据")
	}
	if envelope.Message != "" {
		switch envelope.Code {
		case "changed":
			return zero, logs.ErrChanged
		case "invalid":
			return zero, fmt.Errorf("%w：%s", logs.ErrInvalid, strings.TrimPrefix(envelope.Message, logs.ErrInvalid.Error()+"："))
		default:
			return zero, errors.New(envelope.Message)
		}
	}
	if !envelope.Data.present || envelope.Data.err != nil {
		return zero, errors.New("日志辅助程序返回无效数据")
	}
	return envelope.Data.value, nil
}

func logCommand[T any](h Helper, parent context.Context, action string, input any) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(parent, logs.Timeout+time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "logs", action)
	configureProcess(cmd)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	if input != nil {
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		err := encoder.Encode(input)
		data := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
		if err != nil || len(data) > 4096 {
			return zero, logs.ErrInvalid
		}
		cmd.Stdin = bytes.NewReader(data)
	}
	return runLogCommand[T](ctx, cmd)
}

func runLogCommand[T any](ctx context.Context, cmd *exec.Cmd) (T, error) {
	var zero T
	stdout, stderr := &cronBuffer{limit: 8 * 1024 * 1024}, &cronBuffer{limit: 16 * 1024}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return zero, errors.New("日志读取超时或已取消")
	}
	if err != nil {
		return zero, errors.New("日志辅助程序不可用，请同步升级辅助程序和 sudoers 权限")
	}
	if stdout.overflow || stderr.overflow {
		return zero, errors.New("日志辅助程序返回数据过大")
	}
	return decodeLogResponse[T](stdout.data.Bytes())
}
func (h Helper) List(ctx context.Context) (logs.Catalog, error) {
	out, err := logCommand[logs.Catalog](h, ctx, "list", nil)
	if err == nil && (out.Files == nil || out.Warnings == nil) {
		err = errors.New("日志列表数据无效")
	}
	return out, err
}
func (h Helper) Read(ctx context.Context, req logs.Request) (logs.Window, error) {
	var out logs.Window
	if err := logs.Validate(req); err != nil {
		return out, err
	}
	out, err := logCommand[logs.Window](h, ctx, "read", req)
	if err == nil && (out.Lines == nil || out.Start == "" || out.End == "") {
		err = errors.New("日志读取数据无效")
	}
	return out, err
}
