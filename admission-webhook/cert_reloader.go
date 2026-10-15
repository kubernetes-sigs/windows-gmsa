package main

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

type CertReloader struct {
	certPath     string
	keyPath      string
	certChecksum [sha256.Size]byte
	keyChecksum  [sha256.Size]byte

	certificate *tls.Certificate
	watcher     *fsnotify.Watcher
	mux         sync.RWMutex
	stop        chan struct{}
}

func NewCertReloader(certPath, keyPath string) (*CertReloader, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	cw := &CertReloader{
		certPath: certPath,
		keyPath:  keyPath,
		watcher:  watcher,
		stop:     make(chan struct{}),
	}

	// 1. Initial load the certificate from disk.
	if err := cw.loadCert(); err != nil {
		_ = watcher.Close()
		return nil, err
	}

	// 2. Watch the directory containing the files (When K8s Secret mount updates, it essentially changes the ..data symlink in the parent directory)
	certDir := filepath.Dir(certPath)
	if err := watcher.Add(certDir); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("failed to watch directory %s: %w", certDir, err)
	}

	keyDir := filepath.Dir(keyPath)
	if keyDir != certDir {
		if err := watcher.Add(keyDir); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("failed to watch directory %s: %w", keyDir, err)
		}
	}

	logrus.Infof("fsnotify watcher started on directory: %s", certDir)
	if keyDir != certDir {
		logrus.Infof("fsnotify watcher started on directory: %s", keyDir)
	}

	// 3. Start a goroutine to watch for events
	go cw.watchEvents()

	return cw, nil
}

func (cw *CertReloader) watchEvents() {
	var timer *time.Timer
	var timerCh <-chan time.Time
	// debounceDuration is the quiet period after the last fsnotify event before
	// triggering a reload. fsnotify commonly emits multiple events for a single
	// file operation (e.g., WRITE + CHMOD). Debouncing coalesces these into
	// one reload
	debounceDuration := 200 * time.Millisecond

	for {
		select {
		case <-cw.stop:
			if timer != nil {
				timer.Stop()
			}
			return
		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}
			// K8s Secret updates trigger Write/Create/Chmod or Remove events
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Chmod) || event.Has(fsnotify.Remove) {
				// Use a synchronous debounce mechanism to avoid concurrent loadCert calls and frequent reads
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(debounceDuration)
				timerCh = timer.C
			}
		case <-timerCh:
			if err := cw.loadCert(); err != nil {
				logrus.Errorf("Event-driven cert reload failed: %v", err)
			}
		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			logrus.Errorf("fsnotify error: %v", err)
		}
	}
}

func (cw *CertReloader) Stop() {
	close(cw.stop)
	_ = cw.watcher.Close()
}

func (cw *CertReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cw.mux.RLock()
	defer cw.mux.RUnlock()
	if cw.certificate == nil {
		return nil, os.ErrNotExist
	}
	return cw.certificate, nil
}

func (cw *CertReloader) loadCert() error {
	certFile, err := os.ReadFile(cw.certPath)
	if err != nil {
		return fmt.Errorf("read cert failed: %w", err)
	}
	keyFile, err := os.ReadFile(cw.keyPath)
	if err != nil {
		return fmt.Errorf("read key failed: %w", err)
	}

	certChecksum := sha256.Sum256(certFile)
	keyChecksum := sha256.Sum256(keyFile)

	cw.mux.RLock()
	same := certChecksum == cw.certChecksum && keyChecksum == cw.keyChecksum
	cw.mux.RUnlock()

	if !same {
		keyPair, err := tls.X509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("parse key pair failed: %w", err)
		}

		cw.mux.Lock()
		cw.certificate = &keyPair
		cw.certChecksum = certChecksum
		cw.keyChecksum = keyChecksum
		cw.mux.Unlock()

		logrus.Info("[fsnotify] Certificate dynamically reloaded into memory in real-time!")
	}
	return nil
}
