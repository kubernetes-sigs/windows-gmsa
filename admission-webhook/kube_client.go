package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// these 3 constants are the coordinates of the Custom Resource Definition
	crdAPIGroup     = "windows.k8s.io"
	crdAPIVersion   = "v1"
	crdResourceName = "gmsacredentialspecs"

	// crdContentsField is the single field that's expected to be defined in a GMSA CRD,
	// and to contain the contents of the cred spec itself
	crdContentsField = "credspec"

	// notFound is used in `isNotFoundError` below
	notFound = "not found"
)

// kubeClient centralizes all the operations we need when talking to k8s
type kubeClient struct {
	coreClient    kubernetes.Interface
	dynamicClient dynamic.Interface
}

func newKubeClient(config *rest.Config) (*kubeClient, error) {
	coreClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &kubeClient{
		coreClient:    coreClient,
		dynamicClient: dynamicClient,
	}, nil
}

// isAuthorizedToReadConfigMap checks whether a given service account is authorized to `use` a given cred spec.
// If it denies the request, it also returns a string explaining why.
func (kc *kubeClient) isAuthorizedToUseCredSpec(ctx context.Context, serviceAccountName, namespace, credSpecName string) (bool, string) {
	serviceAccountUserInfo := serviceaccount.UserInfo(namespace, serviceAccountName, "")

	// needed to cast `authorizationv1.ExtraValue` to `[]string`
	var extra map[string]authorizationv1.ExtraValue
	for k, v := range serviceAccountUserInfo.GetExtra() {
		extra[k] = v
	}

	subjectAccessReview := authorizationv1.LocalSubjectAccessReview{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: authorizationv1.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "use",
				Group:     crdAPIGroup,
				Version:   crdAPIVersion,
				Resource:  crdResourceName,
				Name:      credSpecName,
			},
			User:   serviceAccountUserInfo.GetName(),
			Groups: serviceAccountUserInfo.GetGroups(),
			UID:    serviceAccountUserInfo.GetUID(),
			Extra:  extra,
		},
	}

	response, err := kc.coreClient.AuthorizationV1().LocalSubjectAccessReviews(namespace).Create(ctx, &subjectAccessReview, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Sprintf("error when checking authz access: %v", err.Error())
	}
	return response.Status.Allowed && !response.Status.Denied, response.Status.Reason
}

// retrieveCredSpecContents fetches the actual contents of a cred spec.
// If it returns an error, it also returns the corresponding HTTP code.
func (kc *kubeClient) retrieveCredSpecContents(ctx context.Context, credSpecName string) (string, int, error) {
	resource := schema.GroupVersionResource{
		Group:    crdAPIGroup,
		Version:  crdAPIVersion,
		Resource: crdResourceName,
	}
	credSpec, err := kc.dynamicClient.Resource(resource).Get(ctx, credSpecName, metav1.GetOptions{})
	if err != nil {
		if isNotFoundError(err) {
			return "", http.StatusNotFound, fmt.Errorf("cred spec %s does not exist", credSpecName)
		}
		return "", http.StatusInternalServerError, fmt.Errorf("unable to retrieve the contents of cred spec %s: %v", credSpecName, err)
	}

	if contents, present := credSpec.Object[crdContentsField]; !present || contents == "" {
		return "", http.StatusExpectationFailed, fmt.Errorf("cred spec %s does not have a %s key", credSpecName, crdContentsField)
	}

	contentsBytes, err := json.Marshal(credSpec.Object[crdContentsField])
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("unable to marshall cred spec %s into a JSON: %v", credSpecName, err)
	}

	return string(contentsBytes), http.StatusOK, nil
}

// isNotFoundError returns true if the error indicates "not found".  It parses
// the error string looking for known values, which is imperfect but works in
// practice; and there's not much better we can do right now with k8s' dynamic client API
func isNotFoundError(err error) bool {
	msg := err.Error()
	return msg[len(msg)-len(notFound):] == notFound
}
