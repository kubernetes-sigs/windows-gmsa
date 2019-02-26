package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

func TestValidateCreateRequest(t *testing.T) {
	t.Run("with no GMSA annotations, it passes", func(t *testing.T) {
		webhook := newWebhook(nil)
		pod := buildPod(nil, dummyServiceAccoutName, dummyContainerName)

		response, err := webhook.validateCreateRequest(pod, dummyNamespace)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
	})

	kubeClientFactory := func() *dummyKubeClient {
		return &dummyKubeClient{
			retrieveCredSpecContentsFunc: func(credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					contents = dummyCredSpecContents
				} else {
					contents = credSpecName + "-contents"
				}
				return
			},
		}
	}

	annotationsFactory := func(containerName string) map[string]string {
		return map[string]string{
			containerName + ".container.alpha.windows.kubernetes.io/gmsa-credential-spec-name": containerName + "-cred-spec",
			containerName + ".container.alpha.windows.kubernetes.io/gmsa-credential-spec":      containerName + "-cred-spec-contents",
		}
	}

	runWebhookValidateOrMutateTests(t, annotationsFactory, map[string]webhookValidateOrMutateTest{
		"with a correct annotation pair, it passes": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())

			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = dummyCredSpecContents

			response, err := webhook.validateCreateRequest(pod, dummyNamespace)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		},

		"if the cred spec contents are not byte-to-byte equal to that of the one named, but still represent equivalent JSONs, it passes": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())

			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = `{"All in all you're just another":      {"the":"wall","brick":   "in"},"We don't need no":["education", "thought control","dark sarcasm in the classroom"]}`

			response, err := webhook.validateCreateRequest(pod, dummyNamespace)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		},

		"if the cred spec contents are not that of the one named, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())

			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = `{"We don't need no": ["money"], "All in all you're just another": {"brick": "in", "the": "wall"}}`

			response, err := webhook.validateCreateRequest(pod, dummyNamespace)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cred spec contained in annotation %q does not match the contents of GMSA %q",
				contentsKey, dummyCredSpecName)
		},

		"if the cred spec contents are not byte-to-byte equal to that of the one named, and are not even a valid JSON object, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())

			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = "i ain't no JSON object"

			response, err := webhook.validateCreateRequest(pod, dummyNamespace)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cred spec contained in annotation %q does not match the contents of GMSA %q",
				contentsKey, dummyCredSpecName)
		},

		"if the contents annotation is set, but the name one isn't, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())
			pod.Annotations[contentsKey] = dummyCredSpecContents

			response, err := webhook.validateCreateRequest(pod, dummyNamespace)

			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cannot pre-set a pod's gMSA content annotation (annotation %s present)", contentsKey)
		},

		"if the service account is not authorized to use the cred-spec, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			dummyReason := "dummy reason"

			client := kubeClientFactory()
			client.isAuthorizedToUseCredSpecFunc = func(serviceAccountName, namespace, credSpecName string) (authorized bool, reason string) {
				if credSpecName == dummyCredSpecName {
					assert.Equal(t, dummyServiceAccoutName, serviceAccountName)
					assert.Equal(t, dummyNamespace, namespace)

					return false, dummyReason
				}

				return true, ""
			}

			webhook := newWebhook(client)

			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = dummyCredSpecContents

			response, err := webhook.validateCreateRequest(pod, dummyNamespace)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"service account %s is not authorized `use` gMSA cred spec %s, reason: %q",
				dummyServiceAccoutName, dummyCredSpecName, dummyReason)
		},

		"if there is an error when retrieving the cred-spec's contents, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			dummyError := fmt.Errorf("dummy error")

			client := kubeClientFactory()
			previousRetrieveCredSpecContentsFunc := client.retrieveCredSpecContentsFunc
			client.retrieveCredSpecContentsFunc = func(credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					return "", http.StatusNotFound, dummyError
				}
				return previousRetrieveCredSpecContentsFunc(credSpecName)
			}

			webhook := newWebhook(client)

			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = dummyCredSpecContents

			response, err := webhook.validateCreateRequest(pod, dummyNamespace)

			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusNotFound, dummyError.Error())
		},
	})
}

