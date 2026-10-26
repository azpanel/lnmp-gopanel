package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/paneltls"
	"gopkg.in/yaml.v3"
)

const certificateRoot = "/usr/local/nginx/conf/ssl"

func configureHTTPS(args []string) error {
	flags := flag.NewFlagSet("https", flag.ContinueOnError)
	path := flags.String("config", "/etc/lnmp-gopanel/config.yaml", "panel configuration to migrate")
	check := flags.Bool("check", false, "validate HTTPS configuration and print the listen address")
	list := flags.Bool("list-certificates", false, "list usable certificate directories under "+certificateRoot)
	certDir := flags.String("certificate-dir", "", "directory containing an existing certificate/key pair")
	tlsDir := flags.String("tls-dir", "", "installed TLS directory (defaults to the configuration's TLS directory)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *list {
		entries, err := os.ReadDir(certificateRoot)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			dir := filepath.Join(certificateRoot, entry.Name())
			if strings.ContainsAny(dir, "\r\n\t") {
				continue
			}
			if _, _, err := paneltls.Import(dir); err == nil {
				fmt.Println(dir)
			}
		}
		return nil
	}
	if *check {
		c, err := config.Load(*path)
		if err != nil {
			return err
		}
		if _, err := paneltls.Load(c.TLS.CertFile, c.TLS.KeyFile); err != nil {
			return err
		}
		fmt.Println(c.Listen)
		return nil
	}
	var c config.Config
	if err := config.Read(*path, &c); err != nil {
		return err
	}
	if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
		c.Listen = "0.0.0.0:0"
	}
	if err := prepareHTTPS(&c, filepath.Dir(*path), *tlsDir, *certDir); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	// This command writes a staged configuration; the installer promotes it
	// together with its certificates, with backups and rollback.
	if err := os.WriteFile(*path, data, 0600); err != nil {
		return err
	}
	fmt.Println(c.Listen)
	return nil
}

func prepareHTTPS(c *config.Config, configDir, installedTLSDir, certDir string) error {
	address, err := paneltls.FreeAddress(c.Listen)
	if err != nil {
		return err
	}
	var cert, key []byte
	if certDir == "" {
		cert, key, err = paneltls.SelfSigned()
	} else {
		cert, key, err = paneltls.Import(certDir)
	}
	if err != nil {
		return err
	}
	stagedDir := filepath.Join(configDir, "tls")
	if installedTLSDir == "" {
		installedTLSDir = stagedDir
	}
	installedTLSDir, err = filepath.Abs(installedTLSDir)
	if err != nil {
		return err
	}
	if err := os.Mkdir(stagedDir, 0750); err != nil {
		return fmt.Errorf("create TLS directory (existing certificates are never overwritten): %w", err)
	}
	for name, data := range map[string][]byte{"panel.crt": cert, "panel.key": key} {
		file, err := os.OpenFile(filepath.Join(stagedDir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	}
	c.Listen = address
	c.TLS = config.TLSConfig{CertFile: filepath.Join(installedTLSDir, "panel.crt"), KeyFile: filepath.Join(installedTLSDir, "panel.key")}
	c.SecureCookie = true
	if strings.HasPrefix(c.PublicOrigin, "http://") {
		c.PublicOrigin = ""
	}
	return nil
}
