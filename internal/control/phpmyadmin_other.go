//go:build !linux

package control

import "github.com/azpanel/lnmp-gopanel/internal/components"

func probePHPMyAdmin() components.Item {
	return components.Item{Name: "phpmyadmin", Status: "failed", Error: "phpMyAdmin 检测需要 Linux"}
}
