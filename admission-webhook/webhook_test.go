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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

			assertPodAdmissionErrorContains(t, err, pod, http.StatusNotFound, "%s", dummyError.Error())
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
			webhook := newWebhookWithOptions(nil, WithRandomHostname(false))
			pod := buildPod(dummyServiceAccoutName, winOptionsFactory(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory()})

			response, err := webhook.mutateCreateRequest(context.Background(), pod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)

			assert.Nil(t, response.Patch)

		})
	}

	for testCaseName, winOptionsFactory := range map[string]func() *corev1.WindowsSecurityContextOptions{
		"with random hostname env set and empty GMSA settings, it passes and does nothing": func() *corev1.WindowsSecurityContextOptions {
			return &corev1.WindowsSecurityContextOptions{}
		},
		"with random hostname env set and no GMSA settings, it passes and does nothing": func() *corev1.WindowsSecurityContextOptions {
			return nil
		},
	} {
		t.Run(testCaseName, func(t *testing.T) {
			webhook := newWebhookWithOptions(nil, WithRandomHostname(true))
			pod := buildPod(dummyServiceAccoutName, winOptionsFactory(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory()})

			response, err := webhook.mutateCreateRequest(context.Background(), pod)
			assert.Nil(t, err)

			require.NotNil(t, response)
			assert.True(t, response.Allowed)

			assert.Nil(t, response.Patch)

		})
	}

	testCaseName, winOptionsFactory1 := "with random hostname env set and dummy GMSA settings, it passes and set random hostname", func() *corev1.WindowsSecurityContextOptions {
		dummyCredSpecNameVar := dummyCredSpecName
		dummyCredSpecContentsVar := dummyCredSpecContents
		return &corev1.WindowsSecurityContextOptions{GMSACredentialSpecName: &dummyCredSpecNameVar, GMSACredentialSpec: &dummyCredSpecContentsVar}
	}
	t.Run(testCaseName, func(t *testing.T) {
		webhook := newWebhookWithOptions(nil, WithRandomHostname(true))
		pod := buildPod(dummyServiceAccoutName, winOptionsFactory1(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory1()})

		response, err := webhook.mutateCreateRequest(context.Background(), pod)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)

		var patches []map[string]string
		// one more because we're adding the hostname
		if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, 1, len(patches)) {
			foundHostname := false
			for _, patch := range patches {
				if value, hasValue := patch["value"]; assert.True(t, hasValue) {
					if patch["path"] == "/spec/hostname" {
						foundHostname = true
						assert.Equal(t, "add", patch["op"])
						assert.Equal(t, 15, len(value))
					}
				}
			}
			assert.True(t, foundHostname)
		}
	})

	testCaseName, winOptionsFactory1 = "with random hostname env set and dummy GMSA settings and hostname set in spec, it passes and do nothing", func() *corev1.WindowsSecurityContextOptions {
		dummyCredSpecNameVar := dummyCredSpecName
		dummyCredSpecContentsVar := dummyCredSpecContents
		return &corev1.WindowsSecurityContextOptions{GMSACredentialSpecName: &dummyCredSpecNameVar, GMSACredentialSpec: &dummyCredSpecContentsVar}
	}
	t.Run(testCaseName, func(t *testing.T) {
		webhook := newWebhookWithOptions(nil, WithRandomHostname(true))
		dummyPodNameVar := dummyPodName
		pod := buildPodWithHostName(dummyServiceAccoutName, &dummyPodNameVar, winOptionsFactory1(), map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: winOptionsFactory1()})

		response, err := webhook.mutateCreateRequest(context.Background(), pod)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)

		assert.Nil(t, response.Patch)
	})

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
		"with random hostname env and a GMSA cred spec name, it passes and inlines the cred-spec's contents and generate random hostname": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, resourceName string) {
			webhook := newWebhookWithOptions(kubeClientFactory(), WithRandomHostname(true))

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

				switch kind {
				case containerKind:
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
				case initContainerKind:
					containerIndex := -1
					for i, container := range pod.Spec.InitContainers {
						if container.Name == name {
							containerIndex = i
							break
						}
					}
					if containerIndex == -1 {
						t.Fatalf("Did not find any init container named %q", name)
					}

					partialPath = fmt.Sprintf("/initContainers/%d", containerIndex)
				case ephemeralContainerKind:
					containerIndex := -1
					for i, container := range pod.Spec.EphemeralContainers {
						if container.Name == name {
							containerIndex = i
							break
						}
					}
					if containerIndex == -1 {
						t.Fatalf("Did not find any ephemeral container named %q", name)
					}

					partialPath = fmt.Sprintf("/ephemeralContainers/%d", containerIndex)
				}

				return fmt.Sprintf("/spec%s/securityContext/windowsOptions/gmsaCredentialSpec", partialPath)
			}

			// maps the contents to the expected patch for that container
			expectedPatches := make(map[string]map[string]string)
			for i := 0; i < numExtraRegularContainers(pod, resourceKind); i++ {
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
			// numExtraRegularContainers(pod, resourceKind)+2 because the resource under test also gets a
			// patch, and we're adding the hostname
			if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, numExtraRegularContainers(pod, resourceKind)+2, len(patches)) {
				foundHostname := false
				for _, patch := range patches {
					if value, hasValue := patch["value"]; assert.True(t, hasValue) {
						if patch["path"] == "/spec/hostname" {
							foundHostname = true
							assert.Equal(t, "add", patch["op"])
							assert.Equal(t, 15, len(value))
						} else if expectedPatch, present := expectedPatches[value]; assert.True(t, present, "value %s not found in expected patches", value) {
							assert.Equal(t, expectedPatch, patch)
						}
					}
				}
				assert.True(t, foundHostname)
			}
		},

		// random hostname env not set in the following cases, and validated no hostname is set (implicitly)
		"it the cred spec's contents are already set, along with its name, it passes and doesn't overwrite the provided contents": func(t *testing.T, pod *corev1.Pod, optionsSelector winOptionsSelector, resourceKind gmsaResourceKind, _ string) {
			webhook := newWebhook(kubeClientFactory())

			setWindowsOptions(optionsSelector(pod), dummyCredSpecName, `{"pre-set GMSA": "cred contents"}`)

			response, err := webhook.mutateCreateRequest(context.Background(), pod)
			assert.Nil(t, err)

			// all the patches we receive should be for the extra containers
			expectedPatchesLen := numExtraRegularContainers(pod, resourceKind)

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

			assertPodAdmissionErrorContains(t, err, pod, http.StatusNotFound, "%s", dummyError.Error())
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

// allAsNewEphemeralContainers treats every container in `containers` as newly appended, at its own
// index - a convenience for tests exercising `validateEphemeralContainersUpdateRequest` /
// `mutateEphemeralContainersUpdateRequest` directly on a full ephemeral containers list.
func allAsNewEphemeralContainers(containers []corev1.EphemeralContainer) []newEphemeralContainer {
	result := make([]newEphemeralContainer, len(containers))
	for i, container := range containers {
		result[i] = newEphemeralContainer{container: container, index: i}
	}
	return result
}

// TestValidateEphemeralContainersUpdateRequest checks that `validateEphemeralContainersUpdateRequest`
// only inspects the pod's ephemeral containers (as opposed to `validateCreateRequest`, which looks at
// the whole pod), since that's the only thing that can change on an `ephemeralcontainers` subresource
// update request.
func TestValidateEphemeralContainersUpdateRequest(t *testing.T) {
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

	t.Run("it ignores other resources' GMSA settings, only validating ephemeral containers", func(t *testing.T) {
		webhook := newWebhook(kubeClientFactory())

		// this regular container's cred spec name doesn't match its contents - were it inspected,
		// validation would fail, but it must be ignored by this function.
		mismatchedOptions := buildWindowsOptions(dummyCredSpecName, "mismatched-cred-spec-contents")
		matchingOptions := buildWindowsOptions(dummyCredSpecName, dummyCredSpecContents)

		pod := buildPodWithEphemeralContainers(
			dummyServiceAccoutName,
			nil,
			mismatchedOptions,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: mismatchedOptions},
			nil,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: matchingOptions},
		)

		response, err := webhook.validateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(pod.Spec.EphemeralContainers), dummyNamespace)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
	})

	t.Run("if a newly added ephemeral container's cred spec contents don't match, it fails", func(t *testing.T) {
		webhook := newWebhook(kubeClientFactory())

		ephemeralOptions := buildWindowsOptions(dummyCredSpecName, "not-the-right-contents")

		pod := buildPodWithEphemeralContainers(
			dummyServiceAccoutName, nil, nil, nil, nil,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: ephemeralOptions},
		)

		response, err := webhook.validateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(pod.Spec.EphemeralContainers), dummyNamespace)
		assert.Nil(t, response)

		assertPodAdmissionErrorContains(t, err, pod, http.StatusUnprocessableEntity,
			"the GMSA cred spec contents for %s %q does not match the contents of GMSA resource %q",
			ephemeralContainerKind, dummyContainerName, dummyCredSpecName)
	})

	t.Run("if the service account is not authorized to use a newly added ephemeral container's cred-spec, it fails", func(t *testing.T) {
		dummyReason := "dummy reason"

		client := kubeClientFactory()
		client.isAuthorizedToUseCredSpecFunc = func(ctx context.Context, serviceAccountName, namespace, credSpecName string) (authorized bool, reason string) {
			return false, dummyReason
		}

		webhook := newWebhook(client)

		ephemeralOptions := buildWindowsOptions(dummyCredSpecName, dummyCredSpecContents)
		pod := buildPodWithEphemeralContainers(
			dummyServiceAccoutName, nil, nil, nil, nil,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: ephemeralOptions},
		)

		response, err := webhook.validateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(pod.Spec.EphemeralContainers), dummyNamespace)
		assert.Nil(t, response)

		assertPodAdmissionErrorContains(t, err, pod, http.StatusForbidden,
			"service account %q is not authorized to `use` GMSA cred spec %q, reason: %q",
			dummyServiceAccoutName, dummyCredSpecName, dummyReason)
	})

	t.Run("with a newly added ephemeral container that doesn't set a GMSA name, it passes without checking authorization or cred spec contents", func(t *testing.T) {
		client := kubeClientFactory()
		client.isAuthorizedToUseCredSpecFunc = func(ctx context.Context, serviceAccountName, namespace, credSpecName string) (authorized bool, reason string) {
			t.Fatal("isAuthorizedToUseCredSpec should not be called for a container with no GMSA name set")
			return false, ""
		}
		client.retrieveCredSpecContentsFunc = func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
			t.Fatal("retrieveCredSpecContents should not be called for a container with no GMSA name set")
			return "", 0, nil
		}

		webhook := newWebhook(client)

		runAsUserName := "some-user"
		ephemeralOptions := &corev1.WindowsSecurityContextOptions{RunAsUserName: &runAsUserName}
		pod := buildPodWithEphemeralContainers(
			dummyServiceAccoutName, nil, nil, nil, nil,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: ephemeralOptions},
		)

		response, err := webhook.validateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(pod.Spec.EphemeralContainers), dummyNamespace)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
	})
}

