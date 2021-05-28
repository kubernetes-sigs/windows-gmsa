package main

import (
	"context"
	"math/rand"
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
