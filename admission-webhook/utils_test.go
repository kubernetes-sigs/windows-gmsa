package main

import (
	"context"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"math/rand"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const dummyCredSpecName = "dummy-cred-spec-name"

const dummyCredSpecContents = `{"We don't need no": ["education", "thought control", "dark sarcasm in the classroom"], "All in all you're just another": {"brick": "in", "the": "wall"}}`
const dummyServiceAccoutName = "dummy-service-account-name"
const dummyNamespace = "dummy-namespace"
const dummyPodName = "dummy-pod-name"
const dummyContainerName = "dummy-container-name"

type dummyKubeClient struct {
	isAuthorizedToUseCredSpecFunc func(ctx context.Context, serviceAccountName, namespace, credSpecName string) (authorized bool, reason string)
	retrieveCredSpecContentsFunc  func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error)
}

func (dkc *dummyKubeClient) isAuthorizedToUseCredSpec(ctx context.Context, serviceAccountName, namespace, credSpecName string) (authorized bool, reason string) {
	if dkc.isAuthorizedToUseCredSpecFunc != nil {
		return dkc.isAuthorizedToUseCredSpecFunc(ctx, serviceAccountName, namespace, credSpecName)
	}
	authorized = true
	return
}

func (dkc *dummyKubeClient) retrieveCredSpecContents(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
	if dkc.retrieveCredSpecContentsFunc != nil {
		return dkc.retrieveCredSpecContentsFunc(ctx, credSpecName)
	}
	contents = dummyCredSpecContents
	return
}

func buildWindowsOptions(credSpecName, credSpecContents string) *corev1.WindowsSecurityContextOptions {
	winOptions := &corev1.WindowsSecurityContextOptions{}
	setWindowsOptions(winOptions, credSpecName, credSpecContents)
	return winOptions
}

func setWindowsOptions(winOptions *corev1.WindowsSecurityContextOptions, credSpecName, credSpecContents string) {
	if credSpecName != "" {
		winOptions.GMSACredentialSpecName = &credSpecName
	}
	if credSpecContents != "" {
		winOptions.GMSACredentialSpec = &credSpecContents
	}
}

// buildPod builds a pod for unit tests.
// `podWindowsOptions` should be either a full `*corev1.WindowsSecurityContextOptions` or a string, in which
// case a `*corev1.WindowsSecurityContextOptions` is built using that string as the name of the cred spec to use.
// Same goes for the values of `containerNamesAndWindowsOptions`.
func buildPod(serviceAccountName string, podWindowsOptions *corev1.WindowsSecurityContextOptions, containerNamesAndWindowsOptions map[string]*corev1.WindowsSecurityContextOptions) *corev1.Pod {
	return buildPodWithHostName(serviceAccountName, nil, podWindowsOptions, containerNamesAndWindowsOptions)
}

// buildPod builds a pod for unit tests.
// `podWindowsOptions` should be either a full `*corev1.WindowsSecurityContextOptions` or a string, in which
// case a `*corev1.WindowsSecurityContextOptions` is built using that string as the name of the cred spec to use.
// Same goes for the values of `containerNamesAndWindowsOptions`.
func buildPodWithHostName(serviceAccountName string, hostname *string, podWindowsOptions *corev1.WindowsSecurityContextOptions, containerNamesAndWindowsOptions map[string]*corev1.WindowsSecurityContextOptions) *corev1.Pod {
	containers := make([]corev1.Container, len(containerNamesAndWindowsOptions))
	i := 0
	for name, winOptions := range containerNamesAndWindowsOptions {
		containers[i] = corev1.Container{Name: name}
		if winOptions != nil {
			containers[i].SecurityContext = &corev1.SecurityContext{WindowsOptions: winOptions}
		}
		i++
	}

	shuffleContainers(containers)

	podSpec := corev1.PodSpec{
		ServiceAccountName: serviceAccountName,
		Containers:         containers,
	}

	if hostname != nil {
		podSpec.Hostname = *hostname
	}

	if podWindowsOptions != nil {
		podSpec.SecurityContext = &corev1.PodSecurityContext{WindowsOptions: podWindowsOptions}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: dummyPodName},
		Spec:       podSpec,
	}
}

func shuffleContainers(a []corev1.Container) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(a) - 1; i > 0; i-- {
		j := r.Int() % (i + 1)
		tmp := a[j]
		a[j] = a[i]
		a[i] = tmp
	}
}

func GenerateTestCertAndKey() {
	// Generate a 2048-bit RSA private key
	priv, err := rsa.GenerateKey(crand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	// Create a certificate template
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(crand.Reader, certTemplate, certTemplate, &priv.PublicKey, priv)
	if err != nil {
		panic(err)
	}

	err = os.Mkdir("testdata", 0755)
	if err != nil {
		panic(err)
	}

	// Write the certificate to a PEM file
	certFile, err := os.Create("testdata/cert.pem")
	if err != nil {
		panic(err)
	}
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certFile.Close()

	// Write the private key to a PEM file
	keyFile, err := os.Create("testdata/key.pem")
	if err != nil {
		panic(err)
	}
	keyBytes := x509.MarshalPKCS1PrivateKey(priv)
	pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes})
	keyFile.Close()
}
