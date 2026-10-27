package control

import (
	"errors"
	"path/filepath"
)

func acmeHomes() []string { return []string{"/root/.acme.sh", "/usr/local/acme.sh"} }

// Keep a single trusted installation for account registration, issuance,
// certificate deployment and the renewal job, even if another client appears.
type acmeClient struct {
	home  string
	trust func(string) error
}

func findACMEClient(homes []string, trust func(string) error) (acmeClient, error) {
	for _, home := range homes {
		client := acmeClient{home: home, trust: trust}
		if executable(client.program()) && trust(client.program()) == nil {
			return client, nil
		}
	}
	return acmeClient{}, errors.New("未找到可信且可执行的 acme.sh，请检查 /root/.acme.sh/acme.sh 或 /usr/local/acme.sh/acme.sh 及其父目录权限")
}

func (client acmeClient) program() string  { return filepath.Join(client.home, "acme.sh") }
func (client acmeClient) certHome() string { return filepath.Join(client.home, "gopanel-certs") }
func (client acmeClient) baseArgs(server string) []string {
	return []string{"--home", client.home, "--cert-home", client.certHome(), "--server", server}
}
func (client acmeClient) renewalCron() string {
	return "SHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n17 3 * * * root " + shellQuote(client.program()) + " --cron --home " + shellQuote(client.home) + " --cert-home " + shellQuote(client.certHome()) + " > /dev/null 2>&1\n"
}