func TestMutateCreateRequest(t *testing.T) {
	t.Run("with no GMSA annotations, it passes and does nothing", func(t *testing.T) {
		webhook := newWebhook(nil)
		pod := buildPod(nil, dummyServiceAccoutName, dummyContainerName)

		response, err := webhook.validateCreateRequest(pod, dummyNamespace)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
	})

	kubeClientFactory := func() *dummyKubeClient {
		return &dummyKubeClient{
			retrieveCredSpecContentsFunc: func(credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					contents = dummyCredSpecContents
				} else {
					contents = credSpecName + "-contents"
				}
				return
			},
		}
	}

	annotationsFactory := func(containerName string) map[string]string {
		return map[string]string{
			containerName + ".container.alpha.windows.kubernetes.io/gmsa-credential-spec-name": containerName + "-cred-spec",
		}
	}

	runWebhookValidateOrMutateTests(t, annotationsFactory, map[string]webhookValidateOrMutateTest{
		"with a GMSA name annotation, it passes and inlines the cred-spec's contents": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())

			pod.Annotations[nameKey] = dummyCredSpecName

			response, err := webhook.mutateCreateRequest(pod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)

			if assert.NotNil(t, response.PatchType) {
				assert.Equal(t, admissionv1beta1.PatchTypeJSONPatch, *response.PatchType)
			}

			// maps the contents to the expected patch for that container
			expectedPatches := make(map[string]map[string]string)
			for i := 0; i < len(pod.Spec.Containers)-1; i++ {
				credSpecContents := extraContainerName(i) + "-cred-spec-contents"
				expectedPatches[credSpecContents] = map[string]string{
					"op":    "add",
					"path":  fmt.Sprintf("/metadata/annotations/%s.container.alpha.windows.kubernetes.io~1gmsa-credential-spec", extraContainerName(i)),
					"value": credSpecContents,
				}
			}
			// and the patch for this test's specific cred spec
			expectedPatches[dummyCredSpecContents] = map[string]string{
				"op":    "add",
				"path":  fmt.Sprintf("/metadata/annotations/%s", jsonPatchEscape(contentsKey)),
				"value": dummyCredSpecContents,
			}

			var patches []map[string]string
			if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, len(pod.Spec.Containers), len(patches)) {
				for _, patch := range patches {
					if value, hasValue := patch["value"]; assert.True(t, hasValue) {
						if expectedPatch, present := expectedPatches[value]; assert.True(t, present, "value %s not found in expected patches", value) {
							assert.Equal(t, expectedPatch, patch)
						}
					}
				}
			}
		},

		"if the contents annotation is already set, but the name one isn't, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())

			pod.Annotations[contentsKey] = dummyCredSpecContents

			response, err := webhook.mutateCreateRequest(pod)

			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cannot pre-set a pod's gMSA content annotation (annotation %q present) without setting the corresponding name annotation", contentsKey)
		},

		"it the contents annotation is already set, along with the name one, it passes and doesn't overwrite the provided contents": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			webhook := newWebhook(kubeClientFactory())

			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = `{"pre-set GSA": "cred contents"}`

			response, err := webhook.mutateCreateRequest(pod)
			assert.Nil(t, err)

			// all the patches we receive should be for the extra containers
			expectedPatchesLen := len(pod.Spec.Containers) - 1

			if expectedPatchesLen == 0 {
				assert.Nil(t, response.PatchType)
				assert.Nil(t, response.Patch)
			} else {
				var patches []map[string]string
				if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, expectedPatchesLen, len(patches)) {
					for _, patch := range patches {
						if path, hasPath := patch["path"]; assert.True(t, hasPath) {
							assert.NotContains(t, path, dummyCredSpecName)
						}
					}
				}
			}
		},

		"if there is an error when retrieving the cred-spec's contents, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			dummyError := fmt.Errorf("dummy error")

			client := kubeClientFactory()
			previousRetrieveCredSpecContentsFunc := client.retrieveCredSpecContentsFunc
			client.retrieveCredSpecContentsFunc = func(credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					return "", http.StatusNotFound, dummyError
				}
				return previousRetrieveCredSpecContentsFunc(credSpecName)
			}

			webhook := newWebhook(client)

			pod.Annotations[nameKey] = dummyCredSpecName

			response, err := webhook.mutateCreateRequest(pod)

			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusNotFound, dummyError.Error())
		},
	})
}

