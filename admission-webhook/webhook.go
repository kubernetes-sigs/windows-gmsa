package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// gMSAContainerSpecContentsAnnotationKeySuffix is the suffix of the pod annotation where we store
	// the contents of the GMSA credential spec for a given container (the full annotation being
	// the container's name with this suffix appended).
	gMSAContainerSpecContentsAnnotationKeySuffix = ".container.alpha.windows.kubernetes.io/gmsa-credential-spec"
	// gMSAPodSpecContentsAnnotationKey is the pod annotation where we store the contents of the GMSA
	// credential spec to use for containers that do not have their own specific GMSA cred spec set via a
	// gMSAContainerSpecContentsAnnotationKeySuffix annotation as explained above
	gMSAPodSpecContentsAnnotationKey = "pod.alpha.windows.kubernetes.io/gmsa-credential-spec"

	// gMSAContainerSpecNameAnnotationKeySuffix is the suffix of the pod annotation used
	// to give the name of the GMSA credential spec for a given container (the full annotation
	// being the container's name with this suffix appended).
	gMSAContainerSpecNameAnnotationKeySuffix = gMSAContainerSpecContentsAnnotationKeySuffix + "-name"
	// gMSAPodSpecNameAnnotationKey is the pod annotation used to give the name of the GMSA
	// credential spec for containers that do not have their own specific GMSA cred spec name
	// set via a gMSAContainerSpecNameAnnotationKeySuffix annotation as explained above
	gMSAPodSpecNameAnnotationKey = gMSAPodSpecContentsAnnotationKey + "-name"
)

type webhook struct {
	server *http.Server
	client kubeClientInterface
}

type webhookOperation string

const (
	validate webhookOperation = "VALIDATE"
	mutate   webhookOperation = "MUTATE"
)

type podAdmissionError struct {
	error
	code int
	pod  *corev1.Pod
}

func newWebhook(client kubeClientInterface) *webhook {
	return &webhook{client: client}
}

