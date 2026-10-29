package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/control"
)

func handleNginxManagement(action string) error {
	ctx, done := context.WithTimeout(context.Background(), 25*time.Second)
	defer done()
	if action == "repair-preview" {
		plan, err := control.RootNginxManagementPreview(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	var request control.ManagementRepairRequest
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1025))
	if err != nil || len(data) > 1024 {
		return errors.New("修复请求无效或过大")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil || len(request.Revision) != 64 {
		return errors.New("修复预览标识无效，请重新预览")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("修复请求无效")
	}
	result, err := control.RootNginxManagementRepair(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
