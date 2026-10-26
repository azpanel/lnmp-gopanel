// Package paneltls prepares the panel's HTTPS identity without external tools.
package paneltls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func Validate(certPEM, keyPEM []byte) (tls.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return pair, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return pair, err
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return pair, errors.New("certificate is expired or not yet valid")
	}
	if len(leaf.DNSNames) == 0 && len(leaf.IPAddresses) == 0 {
		return pair, errors.New("certificate needs DNS or IP subject alternative names")
	}
	if key, ok := leaf.PublicKey.(*ecdsa.PublicKey); ok && key.Curve.Params().BitSize < 256 {
		return pair, errors.New("certificate EC key must be at least 256 bits")
	}
	if len(leaf.ExtKeyUsage) > 0 {
		server := false
		for _, usage := range leaf.ExtKeyUsage {
			server = server || usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny
		}
		if !server {
			return pair, errors.New("certificate does not permit server authentication")
		}
	}
	return pair, nil
}

func Load(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := os.ReadFile(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	return Validate(cert, key)
}

func SelfSigned() ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	cert := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "LNMP GoPanel"},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" && hostname != "localhost" {
		cert.DNSNames = append(cert.DNSNames, hostname)
	}
	if addresses, err := net.InterfaceAddrs(); err == nil {
		for _, address := range addresses {
			if ip, _, err := net.ParseCIDR(address.String()); err == nil && !ip.IsLoopback() && !ip.IsUnspecified() {
				cert.IPAddresses = append(cert.IPAddresses, ip)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), nil
}

// Import finds a matching certificate/key pair, preferring full chains. Files
// may be symlinks, as used by ACME clients; their targets are only read.
func Import(dir string) ([]byte, []byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var certs, keys [][]byte
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.Contains(strings.ToLower(entries[i].Name()), "fullchain") && !strings.Contains(strings.ToLower(entries[j].Name()), "fullchain")
	})
	for _, entry := range entries {
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".pem", ".crt", ".cer", ".key":
		default:
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "-----BEGIN CERTIFICATE-----") {
			certs = append(certs, data)
		}
		for rest := data; len(rest) > 0; {
			block, remainder := pem.Decode(rest)
			if block == nil {
				break
			}
			if strings.HasSuffix(block.Type, "PRIVATE KEY") {
				keys = append(keys, pem.EncodeToMemory(block))
			}
			rest = remainder
		}
	}
	for _, cert := range certs {
		for _, key := range keys {
			if _, err := Validate(cert, key); err == nil {
				// Only retain certificate PEM blocks, never private data from a
				// combined PEM file in the certificate chain.
				var chain []byte
				for rest := cert; len(rest) > 0; {
					block, remainder := pem.Decode(rest)
					if block == nil {
						break
					}
					if block.Type == "CERTIFICATE" {
						chain = append(chain, pem.EncodeToMemory(block)...)
					}
					rest = remainder
				}
				return chain, key, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("no valid matching certificate and unencrypted private key in %s", dir)
}

// FreeAddress asks the kernel for an unused port and persists it for subsequent
// starts. Startup still fails safely if another process claims it afterwards.
func FreeAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if port != "0" {
		return address, nil
	}
	network := "tcp"
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		network = "tcp4"
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return "", err
	}
	chosen := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, fmt.Sprint(chosen)), nil
}
