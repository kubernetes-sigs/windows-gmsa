package main

import "context"

type tlsConfig struct {
	crtPath string
	keyPath string
}

type kubeClientInterface interface {
	isAuthorizedToUseCredSpec(ctx context.Context, serviceAccountName, namespace, credSpecName string) (authorized bool, reason string)
	retrieveCredSpecContents(ctx context.Context, credSpecName string) (contents string, httpCode int, err error)
}