// TestMutateEphemeralContainersUpdateRequest checks that `mutateEphemeralContainersUpdateRequest` only
// patches the pod's ephemeral containers, and leaves everything else - including the hostname, which
// only makes sense to set at pod creation time - untouched.
func TestMutateEphemeralContainersUpdateRequest(t *testing.T) {
	kubeClientFactory := func() *dummyKubeClient {
		return &dummyKubeClient{
			retrieveCredSpecContentsFunc: func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
				return dummyCredSpecContents, http.StatusOK, nil
			},
		}
	}

	t.Run("it only patches new ephemeral containers, ignoring other resources and the hostname", func(t *testing.T) {
		webhook := newWebhookWithOptions(kubeClientFactory(), WithRandomHostname(true))

		// this regular container would need a patch too if it were inspected by this function.
		regularOptions := buildWindowsOptions(dummyCredSpecName, "")
		ephemeralOptions := buildWindowsOptions(dummyCredSpecName, "")

		pod := buildPodWithEphemeralContainers(
			dummyServiceAccoutName,
			nil,
			nil,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: regularOptions},
			nil,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: ephemeralOptions},
		)

		response, err := webhook.mutateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(pod.Spec.EphemeralContainers))
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)

		var patches []map[string]string
		if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, 1, len(patches)) {
			assert.Contains(t, patches[0]["path"], "/spec/ephemeralContainers/")
			assert.Equal(t, dummyCredSpecContents, patches[0]["value"])
		}
	})

	t.Run("with no ephemeral containers carrying GMSA settings, it passes and does nothing", func(t *testing.T) {
		webhook := newWebhookWithOptions(kubeClientFactory(), WithRandomHostname(true))

		pod := buildPodWithEphemeralContainers(dummyServiceAccoutName, nil, nil, nil, nil, nil)

		response, err := webhook.mutateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(pod.Spec.EphemeralContainers))
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
		assert.Nil(t, response.Patch)
	})

	t.Run("with a new ephemeral container whose windows options don't set a GMSA name, it does not patch it", func(t *testing.T) {
		webhook := newWebhookWithOptions(kubeClientFactory(), WithRandomHostname(true))

		runAsUserName := "some-user"
		ephemeralOptions := &corev1.WindowsSecurityContextOptions{RunAsUserName: &runAsUserName}

		pod := buildPodWithEphemeralContainers(
			dummyServiceAccoutName, nil, nil, nil, nil,
			map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: ephemeralOptions},
		)

		response, err := webhook.mutateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(pod.Spec.EphemeralContainers))
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
		assert.Nil(t, response.Patch)
	})

	t.Run("with several newly appended ephemeral containers, it only patches the one with a GMSA name, at its own index", func(t *testing.T) {
		webhook := newWebhookWithOptions(kubeClientFactory(), WithRandomHostname(true))

		runAsUserName := "some-user"
		newContainers := []corev1.EphemeralContainer{
			{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name:            "no-gmsa-container-1",
					SecurityContext: &corev1.SecurityContext{WindowsOptions: &corev1.WindowsSecurityContextOptions{RunAsUserName: &runAsUserName}},
				},
			},
			{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name:            "gmsa-container",
					SecurityContext: &corev1.SecurityContext{WindowsOptions: buildWindowsOptions(dummyCredSpecName, "")},
				},
			},
			{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name: "no-gmsa-container-2",
					// no security context at all
				},
			},
		}

		pod := buildPod(dummyServiceAccoutName, nil, nil)
		pod.Spec.EphemeralContainers = newContainers

		response, err := webhook.mutateEphemeralContainersUpdateRequest(context.Background(), pod, allAsNewEphemeralContainers(newContainers))
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)

		var patches []map[string]string
		if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, 1, len(patches)) {
			assert.Equal(t, "/spec/ephemeralContainers/1/securityContext/windowsOptions/gmsaCredentialSpec", patches[0]["path"])
			assert.Equal(t, dummyCredSpecContents, patches[0]["value"])
		}
	})
}

