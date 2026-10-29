package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"golang.org/x/term"
)

func configureAuth(args []string) error {
	if len(args) == 0 || (args[0] != "disable-totp" && args[0] != "reset-password") {
		return errors.New("用法：lnmp-gopanel auth {disable-totp|reset-password} --config PATH；请先停止面板并以面板服务账号执行")
	}
	flags := flag.NewFlagSet("auth "+args[0], flag.ContinueOnError)
	path := flags.String("config", "/etc/lnmp-gopanel/config.yaml", "panel configuration")
	var passwordStdin *bool
	if args[0] == "reset-password" {
		passwordStdin = flags.Bool("password-stdin", false, "read a new password from stdin instead of terminal")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不接受额外参数")
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	var password []byte
	if args[0] == "reset-password" {
		password, err = readResetPassword(*passwordStdin, os.Stdin, os.Stderr)
		if err != nil {
			return err
		}
		defer clear(password)
		if err := auth.ValidatePassword(string(password), c.Username); err != nil {
			return err
		}
	}
	account, err := auth.OpenAccount(c.Auth.StateDir, c.Username, c.PasswordHash)
	if err != nil {
		return err
	}
	defer account.Close()
	if args[0] == "reset-password" {
		if err := account.ResetPasswordOffline(string(password)); err != nil {
			return err
		}
		fmt.Println("管理员密码已重置，TOTP 及恢复码已清除。请启动面板服务并使用新密码登录。")
		return nil
	}
	if err = account.DisableTOTPOffline(); err != nil {
		return err
	}
	fmt.Println("TOTP 及恢复码已清除，登录密码保持不变。请启动面板服务。")
	return nil
}

func readResetPassword(stdin bool, input *os.File, output io.Writer) ([]byte, error) {
	if stdin {
		line, err := bufio.NewReader(io.LimitReader(input, 257)).ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		return []byte(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), nil
	}
	fmt.Fprint(output, "新的管理员密码（至少 15 个字符，最多 72 字节）：")
	password, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(output)
	if err != nil {
		clear(password)
		return nil, err
	}
	fmt.Fprint(output, "再次输入新密码：")
	confirmation, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(output)
	defer clear(confirmation)
	if err != nil {
		clear(password)
		return nil, err
	}
	if !bytes.Equal(password, confirmation) {
		clear(password)
		return nil, errors.New("两次输入的新密码不一致")
	}
	return password, nil
}
