package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	admissionV1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type webhookOperation string

//
type gmsaResourceKind string

const (
	contentTypeHeader = "Content-Type"
	jsonContentType   = "application/json"

	validate webhookOperation = "VALIDATE"
	mutate   webhookOperation = "MUTATE"

	podKind       gmsaResourceKind = "pod"
	containerKind gmsaResourceKind = "container"
)

type webhook struct {
	server *http.Server
	client kubeClientInterface
}

type podAdmissionError struct {
	error
	code int
	pod  *corev1.Pod
}

func newWebhook(client kubeClientInterface) *webhook {
	return &webhook{client: client}
}

// start is a blocking call.
// If passed a listeningChan, it will close it when it's started listening
func (webhook *webhook) start(port int, tlsConfig *tlsConfig, listeningChan chan interface{}) error {
	if webhook.server != nil {
		return fmt.Errorf("webhook already started")
	}

	webhook.server = &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: webhook,
	}

	logrus.Infof("starting webhook server at port %v", port)
	listener, err := net.Listen("tcp", webhook.server.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	keepAliveListener := tcpKeepAliveListener{listener.(*net.TCPListener)}

	if listeningChan != nil {
		close(listeningChan)
	}

	if tlsConfig == nil {
		err = webhook.server.Serve(keepAliveListener)
	} else {
		err = webhook.server.ServeTLS(keepAliveListener, tlsConfig.crtPath, tlsConfig.keyPath)
	}

	if err != nil {
		if err == http.ErrServerClosed {
			logrus.Infof("server closed")
		} else {
			return err
		}
	}

	return nil
}

// stop stops the HTTP server.
func (webhook *webhook) stop() error {
	if webhook.server == nil {
		return fmt.Errorf("webhook server not started yet")
	}
	return webhook.server.Shutdown(context.Background())
}

// ServeHTTP makes this object a http.Handler; its job is handling the HTTP routing: paths,
// methods and content-type headers.
// Since we only have a handful of endpoints, there's no need for a full-fledged router here.
func (webhook *webhook) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	var operation webhookOperation

	switch request.URL.Path {
	case "/validate":
		operation = validate
	case "/mutate":
		operation = mutate
	case "/info":
		writeJSONBody(responseWriter, map[string]string{"version": getVersion()})
		return
	case "/health":
		responseWriter.WriteHeader(http.StatusNoContent)
		return
	default:
		abortHTTPRequest(responseWriter, http.StatusNotFound, "received %s request for unknown path %s", request.Method, request.URL.Path)
		return
	}

	// should be a POST request
	if strings.ToUpper(request.Method) != "POST" {
		abortHTTPRequest(responseWriter, http.StatusMethodNotAllowed, "expected POST HTTP request, got a %s %s request", request.Method, operation)
		return
	}
	// verify the content type is JSON
	if contentType := request.Header.Get(contentTypeHeader); contentType != jsonContentType {
		abortHTTPRequest(responseWriter, http.StatusUnsupportedMediaType, "expected JSON content-type header for %s request, got %q", operation, contentType)
		return
	}

	admissionResponse := webhook.httpRequestToAdmissionResponse(request, operation)
	responseAdmissionReview := admissionV1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: admissionResponse,
	}

	writeJSONBody(responseWriter, responseAdmissionReview)
}

// abortHTTPRequest is called for low-level HTTP errors (routing or writing the body)
func abortHTTPRequest(responseWriter http.ResponseWriter, httpCode int, logMsg string, logArs ...interface{}) {
	logrus.Infof(logMsg, logArs...)
	responseWriter.WriteHeader(httpCode)
}

// writeJsonBody writes a JSON object to an HTTP response.
func writeJSONBody(responseWriter http.ResponseWriter, jsonBody interface{}) {
	if responseBytes, err := json.Marshal(jsonBody); err == nil {
		logrus.Debugf("sending response: %s", responseBytes)

		responseWriter.Header().Set(contentTypeHeader, jsonContentType)
		if _, err = responseWriter.Write(responseBytes); err != nil {
			abortHTTPRequest(responseWriter, http.StatusInternalServerError, "error when writing response JSON %s: %v", responseBytes, err)
		}
	} else {
		abortHTTPRequest(responseWriter, http.StatusInternalServerError, "error when marshalling response %v: %v", jsonBody, err)
	}
}

