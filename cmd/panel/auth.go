package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func configureAuth(args []string) error {
	if len(args) == 0 || args[0] != "disable-totp" {
		return errors.New("用法：lnmp-gopanel auth disable-totp --config PATH；请先停止面板并以面板服务账号执行")
	}
	flags := flag.NewFlagSet("auth disable-totp", flag.ContinueOnError)
	path := flags.String("config", "/etc/lnmp-gopanel/config.yaml", "panel configuration")
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
	account, err := auth.OpenAccount(c.Auth.StateDir, c.Username, c.PasswordHash)
	if err != nil {
		return err
	}
	defer account.Close()
	if err = account.DisableTOTPOffline(); err != nil {
		return err
	}
	fmt.Println("TOTP 及恢复码已清除，登录密码保持不变。请启动面板服务。")
	return nil
}