// start is a blocking call.
func (webhook *webhook) start(port int, tlsConfig *tlsConfig) error {
	if webhook.server != nil {
		return fmt.Errorf("webhook already started")
	}

	webhook.server = &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: webhook,
	}

	logrus.Infof("starting webhook server at port %v", port)
	var err error
	if tlsConfig == nil {
		err = webhook.server.ListenAndServe()
	} else {
		err = webhook.server.ListenAndServeTLS(tlsConfig.crtPath, tlsConfig.keyPath)
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
// Since we only have a couple of endpoints, there's no need for a full-fledged router here.
func (webhook *webhook) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	var operation webhookOperation

	switch request.URL.Path {
	case "/validate":
		operation = validate
	case "/mutate":
		operation = mutate
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
	if contentType := request.Header.Get("Content-Type"); contentType != "application/json" {
		abortHTTPRequest(responseWriter, http.StatusUnsupportedMediaType, "expected JSON content-type header for %s request, got %q", operation, contentType)
		return
	}

	admissionResponse := webhook.httpRequestToAdmissionResponse(request, operation)
	responseAdmissionReview := admissionv1beta1.AdmissionReview{Response: admissionResponse}
	if responseBytes, err := json.Marshal(responseAdmissionReview); err == nil {
		logrus.Debugf("sending response: %s", responseBytes)

		if _, err = responseWriter.Write(responseBytes); err != nil {
			abortHTTPRequest(responseWriter, http.StatusInternalServerError, "error when writing response JSON %s: %v", responseBytes, err)
		}
	} else {
		abortHTTPRequest(responseWriter, http.StatusInternalServerError, "error when marshalling response %v: %v", responseAdmissionReview, err)
	}
}

// abortHTTPRequest is called for low-level HTTP errors (routing or writing the body)
func abortHTTPRequest(responseWriter http.ResponseWriter, httpCode int, logMsg string, logArs ...interface{}) {
	logrus.Infof(logMsg, logArs...)
	responseWriter.WriteHeader(httpCode)
}

// httpRequestToAdmissionResponse turns a raw HTTP request into an AdmissionResponse struct.
func (webhook *webhook) httpRequestToAdmissionResponse(request *http.Request, operation webhookOperation) *admissionv1beta1.AdmissionResponse {
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
	admissionReview := admissionv1beta1.AdmissionReview{}
	if err = json.Unmarshal(body, &admissionReview); err != nil {
		return deniedAdmissionResponse(fmt.Errorf("unable to unmarshall JSON body as an admission review: %v", err), http.StatusBadRequest)
	}
	if admissionReview.Request == nil {
		return deniedAdmissionResponse(fmt.Errorf("no 'Request' field in JSON body"), http.StatusBadRequest)
	}

	admissionResponse, admissionError := webhook.validateOrMutate(admissionReview.Request, operation)
	if admissionError != nil {
		admissionResponse = deniedAdmissionResponse(admissionError)
	}

	// return the same UID
	admissionResponse.UID = admissionReview.Request.UID

	return admissionResponse
}

// validateOrMutate is where the non-HTTP-related work happens.
func (webhook *webhook) validateOrMutate(request *admissionv1beta1.AdmissionRequest, operation webhookOperation) (*admissionv1beta1.AdmissionResponse, *podAdmissionError) {
	if request.Kind.Kind != "Pod" {
		return nil, &podAdmissionError{error: fmt.Errorf("expected a Pod object, got a %v", request.Kind.Kind), code: http.StatusBadRequest}
	}

	pod, err := unmarshallPod(request.Object)
	if err != nil {
		return nil, err
	}

	switch request.Operation {
	case admissionv1beta1.Create:
		switch operation {
		case validate:
			return webhook.validateCreateRequest(pod, request.Namespace)
		case mutate:
			return webhook.mutateCreateRequest(pod)
		default:
			// shouldn't happen, but needed so that all paths in the function have a return value
			panic(fmt.Errorf("unexpected webhook operation: %v", operation))
		}

	case admissionv1beta1.Update:
		if operation == validate {
			oldPod, err := unmarshallPod(request.OldObject)
			if err != nil {
				return nil, err
			}
			return validateUpdateRequest(pod, oldPod)
		}

		// we only do validation on updates, no mutation
		return &admissionv1beta1.AdmissionResponse{Allowed: true}, nil
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

// validateCreateRequest ensures that the only GMSA content annotations set on the pod,
// match the corresponding GMSA name annotations, and that the pod's service account
// is authorized to `use` the requested GMSA's.
func (webhook *webhook) validateCreateRequest(pod *corev1.Pod, namespace string) (*admissionv1beta1.AdmissionResponse, *podAdmissionError) {
	var err *podAdmissionError

	iterateOverGMSAAnnotationPairs(pod, func(nameKey, contentsKey string) {
		if err != nil {
			return
		}

		if credSpecName, present := pod.Annotations[nameKey]; present && credSpecName != "" {
			// let's check that the associated service account can read the relevant cred spec CRD
			if authorized, reason := webhook.client.isAuthorizedToUseCredSpec(pod.Spec.ServiceAccountName, namespace, credSpecName); !authorized {
				msg := fmt.Sprintf("service account %s is not authorized `use` gMSA cred spec %s", pod.Spec.ServiceAccountName, credSpecName)
				if reason != "" {
					msg += fmt.Sprintf(", reason: %q", reason)
				}
				err = &podAdmissionError{error: fmt.Errorf(msg), pod: pod, code: http.StatusForbidden}
				return
			}

			// and the content annotation should contain the expected cred spec
			if credSpecContents, present := pod.Annotations[contentsKey]; present {
				if expectedContents, code, retrieveErr := webhook.client.retrieveCredSpecContents(credSpecName); retrieveErr != nil {
					err = &podAdmissionError{error: retrieveErr, pod: pod, code: code}
					return
				} else if specsEqual, compareErr := compareCredSpecContents(credSpecContents, expectedContents); !specsEqual || compareErr != nil {
					msg := fmt.Sprintf("cred spec contained in annotation %q does not match the contents of GMSA %q", contentsKey, credSpecName)
					if compareErr != nil {
						msg += fmt.Sprintf(": %v", compareErr)
					}
					err = &podAdmissionError{error: fmt.Errorf(msg), pod: pod, code: http.StatusForbidden}
					return
				}
			}
		} else if _, present := pod.Annotations[contentsKey]; present {
			// the name annotation is not present, but the content one is
			err = &podAdmissionError{error: fmt.Errorf("cannot pre-set a pod's gMSA content annotation (annotation %v present)", contentsKey), pod: pod, code: http.StatusForbidden}
			return
		}
	})
	if err != nil {
		return nil, err
	}

	return &admissionv1beta1.AdmissionResponse{Allowed: true}, nil
}

// compareCredSpecContents returns true iff the two strings represent the same credential spec contents.
func compareCredSpecContents(fromAnnotation, fromCRD string) (bool, error) {
	// this is actually what happens almost all the time, when users don't set the contents annotation directly
	// but instead rely on the mutating webhook to do that for them; and in that case no need for a slow
	// JSON parsing and comparison
	if fromAnnotation == fromCRD {
		return true, nil
	}

	var (
		jsonObjectFromAnnotation map[string]interface{}
		jsonObjectFromCRD        map[string]interface{}
	)

	if err := json.Unmarshal([]byte(fromAnnotation), &jsonObjectFromAnnotation); err != nil {
		return false, fmt.Errorf("unable to parse annotation %q as a JSON object: %v", fromAnnotation, err)
	}
	if err := json.Unmarshal([]byte(fromCRD), &jsonObjectFromCRD); err != nil {
		return false, fmt.Errorf("unable to parse CRD %q as a JSON object: %v", fromCRD, err)
	}

	return reflect.DeepEqual(jsonObjectFromAnnotation, jsonObjectFromCRD), nil
}

// mutateCreateRequest inlines the requested GMSA's into the pod's spec as annotations.
func (webhook *webhook) mutateCreateRequest(pod *corev1.Pod) (*admissionv1beta1.AdmissionResponse, *podAdmissionError) {
	var (
		patches []map[string]string
		err     *podAdmissionError
	)

	iterateOverGMSAAnnotationPairs(pod, func(nameKey, contentsKey string) {
		if err != nil {
			return
		}

		credSpecName, nameAnnotationPresent := pod.Annotations[nameKey]
		_, contentsAnnotationPresent := pod.Annotations[contentsKey]

		if nameAnnotationPresent && credSpecName != "" {
			// if the user has pre-set the contents annotation, we won't override it - it'll be down to the validation
			// endpoint to make sure the contents actually are what they should
			if !contentsAnnotationPresent {
				contents, code, retrieveErr := webhook.client.retrieveCredSpecContents(credSpecName)
				if retrieveErr != nil {
					err = &podAdmissionError{error: retrieveErr, pod: pod, code: code}
					return
				}

				// worth noting that this JSON patch is guaranteed to work since we know at this point
				// that the pod has annotations, and and that it doesn't have this specific one
				patches = append(patches, map[string]string{
					"op":    "add",
					"path":  fmt.Sprintf("/metadata/annotations/%s", jsonPatchEscape(contentsKey)),
					"value": contents,
				})
			}
		} else if contentsAnnotationPresent {
			// the name annotation is not present, but the content one is
			msg := fmt.Sprintf("cannot pre-set a pod's gMSA content annotation (annotation %q present) without setting the corresponding name annotation %q", contentsKey, nameKey)
			err = &podAdmissionError{error: fmt.Errorf(msg), pod: pod, code: http.StatusForbidden}
			return
		}
	})
	if err != nil {
		return nil, err
	}

	admissionResponse := &admissionv1beta1.AdmissionResponse{Allowed: true}

	if len(patches) != 0 {
		patchesBytes, err := json.Marshal(patches)
		if err != nil {
			return nil, &podAdmissionError{error: fmt.Errorf("unable to marshall patch JSON %v: %v", patches, err), pod: pod, code: http.StatusInternalServerError}
		}

		admissionResponse.Patch = patchesBytes
		patchType := admissionv1beta1.PatchTypeJSONPatch
		admissionResponse.PatchType = &patchType
	}

	return admissionResponse, nil
}

// see jsonPatchEscape below
var jsonPatchEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// jsonPatchEscape complies with JSON Patch's way of escaping special characters
// in key names. See https://tools.ietf.org/html/rfc6901#section-3
func jsonPatchEscape(s string) string {
	return jsonPatchEscaper.Replace(s)
}

// validateUpdateRequest ensures that there are no updates to any of the GMSA annotations.
func validateUpdateRequest(pod, oldPod *corev1.Pod) (*admissionv1beta1.AdmissionResponse, *podAdmissionError) {
	var err *podAdmissionError

	iterateOverGMSAAnnotationPairs(pod, func(nameKey, contentsKey string) {
		if err != nil {
			return
		}
		if err = assertAnnotationsUnchanged(pod, oldPod, nameKey); err != nil {
			return
		}
		if err = assertAnnotationsUnchanged(pod, oldPod, contentsKey); err != nil {
			return
		}
	})
	if err != nil {
		return nil, err
	}

	return &admissionv1beta1.AdmissionResponse{Allowed: true}, nil
}

// assertAnnotationsUnchanged returns an error if the two pods don't have the same annotation for the given key.
func assertAnnotationsUnchanged(pod, oldPod *corev1.Pod, key string) *podAdmissionError {
	if pod.Annotations[key] != oldPod.Annotations[key] {
		return &podAdmissionError{
			error: fmt.Errorf("cannot update an existing pod's gMSA annotation (annotation %v changed)", key),
			pod:   pod,
			code:  http.StatusForbidden,
		}
	}
	return nil
}

// iterateOverGMSAAnnotationPairs calls `f` on the successive pairs of GMSA name and contents
// annotation keys.
func iterateOverGMSAAnnotationPairs(pod *corev1.Pod, f func(nameKey, contentsKey string)) {
	f(gMSAPodSpecNameAnnotationKey, gMSAPodSpecContentsAnnotationKey)
	for _, container := range pod.Spec.Containers {
		f(container.Name+gMSAContainerSpecNameAnnotationKeySuffix, container.Name+gMSAContainerSpecContentsAnnotationKeySuffix)
	}
}

// deniedAdmissionResponse is a helper function to create an AdmissionResponse
// with an embedded error.
func deniedAdmissionResponse(err error, httpCode ...int) *admissionv1beta1.AdmissionResponse {
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

	return &admissionv1beta1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: err.Error(),
			Code:    int32(code),
		},
	}
}