// httpRequestToAdmissionResponse turns a raw HTTP request into an AdmissionResponse struct.
func (webhook *webhook) httpRequestToAdmissionResponse(request *http.Request, operation webhookOperation) *admissionV1.AdmissionResponse {
	// read the body
	if request.Body == nil {
		deniedAdmissionResponse(fmt.Errorf("no request body"), http.StatusBadRequest)
	}
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return deniedAdmissionResponse(fmt.Errorf("couldn't read request body: %v", err), http.StatusBadRequest)
	}
	defer request.Body.Close()

	logrus.Debugf("handling %s request: %s", operation, body)

	// unmarshall the request
	admissionReview := admissionV1.AdmissionReview{}
	if err = json.Unmarshal(body, &admissionReview); err != nil {
		return deniedAdmissionResponse(fmt.Errorf("unable to unmarshall JSON body as an admission review: %v", err), http.StatusBadRequest)
	}
	if admissionReview.Request == nil {
		return deniedAdmissionResponse(fmt.Errorf("no 'Request' field in JSON body"), http.StatusBadRequest)
	}

	admissionResponse, admissionError := webhook.validateOrMutate(request.Context(), admissionReview.Request, operation)
	if admissionError != nil {
		admissionResponse = deniedAdmissionResponse(admissionError)
	}

	// return the same UID
	admissionResponse.UID = admissionReview.Request.UID

	return admissionResponse
}

// validateOrMutate is where the non-HTTP-related work happens.
func (webhook *webhook) validateOrMutate(ctx context.Context, request *admissionV1.AdmissionRequest, operation webhookOperation) (*admissionV1.AdmissionResponse, *podAdmissionError) {
	if request.Kind.Kind != "Pod" {
		return nil, &podAdmissionError{error: fmt.Errorf("expected a Pod object, got a %v", request.Kind.Kind), code: http.StatusBadRequest}
	}

	pod, err := unmarshallPod(request.Object)
	if err != nil {
		return nil, err
	}

	switch request.Operation {
	case admissionV1.Create:
		switch operation {
		case validate:
			return webhook.validateCreateRequest(ctx, pod, request.Namespace)
		case mutate:
			return webhook.mutateCreateRequest(ctx, pod)
		default:
			// shouldn't happen, but needed so that all paths in the function have a return value
			panic(fmt.Errorf("unexpected webhook operation: %v", operation))
		}

	case admissionV1.Update:
		if operation == validate {
			oldPod, err := unmarshallPod(request.OldObject)
			if err != nil {
				return nil, err
			}
			return validateUpdateRequest(pod, oldPod)
		}

		// we only do validation on updates, no mutation
		return &admissionV1.AdmissionResponse{Allowed: true}, nil
	default:
		return nil, &podAdmissionError{error: fmt.Errorf("unpexpected operation %s", request.Operation), pod: pod, code: http.StatusBadRequest}
	}
}

// unmarshallPod unmarshalls a pod object from its raw JSON representation.
func unmarshallPod(object runtime.RawExtension) (*corev1.Pod, *podAdmissionError) {
	pod := &corev1.Pod{}
	if err := json.Unmarshal(object.Raw, pod); err != nil {
		return nil, &podAdmissionError{error: fmt.Errorf("unable to unmarshall pod JSON object: %v", err), code: http.StatusBadRequest}
	}

	return pod, nil
}

