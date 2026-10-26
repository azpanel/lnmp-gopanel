package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/docker"
)

const dockerOutputLimit = 4 * 1024 * 1024

type dockerBuffer struct {
	data     bytes.Buffer
	limit    int
	overflow bool
}

func (b *dockerBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.limit-b.data.Len() {
		b.overflow = true
		p = p[:b.limit-b.data.Len()]
	}
	_, _ = b.data.Write(p)
	return n, nil
}

func dockerCommand(ctx context.Context, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	configureProcess(cmd)
	out := &dockerBuffer{limit: dockerOutputLimit}
	stderr := &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = out, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if out.overflow {
		return "", errors.New("Docker 返回数据过大")
	}
	return out.data.String(), err
}

func DockerSnapshot(ctx context.Context) (docker.Snapshot, error) {
	program := firstExecutable("/usr/bin/docker", "/usr/local/bin/docker")
	if program == "" {
		return docker.Snapshot{Status: "unavailable", Message: "本机未安装 Docker", Errors: map[string]string{}}, nil
	}
	if err := TrustedPath(program); err != nil {
		return docker.Snapshot{}, errors.New("Docker 程序权限不安全")
	}
	return docker.Collect(ctx, func(ctx context.Context, args ...string) (string, error) { return dockerCommand(ctx, program, args...) }), nil
}

func (h Helper) Docker(parent context.Context) (docker.Snapshot, error) {
	ctx, cancel := context.WithTimeout(parent, docker.Timeout+time.Second)
	defer cancel()
	out, err := dockerCommand(ctx, "/usr/bin/sudo", "-n", h.Path, "docker", "snapshot")
	if err != nil {
		return docker.Snapshot{}, errors.New("Docker 查询辅助程序不可用，请检查安装与 docker snapshot 的 sudoers 权限")
	}
	var result docker.Snapshot
	if json.Unmarshal([]byte(out), &result) != nil {
		return result, errors.New("Docker 辅助程序返回无效数据")
	}
	switch result.Status {
	case "ready", "partial", "failed", "unavailable":
		return result, nil
	}
	return result, errors.New("Docker 辅助程序返回无效状态")
}
