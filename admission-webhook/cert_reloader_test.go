package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestCert generates a self-signed certificate and key for testing
func generateTestCert(certPath, keyPath string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Corp"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24),
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}

	return nil
}

func TestCertReloader(t *testing.T) {
	// 1. Create a temporary directory for certs
	tmpDir, err := os.MkdirTemp("", "cert-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "tls.crt")
	keyPath := filepath.Join(tmpDir, "tls.key")

	// 2. Generate initial certificates
	err = generateTestCert(certPath, keyPath)
	require.NoError(t, err)

	// 3. Initialize the EventCertWatcher
	watcher, err := NewCertReloader(certPath, keyPath)
	require.NoError(t, err)
	defer watcher.Stop()

	// 4. Verify initial certificate is loaded
	cert1, err := watcher.GetCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, cert1)

	// 5. Generate new certificates to simulate a K8s Secret update
	err = generateTestCert(certPath, keyPath)
	require.NoError(t, err)

	// 6. Wait for fsnotify to pick up the change and the 100ms debounce to fire
	time.Sleep(300 * time.Millisecond)

	// 7. Verify the certificate was reloaded
	cert2, err := watcher.GetCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, cert2)

	// The pointers should be different, meaning a new cert was loaded
	assert.NotSame(t, cert1, cert2)
}