// validateCreateRequest ensures that the GMSA contents set in the pod's spec
// match the corresponding GMSA names, and that the pod's service account
// is authorized to `use` the requested GMSA's.
func (webhook *webhook) validateCreateRequest(ctx context.Context, pod *corev1.Pod, namespace string) (*admissionV1.AdmissionResponse, *podAdmissionError) {
	if err := iterateOverWindowsSecurityOptions(pod, func(windowsOptions *corev1.WindowsSecurityContextOptions, resourceKind gmsaResourceKind, resourceName string, _ int) *podAdmissionError {
		if credSpecName := windowsOptions.GMSACredentialSpecName; credSpecName != nil {
			// let's check that the associated service account can read the relevant cred spec CRD
			if authorized, reason := webhook.client.isAuthorizedToUseCredSpec(ctx, pod.Spec.ServiceAccountName, namespace, *credSpecName); !authorized {
				msg := fmt.Sprintf("service account %q is not authorized to `use` GMSA cred spec %q", pod.Spec.ServiceAccountName, *credSpecName)
				if reason != "" {
					msg += fmt.Sprintf(", reason: %q", reason)
				}
				return &podAdmissionError{error: fmt.Errorf(msg), pod: pod, code: http.StatusForbidden}
			}

			// and the contents should match the ones contained in the GMSA resource with that name
			if credSpecContents := windowsOptions.GMSACredentialSpec; credSpecContents != nil {
				if expectedContents, code, retrieveErr := webhook.client.retrieveCredSpecContents(ctx, *credSpecName); retrieveErr != nil {
					return &podAdmissionError{error: retrieveErr, pod: pod, code: code}
				} else if specsEqual, compareErr := compareCredSpecContents(*credSpecContents, expectedContents); !specsEqual || compareErr != nil {
					msg := fmt.Sprintf("the GMSA cred spec contents for %s %q does not match the contents of GMSA resource %q", resourceKind, resourceName, *credSpecName)
					if compareErr != nil {
						msg += fmt.Sprintf(": %v", compareErr)
					}
					return &podAdmissionError{error: fmt.Errorf(msg), pod: pod, code: http.StatusUnprocessableEntity}
				}
			}
		} else if windowsOptions.GMSACredentialSpec != nil {
			// the GMSA's name is not set, but the contents are
			msg := fmt.Sprintf("%s %q has a GMSA cred spec set, but does not define the name of the corresponding resource", resourceKind, resourceName)
			return &podAdmissionError{error: fmt.Errorf(msg), pod: pod, code: http.StatusUnprocessableEntity}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &admissionV1.AdmissionResponse{Allowed: true}, nil
}

// compareCredSpecContents returns true iff the two strings represent the same credential spec contents.
func compareCredSpecContents(fromResource, fromCRD string) (bool, error) {
	// this is actually what happens almost all the time, when users don't set the GMSA contents directly
	// but instead rely on the mutating webhook to do that for them; and in that case no need for a slow
	// JSON parsing and comparison
	if fromResource == fromCRD {
		return true, nil
	}

	var (
		jsonObjectFromResource map[string]interface{}
		jsonObjectFromCRD      map[string]interface{}
	)

	if err := json.Unmarshal([]byte(fromResource), &jsonObjectFromResource); err != nil {
		return false, fmt.Errorf("unable to parse %q as a JSON object: %v", fromResource, err)
	}
	if err := json.Unmarshal([]byte(fromCRD), &jsonObjectFromCRD); err != nil {
		return false, fmt.Errorf("unable to parse CRD %q as a JSON object: %v", fromCRD, err)
	}

	return reflect.DeepEqual(jsonObjectFromResource, jsonObjectFromCRD), nil
}

// mutateCreateRequest inlines the requested GMSA's into the pod's and containers' `WindowsSecurityOptions` structs.
func (webhook *webhook) mutateCreateRequest(ctx context.Context, pod *corev1.Pod) (*admissionV1.AdmissionResponse, *podAdmissionError) {
	var patches []map[string]string

	if err := iterateOverWindowsSecurityOptions(pod, func(windowsOptions *corev1.WindowsSecurityContextOptions, resourceKind gmsaResourceKind, resourceName string, containerIndex int) *podAdmissionError {
		if credSpecName := windowsOptions.GMSACredentialSpecName; credSpecName != nil {
			// if the user has pre-set the GMSA's contents, we won't override it - it'll be down
			// to the validation endpoint to make sure the contents actually are what they should
			if credSpecContents := windowsOptions.GMSACredentialSpec; credSpecContents == nil {
				contents, code, retrieveErr := webhook.client.retrieveCredSpecContents(ctx, *credSpecName)
				if retrieveErr != nil {
					return &podAdmissionError{error: retrieveErr, pod: pod, code: code}
				}

				partialPath := ""
				if resourceKind == containerKind {
					partialPath = fmt.Sprintf("/containers/%d", containerIndex)
				}

				// worth noting that this JSON patch is guaranteed to work since we know at this point
				// that the resource comprises a `windowsOptions` object, and and that it doesn't have a
				// "gmsaCredentialSpec" field
				patches = append(patches, map[string]string{
					"op":    "add",
					"path":  fmt.Sprintf("/spec%s/securityContext/windowsOptions/gmsaCredentialSpec", partialPath),
					"value": contents,
				})
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	admissionResponse := &admissionV1.AdmissionResponse{Allowed: true}

	if len(patches) != 0 {
		patchesBytes, err := json.Marshal(patches)
		if err != nil {
			return nil, &podAdmissionError{error: fmt.Errorf("unable to marshall patch JSON %v: %v", patches, err), pod: pod, code: http.StatusInternalServerError}
		}

		admissionResponse.Patch = patchesBytes
		patchType := admissionV1.PatchTypeJSONPatch
		admissionResponse.PatchType = &patchType
	}

	return admissionResponse, nil
}

// validateUpdateRequest ensures that there are no updates to any of the GMSA names or contents.
func validateUpdateRequest(pod, oldPod *corev1.Pod) (*admissionV1.AdmissionResponse, *podAdmissionError) {
	var oldPodContainerOptions map[string]*corev1.WindowsSecurityContextOptions

	if err := iterateOverWindowsSecurityOptions(pod, func(windowsOptions *corev1.WindowsSecurityContextOptions, resourceKind gmsaResourceKind, resourceName string, _ int) *podAdmissionError {
		var oldWindowsOptions *corev1.WindowsSecurityContextOptions
		if resourceKind == podKind {
			if oldPod.Spec.SecurityContext != nil {
				oldWindowsOptions = oldPod.Spec.SecurityContext.WindowsOptions
			}
		} else {
			// it's a container; look for the same container in the old pod,
			// lazily building the map of container names to security options if needed
			if oldPodContainerOptions == nil {
				oldPodContainerOptions = make(map[string]*corev1.WindowsSecurityContextOptions)
				iterateOverWindowsSecurityOptions(oldPod, func(winOpts *corev1.WindowsSecurityContextOptions, rsrcKind gmsaResourceKind, rsrcName string, _ int) *podAdmissionError {
					if rsrcKind == containerKind {
						oldPodContainerOptions[rsrcName] = winOpts
					}
					return nil
				})
			}

			oldWindowsOptions = oldPodContainerOptions[resourceName]
		}

		if oldWindowsOptions == nil {
			oldWindowsOptions = &corev1.WindowsSecurityContextOptions{}
		}

		var modifiedFieldNames []string
		if !equalStringPointers(windowsOptions.GMSACredentialSpecName, oldWindowsOptions.GMSACredentialSpecName) {
			modifiedFieldNames = append(modifiedFieldNames, "name")
		}
		if !equalStringPointers(windowsOptions.GMSACredentialSpec, oldWindowsOptions.GMSACredentialSpec) {
			modifiedFieldNames = append(modifiedFieldNames, "contents")
		}

		if len(modifiedFieldNames) != 0 {
			msg := fmt.Errorf("cannot update an existing pod's GMSA settings (GMSA %s modified on %s %q)", strings.Join(modifiedFieldNames, " and "), resourceKind, resourceName)
			return &podAdmissionError{error: msg, pod: pod, code: http.StatusForbidden}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &admissionV1.AdmissionResponse{Allowed: true}, nil
}

func equalStringPointers(s1, s2 *string) bool {
	if s1 == nil {
		return s2 == nil
	}
	if s2 == nil {
		return false
	}
	return *s1 == *s2
}

// iterateOverWindowsSecurityOptions calls `f` on the pod's `.Spec.SecurityContext.WindowsOptions` field,
// as well as over each of its container's `.SecurityContext.WindowsOptions` field.
// `f` can assume it only gets called with non-nil `WindowsSecurityOptions` pointers; the other
// arguments give information on the resource owning that pointer - in particular, if that
// resource is a container, `containerIndex` is the index of the container in the spec's list (-1 for pods).
// If `f` returns an error, that breaks the loop, and the error is bubbled up.
func iterateOverWindowsSecurityOptions(pod *corev1.Pod, f func(windowsOptions *corev1.WindowsSecurityContextOptions, resourceKind gmsaResourceKind, resourceName string, containerIndex int) *podAdmissionError) *podAdmissionError {
	if pod.Spec.SecurityContext != nil && pod.Spec.SecurityContext.WindowsOptions != nil {
		if err := f(pod.Spec.SecurityContext.WindowsOptions, podKind, pod.Name, -1); err != nil {
			return err
		}
	}

	for i, container := range pod.Spec.Containers {
		if container.SecurityContext != nil && container.SecurityContext.WindowsOptions != nil {
			if err := f(container.SecurityContext.WindowsOptions, containerKind, container.Name, i); err != nil {
				return err
			}
		}
	}

	return nil
}

// deniedAdmissionResponse is a helper function to create an AdmissionResponse
// with an embedded error.
func deniedAdmissionResponse(err error, httpCode ...int) *admissionV1.AdmissionResponse {
	var code int
	logMsg := "refusing to admit"

	if admissionError, ok := err.(*podAdmissionError); ok {
		code = admissionError.code
		if admissionError.pod != nil {
			logMsg += fmt.Sprintf(" pod %+v", admissionError.pod)
		}
	}

	if len(httpCode) > 0 {
		code = httpCode[0]
	}

	if code != 0 {
		logMsg += fmt.Sprintf(" with code %v", code)
	}

	logrus.Infof("%s: %v", logMsg, err)

	return &admissionV1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: err.Error(),
			Code:    int32(code),
		},
	}
}

// stolen from https://github.com/golang/go/blob/go1.12/src/net/http/server.go#L3255-L3271
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}
