package main

import (
	"os"
	"testing"
)

// TestCertReloader tests the reloading functionality of the certificate.
func TestCertReloader(t *testing.T) {
	// Create temporary cert and key files
	tmpCertFile, err := os.CreateTemp("", "cert*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp cert file: %v", err)
	}
	defer os.Remove(tmpCertFile.Name()) // clean up

	tmpKeyFile, err := os.CreateTemp("", "key*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp key file: %v", err)
	}
	defer os.Remove(tmpKeyFile.Name()) // clean up

	// Write initial cert and key to temp files
	initialCertData, _ := os.ReadFile("testdata/cert.pem")
	if err := os.WriteFile(tmpCertFile.Name(), initialCertData, 0644); err != nil {
		t.Fatalf("Failed to write to temp cert file: %v", err)
	}

	initialKeyData, _ := os.ReadFile("testdata/key.pem")
	if err := os.WriteFile(tmpKeyFile.Name(), initialKeyData, 0644); err != nil {
		t.Fatalf("Failed to write to temp key file: %v", err)
	}

	// Setup CertReloader with temp files
	certReloader := NewCertReloader(tmpCertFile.Name(), tmpKeyFile.Name())
	_, err = certReloader.LoadCertificate()
	if err != nil {
		t.Fatalf("Failed to load initial certificate: %v", err)
	}

	// Mocking a certificate change by writing new data to the files
	newCertData, _ := os.ReadFile("testdata/cert.pem")
	if err := os.WriteFile(tmpCertFile.Name(), newCertData, 0644); err != nil {
		t.Fatalf("Failed to write new data to cert file: %v", err)
	}

	// Simulate reloading
	_, err = certReloader.LoadCertificate()
	if err != nil {
		t.Fatalf("Failed to reload certificate: %v", err)
	}
}