// TestNewlyAppendedEphemeralContainers checks that `newlyAppendedEphemeralContainers` correctly
// identifies the containers newly appended by a `pods/ephemeralcontainers` update request, pairing
// each with its actual index in the new list, and rejects requests where an old container is
// missing from the new list (having been removed or modified).
func TestNewlyAppendedEphemeralContainers(t *testing.T) {
	existingContainer := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "existing-container"},
	}
	newContainer := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "new-container"},
	}

	buildPodWithContainers := func(containers ...corev1.EphemeralContainer) *corev1.Pod {
		pod := buildPod(dummyServiceAccoutName, nil, nil)
		pod.Spec.EphemeralContainers = containers
		return pod
	}

	t.Run("with no new containers, it returns an empty slice and no error", func(t *testing.T) {
		oldPod := buildPodWithContainers(existingContainer)
		pod := buildPodWithContainers(existingContainer)

		newContainers, err := newlyAppendedEphemeralContainers(pod, oldPod)
		assert.Nil(t, err)
		assert.Empty(t, newContainers)
	})

	t.Run("with a newly appended container, it returns just that container, at its own index", func(t *testing.T) {
		oldPod := buildPodWithContainers(existingContainer)
		pod := buildPodWithContainers(existingContainer, newContainer)

		newContainers, err := newlyAppendedEphemeralContainers(pod, oldPod)
		assert.Nil(t, err)
		assert.Equal(t, []newEphemeralContainer{{container: newContainer, index: 1}}, newContainers)
	})

	t.Run("if the new list is shorter than the old one, it fails", func(t *testing.T) {
		oldPod := buildPodWithContainers(existingContainer, newContainer)
		pod := buildPodWithContainers(existingContainer)

		newContainers, err := newlyAppendedEphemeralContainers(pod, oldPod)
		assert.Nil(t, newContainers)
		assertPodAdmissionErrorContains(t, err, pod, http.StatusBadRequest,
			"ephemeral containers can only be appended to a pod, existing ones cannot be modified or removed")
	})

	t.Run("if an existing container was modified, it fails", func(t *testing.T) {
		oldPod := buildPodWithContainers(existingContainer)
		modifiedContainer := existingContainer
		modifiedContainer.Image = "some-other-image"
		pod := buildPodWithContainers(modifiedContainer, newContainer)

		newContainers, err := newlyAppendedEphemeralContainers(pod, oldPod)
		assert.Nil(t, newContainers)
		assertPodAdmissionErrorContains(t, err, pod, http.StatusBadRequest,
			"ephemeral containers can only be appended to a pod, existing ones cannot be modified or removed")
	})

	t.Run("if an existing container is removed and a new one appended in the same request, it fails", func(t *testing.T) {
		anotherExistingContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "another-existing-container"},
		}

		// oldPod has two existing containers; the request removes anotherExistingContainer and
		// appends newContainer, keeping the same total count - this must still be rejected, since
		// anotherExistingContainer is no longer present anywhere in the new list.
		oldPod := buildPodWithContainers(existingContainer, anotherExistingContainer)
		pod := buildPodWithContainers(existingContainer, newContainer)

		newContainers, err := newlyAppendedEphemeralContainers(pod, oldPod)
		assert.Nil(t, newContainers)
		assertPodAdmissionErrorContains(t, err, pod, http.StatusBadRequest,
			"ephemeral containers can only be appended to a pod, existing ones cannot be modified or removed")
	})

	t.Run("if an existing container is removed and two new ones are appended, growing the list, it still fails", func(t *testing.T) {
		anotherExistingContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "another-existing-container"},
		}
		anotherNewContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "another-new-container"},
		}

		// the new list is longer than the old one, so a cheap length check alone wouldn't catch
		// this - it must be caught by the missing-old-container check instead.
		oldPod := buildPodWithContainers(existingContainer, anotherExistingContainer)
		pod := buildPodWithContainers(existingContainer, newContainer, anotherNewContainer)

		newContainers, err := newlyAppendedEphemeralContainers(pod, oldPod)
		assert.Nil(t, newContainers)
		assertPodAdmissionErrorContains(t, err, pod, http.StatusBadRequest,
			"ephemeral containers can only be appended to a pod, existing ones cannot be modified or removed")
	})

	t.Run("if an existing container is modified but an unmodified duplicate of it also appears in the new list, it still fails", func(t *testing.T) {
		modifiedContainer := existingContainer
		modifiedContainer.Image = "some-other-image"

		// an attacker could try to disguise a modification as an append by pairing a modified
		// copy of existingContainer with a byte-for-byte duplicate elsewhere in the new list:
		// content-only matching would let the duplicate "absorb" the old container, leaving the
		// modified copy to be waved through as newly appended. Matching by name must catch this.
		oldPod := buildPodWithContainers(existingContainer)
		pod := buildPodWithContainers(modifiedContainer, existingContainer)

		newContainers, err := newlyAppendedEphemeralContainers(pod, oldPod)
		assert.Nil(t, newContainers)
		assertPodAdmissionErrorContains(t, err, pod, http.StatusBadRequest,
			"ephemeral containers can only be appended to a pod, existing ones cannot be modified or removed")
	})
}

