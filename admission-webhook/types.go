package main

type tlsConfig struct {
	crtPath string
	keyPath string
}

type kubeClientInterface interface {
	isAuthorizedToUseCredSpec(serviceAccountName, namespace, credSpecName string) (authorized bool, reason string)
	retrieveCredSpecContents(credSpecName string) (contents string, httpCode int, err error)
}
