package main

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const dummyCredSpecName = "dummy-cred-spec-name"

const dummyCredSpecContents = `{"We don't need no": ["education", "thought control", "dark sarcasm in the classroom"], "All in all you're just another": {"brick": "in", "the": "wall"}}`
const dummyServiceAccoutName = "dummy-service-account-name"
const dummyNamespace = "dummy-namespace"
const dummyContainerName = "dummy-container-name"

type dummyKubeClient struct {
	isAuthorizedToUseCredSpecFunc func(serviceAccountName, namespace, credSpecName string) (authorized bool, reason string)
	retrieveCredSpecContentsFunc  func(credSpecName string) (contents string, httpCode int, err error)
}

func (dkc *dummyKubeClient) isAuthorizedToUseCredSpec(serviceAccountName, namespace, credSpecName string) (authorized bool, reason string) {
	if dkc.isAuthorizedToUseCredSpecFunc != nil {
		return dkc.isAuthorizedToUseCredSpecFunc(serviceAccountName, namespace, credSpecName)
	}
	authorized = true
	return
}

func (dkc *dummyKubeClient) retrieveCredSpecContents(credSpecName string) (contents string, httpCode int, err error) {
	if dkc.retrieveCredSpecContentsFunc != nil {
		return dkc.retrieveCredSpecContentsFunc(credSpecName)
	}
	contents = dummyCredSpecContents
	return
}

func buildPod(annotations map[string]string, serviceAccountName string, containerNames ...string) *corev1.Pod {
	containers := make([]corev1.Container, len(containerNames))
	for i, name := range containerNames {
		containers[i] = corev1.Container{Name: name}
	}

	annotationsCopy := make(map[string]string)
	for k, v := range annotations {
		annotationsCopy[k] = v
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: annotationsCopy,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: serviceAccountName,
			Containers:         containers,
		},
	}
}
