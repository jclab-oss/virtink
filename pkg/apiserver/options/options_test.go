package options

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeServingCert writes a certificate and key the way cert-manager lays out
// its Secrets, which are mounted read-only.
func writeServingCert(t *testing.T, dir string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "virt-controller.virtink-system.svc"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
}

func TestConfigUsesMountedServingCert(t *testing.T) {
	dir := t.TempDir()
	writeServingCert(t, dir)

	o := NewServerOptions()
	o.SecureServing.BindPort = 0
	o.SecureServing.ServerCert.CertKey.CertFile = filepath.Join(dir, "tls.crt")
	o.SecureServing.ServerCert.CertKey.KeyFile = filepath.Join(dir, "tls.key")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	o.SecureServing.Listener = listener

	if _, err := o.Config(); err != nil {
		t.Fatalf("Config() = %v, want no error", err)
	}
}

func TestDefaultServingCertPaths(t *testing.T) {
	o := NewServerOptions()
	certKey := o.SecureServing.ServerCert.CertKey
	if certKey.CertFile != "/var/run/virtink/serving-cert/tls.crt" || certKey.KeyFile != "/var/run/virtink/serving-cert/tls.key" {
		t.Errorf("serving cert = (%q, %q), want the tls.crt and tls.key cert-manager puts in the mounted Secret", certKey.CertFile, certKey.KeyFile)
	}
}
