package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// this is just a quick-and-dirty test to ensure the HTTP server works. E2e/integration tests explore
// this much further.
func TestHTTPWebhook(t *testing.T) {
	var requestUID types.UID = "283f4877-34d4-11e9-a9f1-06da3a0adce4"

	pod := buildPod(map[string]string{"pod.alpha.windows.kubernetes.io/gmsa-credential-spec-name": dummyCredSpecName},
		dummyServiceAccoutName, "container-name")

	admissionRequest := &admissionv1beta1.AdmissionReview{
		Request: &admissionv1beta1.AdmissionRequest{
			UID: requestUID,
			Kind: metav1.GroupVersionKind{
				Version: "v1",
				Kind:    "Pod",
			},
			Resource: metav1.GroupVersionResource{
				Version:  "v1",
				Resource: "pods",
			},
			Namespace: dummyNamespace,
			Operation: admissionv1beta1.Create,
			UserInfo: authenticationv1.UserInfo{
				Username: "system:serviceaccount:kube-system:replicaset-controller",
				UID:      "cb335ac0-34b4-11e9-9745-06da3a0adce4",
				Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:kube-system"},
			},
			Object: runtime.RawExtension{
				Object: pod,
			},
		},
	}

	authorizedToUseCredSpec := true

	kubeClient := &dummyKubeClient{
		isAuthorizedToUseCredSpecFunc: func(serviceAccountName, namespace, credSpecName string) (authorized bool, reason string) {
			assert.Equal(t, dummyServiceAccoutName, serviceAccountName)
			assert.Equal(t, dummyNamespace, namespace)
			assert.Equal(t, dummyCredSpecName, credSpecName)

			return authorizedToUseCredSpec, "bogus reason"
		},
		retrieveCredSpecContentsFunc: func(credSpecName string) (contents string, httpCode int, err error) {
			assert.Equal(t, dummyCredSpecName, credSpecName)

			contents = dummyCredSpecContents
			return
		},
	}

	t.Run("success path", func(t *testing.T) {
		port, tearDownHTTP := startHTTPServer(t, kubeClient)
		defer tearDownHTTP()

		httpCode, response := makeHTTPRequest(t, port, "POST", "mutate", admissionRequest)
		assert.Equal(t, http.StatusOK, httpCode)
		require.NotNil(t, response)
		require.NotNil(t, response.Response)

		assert.Equal(t, requestUID, response.Response.UID)
		assert.True(t, response.Response.Allowed)

		if assert.NotNil(t, response.Response.PatchType) {
			assert.Equal(t, admissionv1beta1.PatchTypeJSONPatch, *response.Response.PatchType)
		}

		var patches []map[string]string
		if err := json.Unmarshal(response.Response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, 1, len(patches)) {
			expectedPatch := map[string]string{
				"op":    "add",
				"path":  "/metadata/annotations/pod.alpha.windows.kubernetes.io~1gmsa-credential-spec",
				"value": dummyCredSpecContents,
			}
			assert.Equal(t, expectedPatch, patches[0])
		}
	})

	t.Run("failure", func(t *testing.T) {
		previousAuthorizedToUseCredSpec := authorizedToUseCredSpec
		authorizedToUseCredSpec = false
		defer func() { authorizedToUseCredSpec = previousAuthorizedToUseCredSpec }()

		port, tearDownHTTP := startHTTPServer(t, kubeClient)
		defer tearDownHTTP()

		httpCode, response := makeHTTPRequest(t, port, "POST", "validate", admissionRequest)
		assert.Equal(t, http.StatusOK, httpCode)
		require.NotNil(t, response)
		require.NotNil(t, response.Response)

		assert.Equal(t, requestUID, response.Response.UID)
		assert.False(t, response.Response.Allowed)

		require.NotNil(t, response.Response.Result)
		assert.Equal(t, int32(http.StatusForbidden), response.Response.Result.Code)
		expectedSubstr := fmt.Sprintf("service account %s is not authorized `use` gMSA cred spec", dummyServiceAccoutName)
		assert.Contains(t, response.Response.Result.Message, expectedSubstr)
	})

	for _, path := range []string{"validate", "mutate"} {
		t.Run(fmt.Sprintf("wrong HTTP method for %s", path), func(t *testing.T) {
			port, tearDownHTTP := startHTTPServer(t, kubeClient)
			defer tearDownHTTP()

			httpCode, response := makeHTTPRequest(t, port, "PUT", path, admissionRequest)
			assert.Equal(t, http.StatusMethodNotAllowed, httpCode)
			assert.Nil(t, response)
		})

		t.Run(fmt.Sprintf("wrong content-type for %s", path), func(t *testing.T) {
			port, tearDownHTTP := startHTTPServer(t, kubeClient)
			defer tearDownHTTP()

			httpCode, response := makeHTTPRequest(t, port, "POST", path, admissionRequest, "content-type", "text/plain")
			assert.Equal(t, http.StatusUnsupportedMediaType, httpCode)
			assert.Nil(t, response)
		})

		t.Run(fmt.Sprintf("wrong object kind for %s", path), func(t *testing.T) {
			previousKind := admissionRequest.Request.Kind.Kind
			admissionRequest.Request.Kind.Kind = "Deployment"
			defer func() { admissionRequest.Request.Kind.Kind = previousKind }()

			port, tearDownHTTP := startHTTPServer(t, kubeClient)
			defer tearDownHTTP()

			httpCode, response := makeHTTPRequest(t, port, "POST", path, admissionRequest)
			assert.Equal(t, http.StatusOK, httpCode)
			require.NotNil(t, response)
			require.NotNil(t, response.Response)

			assert.Equal(t, requestUID, response.Response.UID)
			assert.False(t, response.Response.Allowed)

			require.NotNil(t, response.Response.Result)
			assert.Equal(t, int32(http.StatusBadRequest), response.Response.Result.Code)
			assert.Equal(t, "expected a Pod object, got a Deployment", response.Response.Result.Message)
		})
	}

	t.Run("wrong route", func(t *testing.T) {
		port, tearDownHTTP := startHTTPServer(t, kubeClient)
		defer tearDownHTTP()

		httpCode, response := makeHTTPRequest(t, port, "POST", "i_dont_exist", admissionRequest)
		assert.Equal(t, http.StatusNotFound, httpCode)
		assert.Nil(t, response)
	})
}