func TestValidateUpdateRequest(t *testing.T) {
	t.Run("with no GMSA annotations, it passes and does nothing", func(t *testing.T) {
		pod := buildPod(nil, dummyServiceAccoutName, dummyContainerName)
		oldPod := buildPod(nil, dummyServiceAccoutName, dummyContainerName)

		response, err := validateUpdateRequest(pod, oldPod)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
	})

	annotationsFactory := func(containerName string) map[string]string {
		return map[string]string{
			containerName + ".container.alpha.windows.kubernetes.io/gmsa-credential-spec-name": containerName + "-cred-spec",
			containerName + ".container.alpha.windows.kubernetes.io/gmsa-credential-spec":      containerName + "-cred-spec-contents",
		}
	}

	runWebhookValidateOrMutateTests(t, annotationsFactory, map[string]webhookValidateOrMutateTest{
		"if no GMSA annotation has changed, it passes": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = dummyCredSpecContents

			oldPod := pod.DeepCopy()

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		},

		"if a name annotation has changed, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			pod.Annotations[nameKey] = "new-cred-spec-name"
			pod.Annotations[contentsKey] = dummyCredSpecContents

			oldPod := pod.DeepCopy()
			oldPod.Annotations[nameKey] = dummyCredSpecName

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cannot update an existing pod's gMSA annotation (annotation %s changed)", nameKey)
		},

		"if a contents annotation has changed, it fails": func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string) {
			pod.Annotations[nameKey] = dummyCredSpecName
			pod.Annotations[contentsKey] = "new-cred-spec-contents"

			oldPod := pod.DeepCopy()
			oldPod.Annotations[contentsKey] = dummyCredSpecContents

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cannot update an existing pod's gMSA annotation (annotation %s changed)", contentsKey)
		},
	})
}

func TestJsonPatchEscape(t *testing.T) {
	assert.Equal(t, "foobar", jsonPatchEscape("foobar"))
	assert.Equal(t, "f~0~0oob~0ar", jsonPatchEscape("f~~oob~ar"))
	assert.Equal(t, "foo~1bar~1~1", jsonPatchEscape("foo/bar//"))
	assert.Equal(t, "~0fo~0~1o~1~0ba~1~1~0~0r~1", jsonPatchEscape("~fo~/o/~ba//~~r/"))
}

/* Helpers below */

type annotationsFactory func(containerName string) map[string]string

// a webhookValidateOrMutateTest function should run a test on one of the webhook's validate or mutate
// functions, given the pair of labels it can play with.
// It should assume that the pod it receives has any number of extra containers with correct
// (in the sense of the test) annotations generated by a relevant annotationsFactory
type webhookValidateOrMutateTest func(t *testing.T, pod *corev1.Pod, nameKey, contentsKey string)

// runWebhookValidateOrMutateTests runs the given tests with 0 to 5 extra containers with correct tags
// as generated by the given annotationsFactory
func runWebhookValidateOrMutateTests(t *testing.T, annotationsFactory annotationsFactory, tests map[string]webhookValidateOrMutateTest) {
	for extraContainersCount := 0; extraContainersCount <= 5; extraContainersCount++ {
		containersNames := make([]string, extraContainersCount+1)
		containersNames[extraContainersCount] = dummyContainerName
		extraAnnotations := make(map[string]string)

		for i := 0; i < extraContainersCount; i++ {
			containersNames[i] = extraContainerName(i)
			for k, v := range annotationsFactory(containersNames[i]) {
				extraAnnotations[k] = v
			}
		}

		testNameSuffix := ""
		if extraContainersCount > 0 {
			testNameSuffix = fmt.Sprintf(" and %d extra containers", extraContainersCount)
		}

		for annotationLevel, annotationPrefix := range map[string]string{
			"pod-level":       "pod",
			"container-level": dummyContainerName + ".container",
		} {
			nameKey := annotationPrefix + ".alpha.windows.kubernetes.io/gmsa-credential-spec-name"
			contentsKey := annotationPrefix + ".alpha.windows.kubernetes.io/gmsa-credential-spec"

			for testName, testFunc := range tests {
				shuffle(containersNames)

				pod := buildPod(extraAnnotations, dummyServiceAccoutName, containersNames...)

				t.Run(fmt.Sprintf("%s - with %s annotations%s", testName, annotationLevel, testNameSuffix), func(t *testing.T) {
					testFunc(t, pod, nameKey, contentsKey)
				})
			}
		}
	}
}

func extraContainerName(i int) string {
	return fmt.Sprintf("extra-container-%d", i)
}

func assertPodAdmissionErrorContains(t *testing.T, err *podAdmissionError, pod *corev1.Pod, httpCode int, msgFormat string, msgArgs ...interface{}) bool {
	if !assert.NotNil(t, err) {
		return false
	}

	result := assert.Equal(t, pod, err.pod)
	result = assert.Equal(t, httpCode, err.code) && result
	return assert.Contains(t, err.Error(), fmt.Sprintf(msgFormat, msgArgs...)) && result
}

func shuffle(a []string) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(a) - 1; i > 0; i-- {
		j := r.Int() % (i + 1)
		tmp := a[j]
		a[j] = a[i]
		a[i] = tmp
	}
}
