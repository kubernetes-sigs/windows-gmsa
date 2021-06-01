package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionV1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestValidateCreateRequest(t *testing.T) {
	for testCaseName, winOptionsFactory := range map[string]func() *corev1.WindowsSecurityContextOptions{
		"with empty GMSA settings": func() *corev1.WindowsSecurityContextOptions {
			return &corev1.WindowsSecurityContextOptions{}
		},
		"with no GMSA settings": func() *corev1.WindowsSecurityContextOptions {
			return nil
		},
	} {
		t.Run(testCaseName, func(t *testing.T) {
			webhook := newWebhook(nil)
			pod := buildPod(dummyServiceAccoutName, winOptionsFactory(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory()})

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		})
	}

	kubeClientFactory := func() *dummyKubeClient {
		return &dummyKubeClient{
			retrieveCredSpecContentsFunc: func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					contents = dummyCredSpecContents
				} else {
					contents = credSpecName + "-contents"
				}
				return
			},
		}
	}

	winOptionsFactory := func(containerName string) *corev1.WindowsSecurityContextOptions {
		return buildWindowsOptions(containerName+"-cred-spec", containerName+"-cred-spec-contents")
	}

	runWebhookValidateOrMutateTests(t, winOptionsFactory, map[string]webhookValidateOrMutateTest{
		"with matching name & content, it passes": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, _ gmsaResourceKind, _ string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, dummyCredSpecContents)

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		},

		"if the cred spec contents are not byte-to-byte equal to that of the one named, but still represent equivalent JSONs, it passes": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, _ gmsaResourceKind, _ string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(
				optionsSelector(pod),
				dummyCredSpecName,
				`{"All in all you're just another":      {"the":"wall","brick":   "in"},"We don't need no":["education", "thought control","dark sarcasm in the classroom"]}`,
			)

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		},

		"if the cred spec contents are not that of the one named, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(
				optionsSelector(pod),
				dummyCredSpecName,
				`{"We don't need no": ["money"], "All in all you're just another": {"brick": "in", "the": "wall"}}`,
			)

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusUnprocessableEntity,
				"the GMSA cred spec contents for %s %q does not match the contents of GMSA resource %q",
				resourceKind, resourceName, dummyCredSpecName)
		},

		"if the cred spec contents are not byte-to-byte equal to that of the one named, and are not even a valid JSON object, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, "i ain't no JSON object")

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusUnprocessableEntity,
				"the GMSA cred spec contents for %s %q does not match the contents of GMSA resource %q",
				resourceKind, resourceName, dummyCredSpecName)
		},

		"if the contents are set, but the name one isn't provided, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(optionsSelector(pod), "", dummyCredSpecContents)

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)

			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusUnprocessableEntity,
				"%s %q has a GMSA cred spec set, but does not define the name of the corresponding resource",
				resourceKind, resourceName)
		},

		"if the service account is not authorized to use the cred-spec, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, _ gmsaResourceKind, _ string) {
			dummyReason := "dummy reason"

			client := kubeClientFactory()
			client.isAuthorizedToUseCredSpecFunc = func(ctx context.Context, serviceAccountName, namespace, credSpecName string) (authorized bool, reason string) {
				if credSpecName == dummyCredSpecName {
					assert.Equal(t, dummyServiceAccoutName, serviceAccountName)
					assert.Equal(t, dummyNamespace, namespace)

					return false, dummyReason
				}

				return true, ""
			}

			webhook := newWebhook(client)

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, dummyCredSpecContents)

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"service account %q is not authorized to `use` GMSA cred spec %q, reason: %q",
				dummyServiceAccoutName, dummyCredSpecName, dummyReason)
		},

		"if there is an error when retrieving the cred-spec's contents, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, _ gmsaResourceKind, _ string) {
			dummyError := fmt.Errorf("dummy error")

			client := kubeClientFactory()
			previousRetrieveCredSpecContentsFunc := client.retrieveCredSpecContentsFunc
			client.retrieveCredSpecContentsFunc = func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					return "", http.StatusNotFound, dummyError
				}
				return previousRetrieveCredSpecContentsFunc(ctx, credSpecName)
			}

			webhook := newWebhook(client)

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, dummyCredSpecContents)

			response, err := webhook.validateCreateRequest(context.Background(), pod, dummyNamespace)

			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusNotFound, dummyError.Error())
		},
	})
}

