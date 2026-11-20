package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func TestPHPReloadChecksBeforeManagerAction(t *testing.T) {
	for _, s := range []config.Service{{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"}, {Name: "php-fpm", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "php-fpm.service"}, {Name: "php-fpm", Manager: "lnmp", Program: "/usr/bin/lnmp", Target: "php-fpm"}} {
		t.Run(s.Manager, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				calls := []string{}
				run := func(_ context.Context, program string, args ...string) (string, error) {
					calls = append(calls, program+" "+strings.Join(args, " "))
					if len(calls) == 1 && fail {
						return "", errors.New("invalid PHP config")
					}
					return "", nil
				}
				_, err := Execute(context.Background(), s, "reload", run)
				if fail {
					if err == nil || len(calls) != 1 {
						t.Fatal(calls, err)
					}
				} else if err != nil || len(calls) != 2 || !strings.HasPrefix(calls[1], s.Program) || !strings.Contains(calls[1], "reload") {
					t.Fatal(calls, err)
				}
				if calls[0] != "/usr/local/php/sbin/php-fpm -t -y /usr/local/php/etc/php-fpm.conf -c /usr/local/php/etc/php.ini" {
					t.Fatal(calls)
				}
			}
		})
	}
}
