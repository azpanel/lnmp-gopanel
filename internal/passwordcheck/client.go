// Package passwordcheck runs the password scorer outside the panel process.
// It deliberately does not import the scorer or its resident dictionaries.
package passwordcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode"
	"unicode/utf8"
)

const BinaryName = "lnmp-gopanel-password-check"
const MaxRequestBytes = 2048
const Timeout = 5 * time.Second

var ErrUnavailable = errors.New("密码强度校验暂时不可用，请稍后重试")

type Request struct {
	Password string `json:"password"`
	Username string `json:"username"`
}

type Response struct {
	Version int  `json:"version"`
	Score   *int `json:"score"`
}

func ValidRequest(r Request) bool {
	if !utf8.ValidString(r.Password) || utf8.RuneCountInString(r.Password) < 15 || len(r.Password) > 72 || len(r.Username) > 128 {
		return false
	}
	for _, char := range r.Password {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

// Score starts one ordinary child process. Passwords never appear in arguments,
// environment variables, files, diagnostics, or returned execution errors.
func Score(password, username string) (int, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, ErrUnavailable
	}
	return score(context.Background(), filepath.Join(filepath.Dir(executable), BinaryName), Request{Password: password, Username: username})
}

func score(parent context.Context, executable string, request Request) (int, error) {
	if !ValidRequest(request) {
		return 0, ErrUnavailable
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > MaxRequestBytes {
		return 0, ErrUnavailable
	}
	defer clear(data)
	ctx, cancel := context.WithTimeout(parent, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	var output responseBuffer
	cmd.Stdout = &output
	if cmd.Run() != nil || ctx.Err() != nil || output.overflow {
		return 0, ErrUnavailable
	}
	var response Response
	if json.Unmarshal(output.data[:output.n], &response) != nil || response.Version != 1 || response.Score == nil || *response.Score < 0 || *response.Score > 4 {
		return 0, ErrUnavailable
	}
	return *response.Score, nil
}

type responseBuffer struct {
	data     [128]byte
	n        int
	overflow bool
}

func (b *responseBuffer) Write(p []byte) (int, error) {
	n := copy(b.data[b.n:], p)
	b.n += n
	b.overflow = b.overflow || n < len(p)
	return len(p), nil
}