func TestMutateCreateRequest(t *testing.T) {
	for testCaseName, winOptionsFactory := range map[string]func() *corev1.WindowsSecurityContextOptions{
		"with empty GMSA settings, it passes and does nothing": func() *corev1.WindowsSecurityContextOptions {
			return &corev1.WindowsSecurityContextOptions{}
		},
		"with no GMSA settings, it passes and does nothing": func() *corev1.WindowsSecurityContextOptions {
			return nil
		},
	} {
		t.Run(testCaseName, func(t *testing.T) {
			webhook := newWebhook(nil)
			pod := buildPod(dummyServiceAccoutName, winOptionsFactory(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory()})

			response, err := webhook.mutateCreateRequest(context.Background(), pod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		})
	}

	kubeClientFactory := func() *dummyKubeClient {
		return &dummyKubeClient{
			retrieveCredSpecContentsFunc: func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					contents = dummyCredSpecContents
				} else {
					contents = credSpecName + "-contents"
				}
				return
			},
		}
	}

	winOptionsFactory := func(containerName string) *corev1.WindowsSecurityContextOptions {
		return buildWindowsOptions(containerName+"-cred-spec", "")
	}

	runWebhookValidateOrMutateTests(t, winOptionsFactory, map[string]webhookValidateOrMutateTest{
		"with a GMSA cred spec name, it passes and inlines the cred-spec's contents": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, "")

			response, err := webhook.mutateCreateRequest(context.Background(), pod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)

			if assert.NotNil(t, response.PatchType) {
				assert.Equal(t, admissionV1.PatchTypeJSONPatch, *response.PatchType)
			}

			patchPath := func(kind gmsaResourceKind, name string) string {
				partialPath := ""

				if kind == containerKind {
					containerIndex := -1
					for i, container := range pod.Spec.Containers {
						if container.Name == name {
							containerIndex = i
							break
						}
					}
					if containerIndex == -1 {
						t.Fatalf("Did not find any container named %q", name)
					}

					partialPath = fmt.Sprintf("/containers/%d", containerIndex)
				}

				return fmt.Sprintf("/spec%s/securityContext/windowsOptions/gmsaCredentialSpec", partialPath)
			}

			// maps the contents to the expected patch for that container
			expectedPatches := make(map[string]map[string]string)
			for i := 0; i < len(pod.Spec.Containers)-1; i++ {
				credSpecContents := extraContainerName(i) + "-cred-spec-contents"
				expectedPatches[credSpecContents] = map[string]string{
					"op":    "add",
					"path":  patchPath(containerKind, extraContainerName(i)),
					"value": credSpecContents,
				}
			}
			// and the patch for this test's specific cred spec
			expectedPatches[dummyCredSpecContents] = map[string]string{
				"op":    "add",
				"path":  patchPath(resourceKind, resourceName),
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

		"it the cred spec's contents are already set, along with its name, it passes and doesn't overwrite the provided contents": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, _ gmsaResourceKind, _ string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, `{"pre-set GMSA": "cred contents"}`)

			response, err := webhook.mutateCreateRequest(context.Background(), pod)
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

		"if there is an error when retrieving the cred-spec's contents, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, _ gmsaResourceKind, _ string) {
			dummyError := fmt.Errorf("dummy error")

			client := kubeClientFactory()
			previousRetrieveCredSpecContentsFunc := client.retrieveCredSpecContentsFunc
			client.retrieveCredSpecContentsFunc = func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
				if credSpecName == dummyCredSpecName {
					return "", http.StatusNotFound, dummyError
				}
				return previousRetrieveCredSpecContentsFunc(ctx, credSpecName)
			}

			webhook := newWebhook(client)

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, "")

			response, err := webhook.mutateCreateRequest(context.Background(), pod)

			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusNotFound, dummyError.Error())
		},
	})
}

func TestValidateUpdateRequest(t *testing.T) {
	for testCaseName, winOptionsFactory := range map[string]func() *corev1.WindowsSecurityContextOptions{
		"with empty GMSA settings, it passes and does nothing": func() *corev1.WindowsSecurityContextOptions {
			return &corev1.WindowsSecurityContextOptions{}
		},
		"with no GMSA settings, it passes and does nothing": func() *corev1.WindowsSecurityContextOptions {
			return nil
		},
	} {
		t.Run(testCaseName, func(t *testing.T) {
			pod := buildPod(dummyServiceAccoutName, winOptionsFactory(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory()})
			oldPod := buildPod(dummyServiceAccoutName, winOptionsFactory(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory()})

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		})
	}

	winOptionsFactory := func(containerName string) *corev1.WindowsSecurityContextOptions {
		return buildWindowsOptions(containerName+"-cred-spec", containerName+"-cred-spec-contents")
	}

	runWebhookValidateOrMutateTests(t, winOptionsFactory, map[string]webhookValidateOrMutateTest{
		"if there was no changes to GMSA settings, it passes": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, _ gmsaResourceKind, _ string) {
			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, dummyCredSpecContents)

			oldPod := pod.DeepCopy()

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)
		},

		"if there was a change to a GMSA name, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			setWindowsOptions(optionsSelector(pod), "new-cred-spec-name", dummyCredSpecContents)

			oldPod := pod.DeepCopy()
			setWindowsOptions(optionsSelector(oldPod), dummyCredSpecName, "")

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cannot update an existing pod's GMSA settings (GMSA name modified on %s %q)",
				resourceKind, resourceName)
		},

		"if there was a change to a GMSA contents, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, "new-cred-spec-contents")

			oldPod := pod.DeepCopy()
			setWindowsOptions(optionsSelector(oldPod), "", dummyCredSpecContents)

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cannot update an existing pod's GMSA settings (GMSA contents modified on %s %q)",
				resourceKind, resourceName)
		},

		"if there were changes to both GMSA name & contents, it fails": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			setWindowsOptions(optionsSelector(pod), "new-cred-spec-name", "new-cred-spec-contents")

			oldPod := pod.DeepCopy()
			setWindowsOptions(optionsSelector(oldPod), dummyCredSpecName, dummyCredSpecContents)

			response, err := validateUpdateRequest(pod, oldPod)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
				"cannot update an existing pod's GMSA settings (GMSA name and contents modified on %s %q)",
				resourceKind, resourceName)
		},
	})
}