// TestValidateOrMutateEphemeralContainersSubresourceRequest is an AdmissionRequest-level test (as
// opposed to the more unit-level TestValidateEphemeralContainersUpdateRequest and
// TestMutateEphemeralContainersUpdateRequest above) covering a pod with one already-admitted
// ephemeral container and one newly appended one, to make sure `validateOrMutate` only inspects the
// newly appended container: the already-admitted one's GMSA settings must not be re-validated (its
// backing GMSA resource may have changed since it was admitted), and any patch generated for the new
// container must target the right index in `.Spec.EphemeralContainers`.
func TestValidateOrMutateEphemeralContainersSubresourceRequest(t *testing.T) {
	const (
		existingContainerName   = "existing-container"
		newContainerName        = "new-container"
		currentCredSpecContents = "current-cred-spec-contents"
		staleCredSpecContents   = "stale-cred-spec-contents"
	)

	// the existing container was admitted with `staleCredSpecContents`, which no longer matches
	// what the GMSA resource currently holds (`currentCredSpecContents`) - simulating drift since
	// admission. The newly appended container was already mutated with the current contents.
	oldPod := buildPod(dummyServiceAccoutName, nil, nil)
	oldPod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
		{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            existingContainerName,
				SecurityContext: &corev1.SecurityContext{WindowsOptions: buildWindowsOptions(dummyCredSpecName, staleCredSpecContents)},
			},
		},
	}

	buildRequest := func(t *testing.T, newContainer corev1.EphemeralContainer) (*admissionV1.AdmissionRequest, *corev1.Pod) {
		pod := oldPod.DeepCopy()
		pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, newContainer)

		oldPodRaw, err := json.Marshal(oldPod)
		require.NoError(t, err)
		podRaw, err := json.Marshal(pod)
		require.NoError(t, err)

		request := &admissionV1.AdmissionRequest{
			Kind:        metav1.GroupVersionKind{Kind: "Pod"},
			Namespace:   dummyNamespace,
			Operation:   admissionV1.Update,
			SubResource: "ephemeralcontainers",
			Object:      runtime.RawExtension{Raw: podRaw},
			OldObject:   runtime.RawExtension{Raw: oldPodRaw},
		}

		// validateOrMutate re-unmarshals `request.Object` into its own `*corev1.Pod`, so callers
		// checking `podAdmissionError.pod` need this equivalent (but distinct) copy to compare against.
		expectedPod := &corev1.Pod{}
		require.NoError(t, json.Unmarshal(podRaw, expectedPod))

		return request, expectedPod
	}

	kubeClientFactory := func() *dummyKubeClient {
		return &dummyKubeClient{
			retrieveCredSpecContentsFunc: func(ctx context.Context, credSpecName string) (contents string, httpCode int, err error) {
				return currentCredSpecContents, http.StatusOK, nil
			},
		}
	}

	t.Run("validate ignores the already-admitted container's now-stale GMSA settings", func(t *testing.T) {
		webhook := newWebhook(kubeClientFactory())

		newContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            newContainerName,
				SecurityContext: &corev1.SecurityContext{WindowsOptions: buildWindowsOptions(dummyCredSpecName, currentCredSpecContents)},
			},
		}

		request, _ := buildRequest(t, newContainer)
		response, err := webhook.validateOrMutate(context.Background(), request, validate)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)
	})

	t.Run("mutate only patches the newly appended container, at the correct index", func(t *testing.T) {
		webhook := newWebhook(kubeClientFactory())

		newContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            newContainerName,
				SecurityContext: &corev1.SecurityContext{WindowsOptions: buildWindowsOptions(dummyCredSpecName, "")},
			},
		}

		request, _ := buildRequest(t, newContainer)
		response, err := webhook.validateOrMutate(context.Background(), request, mutate)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)

		var patches []map[string]string
		if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, 1, len(patches)) {
			assert.Equal(t, "/spec/ephemeralContainers/1/securityContext/windowsOptions/gmsaCredentialSpec", patches[0]["path"])
			assert.Equal(t, currentCredSpecContents, patches[0]["value"])
		}
	})

	t.Run("validate still rejects a newly appended container with mismatching GMSA settings", func(t *testing.T) {
		webhook := newWebhook(kubeClientFactory())

		newContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            newContainerName,
				SecurityContext: &corev1.SecurityContext{WindowsOptions: buildWindowsOptions(dummyCredSpecName, staleCredSpecContents)},
			},
		}

		request, expectedPod := buildRequest(t, newContainer)
		response, err := webhook.validateOrMutate(context.Background(), request, validate)
		assert.Nil(t, response)

		assertPodAdmissionErrorContains(t, err, expectedPod, http.StatusUnprocessableEntity,
			"the GMSA cred spec contents for %s %q does not match the contents of GMSA resource %q",
			ephemeralContainerKind, newContainerName, dummyCredSpecName)
	})

	t.Run("both validate and mutate reject a request that removes the existing container while appending a new one", func(t *testing.T) {
		newContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            newContainerName,
				SecurityContext: &corev1.SecurityContext{WindowsOptions: buildWindowsOptions(dummyCredSpecName, currentCredSpecContents)},
			},
		}

		// unlike buildRequest, this pod does not retain the existing container - it replaces it
		// with the new one instead, keeping the same total count.
		pod := buildPod(dummyServiceAccoutName, nil, nil)
		pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{newContainer}

		oldPodRaw, err := json.Marshal(oldPod)
		require.NoError(t, err)
		podRaw, err := json.Marshal(pod)
		require.NoError(t, err)

		request := &admissionV1.AdmissionRequest{
			Kind:        metav1.GroupVersionKind{Kind: "Pod"},
			Namespace:   dummyNamespace,
			Operation:   admissionV1.Update,
			SubResource: "ephemeralcontainers",
			Object:      runtime.RawExtension{Raw: podRaw},
			OldObject:   runtime.RawExtension{Raw: oldPodRaw},
		}

		expectedPod := &corev1.Pod{}
		require.NoError(t, json.Unmarshal(podRaw, expectedPod))

		for _, operation := range []webhookOperation{validate, mutate} {
			webhook := newWebhook(kubeClientFactory())

			response, err := webhook.validateOrMutate(context.Background(), request, operation)
			assert.Nil(t, response)

			assertPodAdmissionErrorContains(t, err, expectedPod, http.StatusBadRequest,
				"ephemeral containers can only be appended to a pod, existing ones cannot be modified or removed")
		}
	})

	t.Run("mutate patches a newly appended container inserted in the middle of the list, at its own real index", func(t *testing.T) {
		// give oldPod a second existing container, so a new container inserted between the two
		// existing ones lands at an index that an offset-based scheme (old-length as offset)
		// would get wrong.
		anotherExistingContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "another-existing-container"},
		}
		oldPodWithTwoExisting := oldPod.DeepCopy()
		oldPodWithTwoExisting.Spec.EphemeralContainers = append(oldPodWithTwoExisting.Spec.EphemeralContainers, anotherExistingContainer)

		newContainer := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            newContainerName,
				SecurityContext: &corev1.SecurityContext{WindowsOptions: buildWindowsOptions(dummyCredSpecName, "")},
			},
		}

		pod := oldPodWithTwoExisting.DeepCopy()
		pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
			oldPodWithTwoExisting.Spec.EphemeralContainers[0],
			newContainer,
			oldPodWithTwoExisting.Spec.EphemeralContainers[1],
		}

		oldPodRaw, err := json.Marshal(oldPodWithTwoExisting)
		require.NoError(t, err)
		podRaw, err := json.Marshal(pod)
		require.NoError(t, err)

		request := &admissionV1.AdmissionRequest{
			Kind:        metav1.GroupVersionKind{Kind: "Pod"},
			Namespace:   dummyNamespace,
			Operation:   admissionV1.Update,
			SubResource: "ephemeralcontainers",
			Object:      runtime.RawExtension{Raw: podRaw},
			OldObject:   runtime.RawExtension{Raw: oldPodRaw},
		}

		webhook := newWebhook(kubeClientFactory())

		response, err := webhook.validateOrMutate(context.Background(), request, mutate)
		assert.Nil(t, err)

		require.NotNil(t, response)
		assert.True(t, response.Allowed)

		var patches []map[string]string
		if err := json.Unmarshal(response.Patch, &patches); assert.Nil(t, err) && assert.Equal(t, 1, len(patches)) {
			assert.Equal(t, "/spec/ephemeralContainers/1/securityContext/windowsOptions/gmsaCredentialSpec", patches[0]["path"])
			assert.Equal(t, currentCredSpecContents, patches[0]["value"])
		}
	})
}

