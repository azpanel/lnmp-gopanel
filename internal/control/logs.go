package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/logs"
)

type logEnvelope[T any] struct {
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

func (h Helper) logCommand(parent context.Context, action string, input any, output any) error {
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
			return logs.ErrInvalid
		}
		cmd.Stdin = bytes.NewReader(data)
	}
	stdout, stderr := &cronBuffer{limit: 8 * 1024 * 1024}, &cronBuffer{limit: 16 * 1024}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return errors.New("日志读取超时或已取消")
	}
	if err != nil {
		return errors.New("日志辅助程序不可用，请同步升级辅助程序和 sudoers 权限")
	}
	if stdout.overflow || stderr.overflow {
		return errors.New("日志辅助程序返回数据过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.data.Bytes()))
	var envelope logEnvelope[json.RawMessage]
	if decoder.Decode(&envelope) != nil {
		return errors.New("日志辅助程序返回无效数据")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("日志辅助程序返回无效数据")
	}
	if envelope.Message != "" {
		switch envelope.Code {
		case "changed":
			return logs.ErrChanged
		case "invalid":
			return logs.ErrInvalid
		default:
			return errors.New(envelope.Message)
		}
	}
	if json.Unmarshal(envelope.Data, output) != nil {
		return errors.New("日志辅助程序返回无效数据")
	}
	return nil
}
func (h Helper) List(ctx context.Context) (logs.Catalog, error) {
	var out logs.Catalog
	err := h.logCommand(ctx, "list", nil, &out)
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
	err := h.logCommand(ctx, "read", req, &out)
	if err == nil && (out.Lines == nil || out.Start == "" || out.End == "") {
		err = errors.New("日志读取数据无效")
	}
	return out, err
}
