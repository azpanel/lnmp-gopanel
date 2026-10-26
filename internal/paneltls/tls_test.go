package paneltls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSelfSignedIdentity(t *testing.T) {
	certPEM, keyPEM, err := SelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	pair, err := Validate(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.CheckSignatureFrom(leaf); err == nil {
		t.Fatal("server certificate should not be a CA")
	}
	if err := leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"localhost", "127.0.0.1", "::1"} {
		if err := leaf.VerifyHostname(name); err != nil {
			t.Fatal(err)
		}
	}
	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve.Params().BitSize != 256 || leaf.IsCA || leaf.NotAfter.Before(time.Now().Add(364*24*time.Hour)) {
		t.Fatalf("unexpected identity: %+v", leaf)
	}
	other, _, err := SelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(other, keyPEM); err == nil {
		t.Fatal("mismatched key accepted")
	}
	leaf.NotAfter = time.Now().Add(-time.Hour)
	leaf.NotBefore = time.Now().Add(-2 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &pair.PrivateKey.(*ecdsa.PrivateKey).PublicKey, pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM); err == nil {
		t.Fatal("expired certificate accepted")
	}
}

func TestImportChainAndCombinedPEM(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := SelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := SelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	// The extra certificate stands in for an intermediate. A combined PEM must
	// never leak the private key into the copied certificate file.
	chain := append(bytes.Clone(cert), other...)
	if err := os.WriteFile(filepath.Join(dir, "fullchain.cer"), append(bytes.Clone(chain), key...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leaf.crt"), cert, 0600); err != nil {
		t.Fatal(err)
	}
	imported, importedKey, err := Import(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(imported, chain) || !bytes.Equal(importedKey, key) || bytes.Contains(imported, []byte("PRIVATE KEY")) {
		t.Fatal("full chain or key was not preserved correctly")
	}
	if _, _, err := Import(t.TempDir()); err == nil {
		t.Fatal("empty directory accepted")
	}
}

func TestFreeAddressSkipsOccupiedPort(t *testing.T) {
	occupied, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	address, err := FreeAddress("0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	if address == occupied.Addr().String() {
		t.Fatal("selected occupied port")
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
}