func startHTTPServer(t *testing.T, kubeClient *dummyKubeClient) (int, func()) {
	webhook := newWebhook(kubeClient)
	port := getAvailablePort(t)

	listeningChan := make(chan interface{})
	go func() {
		assert.Nil(t, webhook.start(port, nil, listeningChan))
	}()

	select {
	case <-listeningChan:
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for HTTP server to start listening on %d", port)
	}

	return port, func() {
		assert.Nil(t, webhook.stop())
	}
}

func makeHTTPRequest(t *testing.T, port int, method string, path string, admissionRequest *admissionv1beta1.AdmissionReview, headers ...string) (httpCode int, admissionResponse *admissionv1beta1.AdmissionReview) {
	require.Equal(t, 0, len(headers)%2, "header names and values should be provided in pairs")

	reqBody, err := json.Marshal(admissionRequest)
	require.Nil(t, err)

	url := fmt.Sprintf("http://localhost:%d/%s", port, path)
	req, err := http.NewRequest(method, url, bytes.NewBuffer(reqBody))
	require.Nil(t, err)

	i := 0
	for i < len(headers) {
		req.Header.Set(headers[i], headers[i+1])
		i += 2
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	require.Nil(t, err)

	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	require.Nil(t, err)

	admissionResponse = &admissionv1beta1.AdmissionReview{}
	if err := json.Unmarshal(respBody, admissionResponse); err != nil {
		admissionResponse = nil
	}

	return resp.StatusCode, admissionResponse
}

// getAvailablePort asks the kernel for an available port, that is ready to use.
func getAvailablePort(t *testing.T) int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	require.Nil(t, err)

	listen, err := net.ListenTCP("tcp", addr)
	require.Nil(t, err)

	defer listen.Close()
	return listen.Addr().(*net.TCPAddr).Port
}
