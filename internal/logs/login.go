package logs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"time"
)

type LoginEvent struct {
	Time     time.Time `json:"time"`
	IP       string    `json:"ip"`
	UA       string    `json:"ua"`
	Username string    `json:"username"`
	Result   string    `json:"result"`
	Stage    string    `json:"stage"`
	Reason   string    `json:"reason"`
	Password *string   `json:"password,omitempty"`
}

// LoginLog grants access to exactly one panel-owned file, including when its
// directory is forbidden by the general-purpose log reader's path policy.
type LoginLog struct {
	path       string
	mu         sync.Mutex
	writeError string
}

func OpenLoginLog(dir string) (*LoginLog, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, errors.New("登录日志目录必须是规范的绝对路径")
	}
	l := &LoginLog{path: filepath.Join(dir, "login.log")}
	f, err := l.open(true)
	if err == nil {
		err = f.Close()
	}
	if err != nil {
		l.writeError = "登录日志初始化失败，请检查状态目录及文件权限"
	}
	return l, err
}

func (l *LoginLog) Path() string { return l.path }

func (l *LoginLog) Append(event LoginEvent) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer func() {
		l.writeError = ""
		if err != nil {
			l.writeError = "登录日志写入失败，请检查状态目录及文件权限"
		}
	}()
	// Enforce the password rule at the storage boundary as well as the handler.
	if event.Result != "failure" || event.Stage != "password" || event.Reason != "invalid_credentials" {
		event.Password = nil
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := l.open(true)
	if err != nil {
		return err
	}
	n, err := f.Write(append(data, '\n'))
	if err == nil && n != len(data)+1 {
		err = io.ErrShortWrite
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func (l *LoginLog) Read(ctx context.Context, req Request) (Window, error) {
	if req.Path != l.path {
		return Window{}, ErrInvalid
	}
	if err := validateWindow(req); err != nil {
		return Window{}, err
	}
	if err := ctx.Err(); err != nil {
		return Window{}, err
	}
	f, err := l.open(false)
	if err != nil {
		return Window{}, err
	}
	defer f.Close()
	return readFile(ctx, f, req)
}

func (l *LoginLog) File(ctx context.Context) File {
	file := File{Path: l.path, Name: "登录日志", Format: "login"}
	window, err := l.Read(ctx, Request{Path: l.path, Mode: "stat"})
	if err != nil {
		file.Error = "登录日志不可读取，请检查状态目录及文件权限"
	} else {
		file.Size, file.Modified, file.Available = window.Size, window.Modified, true
	}
	l.mu.Lock()
	if l.writeError != "" {
		file.Error = l.writeError
	}
	l.mu.Unlock()
	return file
}
