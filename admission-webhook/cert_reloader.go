package main

import (
	"crypto/tls"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

type CertReloader struct {
	sync.Mutex
	certPath    string
	keyPath     string
	certificate *tls.Certificate
}

func NewCertReloader(certPath, keyPath string) *CertReloader {
	return &CertReloader{
		certPath: certPath,
		keyPath:  keyPath,
	}
}

// LoadCertificate loads or reloads the certificate from disk.
func (cr *CertReloader) LoadCertificate() (*tls.Certificate, error) {
	cr.Lock()
	defer cr.Unlock()

	cert, err := tls.LoadX509KeyPair(cr.certPath, cr.keyPath)
	if err != nil {
		return nil, err
	}
	cr.certificate = &cert
	return cr.certificate, nil
}

// GetCertificateFunc returns a function that can be assigned to tls.Config.GetCertificate
func (cr *CertReloader) GetCertificateFunc() func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
		return cr.certificate, nil
	}
}

func watchCertFiles(certReloader *CertReloader) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("error creating watcher: %v", err)
	}
	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Rename == fsnotify.Rename {
					logrus.Infof("detected change in certificate file: %v", event.Name)
					_, err := certReloader.LoadCertificate()
					if err != nil {
						logrus.Errorf("error reloading certificate: %v", err)
					} else {
						logrus.Infof("successfully reloaded certificate")
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					logrus.Errorf("watcher error returned !ok: %v", err)
					return
				}
				logrus.Errorf("watcher error: %v", err)
			}
		}
	}()

	err = watcher.Add(certReloader.certPath)
	if err != nil {
		logrus.Fatalf("error watching certificate file: %v", err)
	}
	err = watcher.Add(certReloader.keyPath)
	if err != nil {
		logrus.Fatalf("error watching key file: %v", err)
	}
}