func TestDefaultWebhookConfig(t *testing.T) {
	expectedCertReload := false
	webhook := newWebhookWithOptions(nil, WithCertReload(expectedCertReload))
	assert.Equal(t, expectedCertReload, webhook.config.EnableCertReload)
}

func TestSetWebhookConfig(t *testing.T) {
	expectedCertReload := true
	expectedRandomHostname := true
	randomHostname := true
	webhook := newWebhookWithOptions(nil, WithCertReload(expectedCertReload), WithRandomHostname(randomHostname))
	assert.Equal(t, expectedCertReload, webhook.config.EnableCertReload)
	assert.Equal(t, expectedRandomHostname, webhook.config.EnableRandomHostName)
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

		for _, resourceKind := range []gmsaResourceKind{podKind, containerKind, initContainerKind, ephemeralContainerKind} {
			for testName, testFunc := range tests {
				podWindowsOptions := &corev1.WindowsSecurityContextOptions{}

				var pod *corev1.Pod
				var optionsSelector winOptionsSelector
				var resourceName string
				switch resourceKind {
				case podKind:
					containerNamesAndWindowsOptions[dummyContainerName] = &corev1.WindowsSecurityContextOptions{}
					pod = buildPod(dummyServiceAccoutName, podWindowsOptions, containerNamesAndWindowsOptions)

					optionsSelector = func(pod *corev1.Pod) *corev1.WindowsSecurityContextOptions {
						if pod != nil && pod.Spec.SecurityContext != nil {
							return pod.Spec.SecurityContext.WindowsOptions
						}
						return nil
					}

					resourceName = dummyPodName
				case containerKind:
					containerNamesAndWindowsOptions[dummyContainerName] = &corev1.WindowsSecurityContextOptions{}
					pod = buildPod(dummyServiceAccoutName, podWindowsOptions, containerNamesAndWindowsOptions)

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
				case initContainerKind:
					// the dummy container under test is an init container here, so it must not also
					// be present amongst the (regular) extra containers.
					delete(containerNamesAndWindowsOptions, dummyContainerName)
					initContainerNamesAndWindowsOptions := map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: {}}
					pod = buildPodWithInitContainers(dummyServiceAccoutName, nil, podWindowsOptions, containerNamesAndWindowsOptions, initContainerNamesAndWindowsOptions)

					optionsSelector = func(pod *corev1.Pod) *corev1.WindowsSecurityContextOptions {
						if pod != nil {
							for _, container := range pod.Spec.InitContainers {
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
				case ephemeralContainerKind:
					// the dummy container under test is an ephemeral container here, so it must not
					// also be present amongst the (regular) extra containers.
					delete(containerNamesAndWindowsOptions, dummyContainerName)
					ephemeralContainerNamesAndWindowsOptions := map[string]*corev1.WindowsSecurityContextOptions{dummyContainerName: {}}
					pod = buildPodWithEphemeralContainers(dummyServiceAccoutName, nil, podWindowsOptions, containerNamesAndWindowsOptions, nil, ephemeralContainerNamesAndWindowsOptions)

					optionsSelector = func(pod *corev1.Pod) *corev1.WindowsSecurityContextOptions {
						if pod != nil {
							for _, container := range pod.Spec.EphemeralContainers {
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

// numExtraRegularContainers returns the number of "extra" (i.e. not under test) regular
// containers set on pod.Spec.Containers. runWebhookValidateOrMutateTests always mixes the
// container under test into pod.Spec.Containers for podKind/containerKind, but keeps
// pod.Spec.Containers to only the extra containers for initContainerKind and
// ephemeralContainerKind (the container under test lives in pod.Spec.InitContainers or
// pod.Spec.EphemeralContainers instead).
func numExtraRegularContainers(pod *corev1.Pod, resourceKind gmsaResourceKind) int {
	count := len(pod.Spec.Containers)
	if resourceKind != initContainerKind && resourceKind != ephemeralContainerKind {
		count--
	}
	return count
}

func assertPodAdmissionErrorContains(t *testing.T, err *podAdmissionError, pod *corev1.Pod, httpCode int, msgFormat string, msgArgs ...interface{}) bool {
	if !assert.NotNil(t, err) {
		return false
	}

	result := assert.Equal(t, pod, err.pod)
	result = assert.Equal(t, httpCode, err.code) && result
	return assert.Contains(t, err.Error(), fmt.Sprintf(msgFormat, msgArgs...)) && result
}