func TestEqualStringPointers(t *testing.T) {
	ptrToString := func(s *string) string {
		if s == nil {
			return "nil"
		}
		return " = " + *s
	}

	foo := "foo"
	bar := "bar"

	for _, testCase := range []struct {
		s1             *string
		s2             *string
		expectedResult bool
	}{
		{
			s1:             nil,
			s2:             nil,
			expectedResult: true,
		},
		{
			s1:             &foo,
			s2:             nil,
			expectedResult: false,
		},
		{
			s1:             &foo,
			s2:             &foo,
			expectedResult: true,
		},
		{
			s1:             &foo,
			s2:             &bar,
			expectedResult: false,
		},
	} {
		for _, ptrs := range [][]*string{
			{testCase.s1, testCase.s2},
			{testCase.s2, testCase.s1},
		} {
			s1 := ptrs[0]
			s2 := ptrs[1]

			testName := fmt.Sprintf("with s1 %s and s2 %s, should return %v",
				ptrToString(s1),
				ptrToString(s2),
				testCase.expectedResult)

			t.Run(testName, func(t *testing.T) {
				assert.Equal(t, testCase.expectedResult, equalStringPointers(s1, s2))
			})
		}
	}
}

/* Helpers below */

type containerWindowsOptionsFactory func(containerName string) *corev1.WindowsSecurityContextOptions

type winOptionsSelector func(pod *corev1.Pod) *corev1.WindowsSecurityContextOptions

// a webhookValidateOrMutateTest function should run a test on one of the webhook's validate or mutate
// functions, given a selector to extract the WindowsSecurityOptions struct it can play with from the pod.
// It should assume that the pod it receives has any number of extra containers with correct
// (in the sense of the test) windows security options generated by a relevant containerWindowsOptionsFactory.
type webhookValidateOrMutateTest func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string)

// runWebhookValidateOrMutateTests runs the given tests with 0 to 5 extra containers with correct windows
// security options as generated by winOptionsFactory.
func runWebhookValidateOrMutateTests(t *testing.T, winOptionsFactory containerWindowsOptionsFactory, tests map[string]webhookValidateOrMutateTest) {
	for extraContainersCount := 0; extraContainersCount <= 5; extraContainersCount++ {
		containerNamesAndWindowsOptions := make(map[string]*corev1.WindowsSecurityContextOptions)

		for i := 0; i < extraContainersCount; i++ {
			containerName := extraContainerName(i)
			containerNamesAndWindowsOptions[containerName] = winOptionsFactory(containerName)
		}

		testNameSuffix := ""
		if extraContainersCount > 0 {
			testNameSuffix = fmt.Sprintf(" and %d extra containers", extraContainersCount)
		}

		for _, resourceKind := range []gmsaResourceKind{podKind, containerKind} {
			for testName, testFunc := range tests {
				podWindowsOptions := &corev1.WindowsSecurityContextOptions{}
				containerNamesAndWindowsOptions[dummyContainerName] = &corev1.WindowsSecurityContextOptions{}
				pod := buildPod(dummyServiceAccoutName, podWindowsOptions, containerNamesAndWindowsOptions)

				var optionsSelector winOptionsSelector
				var resourceName string
				switch resourceKind {
				case podKind:
					optionsSelector = func(pod *corev1.Pod) *corev1.WindowsSecurityContextOptions {
						if pod != nil && pod.Spec.SecurityContext != nil {
							return pod.Spec.SecurityContext.WindowsOptions
						}
						return nil
					}

					resourceName = dummyPodName
				case containerKind:
					optionsSelector = func(pod *corev1.Pod) *corev1.WindowsSecurityContextOptions {
						if pod != nil {
							for _, container := range pod.Spec.Containers {
								if container.Name == dummyContainerName {
									if container.SecurityContext != nil {
										return container.SecurityContext.WindowsOptions
									}
									return nil
								}
							}
						}
						return nil
					}

					resourceName = dummyContainerName
				default:
					t.Fatalf("Unknown resource kind: %q", resourceKind)
				}

				t.Run(fmt.Sprintf("%s - with %s-level windows options%s", testName, resourceKind, testNameSuffix), func(t *testing.T) {
					testFunc(t, pod, optionsSelector, resourceKind, resourceName)
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
