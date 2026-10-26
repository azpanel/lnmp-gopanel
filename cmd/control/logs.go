package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/azpanel/lnmp-gopanel/internal/logs"
)

func decodeLogRequest(reader io.Reader) (logs.Request, error) {
	var req logs.Request
	raw, err := io.ReadAll(io.LimitReader(reader, 4097))
	if err != nil || len(raw) > 4096 {
		return req, logs.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, logs.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return req, logs.ErrInvalid
	}
	return req, logs.Validate(req)
}
func handleLogs(action string) error {
	ctx, cancel := context.WithTimeout(context.Background(), logs.Timeout)
	defer cancel()
	store := logs.Store{}
	var data any
	var err error
	switch action {
	case "list":
		data, err = store.List(ctx)
	case "read":
		var req logs.Request
		req, err = decodeLogRequest(os.Stdin)
		if err == nil {
			data, err = store.Read(ctx, req)
		}
	default:
		return errors.New("日志操作不受支持")
	}
	if err != nil {
		code := "unavailable"
		if errors.Is(err, logs.ErrInvalid) {
			code = "invalid"
		}
		if errors.Is(err, logs.ErrChanged) {
			code = "changed"
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"message": err.Error(), "code": code})
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"data": data})
}
