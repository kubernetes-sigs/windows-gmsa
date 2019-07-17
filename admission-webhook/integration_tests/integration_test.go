package integrationtests

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// this is the JSON representation of the cred spec from templates/credspec-0.yml
	expectedCredSpec0 = `{"ActiveDirectoryConfig":{"GroupManagedServiceAccounts":[{"Name":"WebApplication0","Scope":"CONTOSO"},{"Name":"WebApplication0","Scope":"contoso.com"}]},"CmsPlugins":["ActiveDirectory"],"DomainJoinConfig":{"DnsName":"contoso.com","DnsTreeName":"contoso.com","Guid":"244818ae-87ca-4fcd-92ec-e79e5252348a","MachineAccountName":"WebApplication0","NetBiosName":"CONTOSO","Sid":"S-1-5-21-2126729477-2524075714-3094792973"}}`
	// this is the JSON representation of the cred spec from templates/credspec-1.yml
	expectedCredSpec1 = `{"ActiveDirectoryConfig":{"GroupManagedServiceAccounts":[{"Name":"WebApplication1","Scope":"CONTOSO"},{"Name":"WebApplication1","Scope":"contoso.com"}]},"CmsPlugins":["ActiveDirectory"],"DomainJoinConfig":{"DnsName":"contoso.com","DnsTreeName":"contoso.com","Guid":"244818ae-87ca-4fcd-92ec-e79e5252348a","MachineAccountName":"WebApplication1","NetBiosName":"CONTOSO","Sid":"S-1-5-21-2126729477-2524175714-3194792973"}}`
	// this is the JSON representation of the cred spec from templates/credspec-2.yml
	expectedCredSpec2 = `{"ActiveDirectoryConfig":{"GroupManagedServiceAccounts":[{"Name":"WebApplication2","Scope":"CONTOSO"},{"Name":"WebApplication2","Scope":"contoso.com"}]},"CmsPlugins":["ActiveDirectory"],"DomainJoinConfig":{"DnsName":"contoso.com","DnsTreeName":"contoso.com","Guid":"244818ae-87ca-4fcd-92ec-e79e5252348a","MachineAccountName":"WebApplication2","NetBiosName":"CONTOSO","Sid":"S-1-5-21-2126729477-2524275714-3294792973"}}`

	tmpRoot      = "tmp"
	ymlExtension = ".yml"
)

func TestHappyPathWithPodLevelCredSpec(t *testing.T) {
	testName := "happy-path-with-pod-level-cred-spec"
	credSpecTemplates := []string{"credspec-0"}
	templates := []string{"credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "simple-with-gmsa"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	pod := waitForPodToComeUp(t, testConfig.Namespace, "app="+testName)

	assert.Equal(t, expectedCredSpec0, extractPodCredSpecContents(t, pod))
}

func TestHappyPathWithContainerLevelCredSpec(t *testing.T) {
	testName := "happy-path-with-container-cred-spec"
	credSpecTemplates := []string{"credspec-0"}
	templates := []string{"credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "simple-with-container-level-gmsa"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	pod := waitForPodToComeUp(t, testConfig.Namespace, "app="+testName)

	assert.Equal(t, expectedCredSpec0, extractContainerCredSpecContents(t, pod, "nginx"))
}

func TestHappyPathWithSeveralContainers(t *testing.T) {
	testName := "happy-path-with-several-containers"
	credSpecTemplates := []string{"credspec-0", "credspec-1", "credspec-2"}
	templates := []string{"credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "several-containers-with-gmsa"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	pod := waitForPodToComeUp(t, testConfig.Namespace, "app="+testName)

	assert.Equal(t, expectedCredSpec0, extractContainerCredSpecContents(t, pod, "nginx0"))
	assert.Equal(t, expectedCredSpec1, extractPodCredSpecContents(t, pod))
	assert.Equal(t, expectedCredSpec2, extractContainerCredSpecContents(t, pod, "nginx2"))
}

func TestHappyPathWithPreSetMatchingContents(t *testing.T) {
	testName := "happy-path-with-pre-set-matching-contents"
	credSpecTemplates := []string{"credspec-0"}
	templates := []string{"credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "simple-with-pre-set-matching-contents"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	pod := waitForPodToComeUp(t, testConfig.Namespace, "app="+testName)

	actualCredSpecContents := extractPodCredSpecContents(t, pod)
	assert.NotEqual(t, expectedCredSpec0, actualCredSpecContents)

	var (
		expectedCredSpec map[string]interface{}
		actualCredSpec   map[string]interface{}
	)
	if assert.Nil(t, json.Unmarshal([]byte(expectedCredSpec0), &expectedCredSpec)) &&
		assert.Nil(t, json.Unmarshal([]byte(actualCredSpecContents), &actualCredSpec)) {
		assert.True(t, reflect.DeepEqual(expectedCredSpec, actualCredSpec))
	}
}

func TestServiceAccountDoesNotHavePermissionsToUseCredSpec(t *testing.T) {
	testName := "sa-does-not-have-permissions-to-use-cred-spec"
	credSpecTemplates := []string{"credspec-0"}
	templates := []string{"credspecs-users-rbac-role", "service-account", "simple-with-container-level-gmsa"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	replicaSet := waitForReplicaSetGen1(t, testConfig.Namespace, "app="+testName)
	assert.Equal(t, int32(0), replicaSet.Status.Replicas)
	if assert.Equal(t, 1, len(replicaSet.Status.Conditions)) {
		condition := replicaSet.Status.Conditions[0]

		assert.Equal(t, condition.Reason, "FailedCreate")

		expectedSubstr := fmt.Sprintf("service account %q is not authorized to `use` GMSA cred spec %q", testConfig.ServiceAccountName, testConfig.CredSpecNames[0])
		assert.Contains(t, condition.Message, expectedSubstr)
	}
}

func TestCredSpecDoesNotExist(t *testing.T) {
	testName := "cred-spec-does-not-exist"
	templates := []string{"all-credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "simple-with-unknown-gmsa"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, nil, templates)
	defer tearDownFunc()

	replicaSet := waitForReplicaSetGen1(t, testConfig.Namespace, "app="+testName)
	assert.Equal(t, int32(0), replicaSet.Status.Replicas)
	if assert.Equal(t, 1, len(replicaSet.Status.Conditions)) {
		condition := replicaSet.Status.Conditions[0]

		assert.Equal(t, condition.Reason, "FailedCreate")

		assert.Contains(t, condition.Message, "cred spec i-sure-dont-exist does not exist")
	}
}

func TestCannotPreSetGMSAPodLevelContentsWithoutName(t *testing.T) {
	testName := "cannot-pre-set-gmsa-pod-level-contents-without-name"
	credSpecTemplates := []string{"credspec-0"}
	templates := []string{"all-credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "simple-with-preset-gmsa-pod-level-contents"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	replicaSet := waitForReplicaSetGen1(t, testConfig.Namespace, "app="+testName)
	assert.Equal(t, int32(0), replicaSet.Status.Replicas)
	if assert.Equal(t, 1, len(replicaSet.Status.Conditions)) {
		condition := replicaSet.Status.Conditions[0]

		assert.Equal(t, condition.Reason, "FailedCreate")

		assert.Contains(t, condition.Message, "has a GMSA cred spec set, but does not define the name of the corresponding resource")
	}
}

func TestCannotPreSetGMSAContainerLevelContentsWithoutName(t *testing.T) {
	testName := "cannot-pre-set-gmsa-container-level-contents-without-name"
	credSpecTemplates := []string{"credspec-0"}
	templates := []string{"all-credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "simple-with-preset-gmsa-container-level-contents"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	replicaSet := waitForReplicaSetGen1(t, testConfig.Namespace, "app="+testName)
	assert.Equal(t, int32(0), replicaSet.Status.Replicas)
	if assert.Equal(t, 1, len(replicaSet.Status.Conditions)) {
		condition := replicaSet.Status.Conditions[0]

		assert.Equal(t, condition.Reason, "FailedCreate")

		assert.Contains(t, condition.Message, "has a GMSA cred spec set, but does not define the name of the corresponding resource")
	}
}

func TestCannotPreSetUnmatchingGMSASettings(t *testing.T) {
	testName := "cannot-pre-set-unmatching-gmsa-settings"
	credSpecTemplates := []string{"credspec-0"}
	templates := []string{"all-credspecs-users-rbac-role", "service-account", "sa-rbac-binding", "simple-with-pre-set-unmatching-contents"}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	replicaSet := waitForReplicaSetGen1(t, testConfig.Namespace, "app="+testName)
	assert.Equal(t, int32(0), replicaSet.Status.Replicas)
	if assert.Equal(t, 1, len(replicaSet.Status.Conditions)) {
		condition := replicaSet.Status.Conditions[0]

		assert.Equal(t, condition.Reason, "FailedCreate")

		expectedSubstr := fmt.Sprintf("does not match the contents of GMSA resource %q", testConfig.CredSpecNames[0])
		assert.Contains(t, condition.Message, expectedSubstr)
	}
}

func TestCannotUpdateExistingPodLevelGMSASettings(t *testing.T) {
	testName := "cannot-update-gmsa-pod-level-settings"
	credSpecTemplates := []string{"credspec-0"}
	singlePodTemplate := "single-pod-with-gmsa"
	templates := []string{"credspecs-users-rbac-role", "service-account", "sa-rbac-binding", singlePodTemplate}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	// let's check that the pod has come up correctly, and has the correct GMSA cred inlined
	pod, err := kubeClient(t).CoreV1().Pods(testConfig.Namespace).Get(testName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, expectedCredSpec0, extractPodCredSpecContents(t, pod))

	// now let's try to update the cred spec's content
	testConfig.CredSpecContent = expectedCredSpec1
	defer func() { testConfig.CredSpecContent = "" }()
	renderedTemplate := renderTemplate(t, testConfig, singlePodTemplate)
	success, _, _ := applyManifest(t, renderedTemplate)
	assert.False(t, success)

	// and same for its name
	testConfig.CredSpecNames[0] = "new-credspec"
	renderedTemplate = renderTemplate(t, testConfig, singlePodTemplate)
	success, _, _ = applyManifest(t, renderedTemplate)
	assert.False(t, success)
}

func TestCannotUpdateExistingContainerLevelGMSASettings(t *testing.T) {
	testName := "cannot-update-gmsa-container-level-settings"
	credSpecTemplates := []string{"credspec-0"}
	singlePodTemplate := "single-pod-with-container-level-gmsa"
	templates := []string{"credspecs-users-rbac-role", "service-account", "sa-rbac-binding", singlePodTemplate}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	// let's check that the pod has come up correctly, and has the correct GMSA cred inlined
	pod, err := kubeClient(t).CoreV1().Pods(testConfig.Namespace).Get(testName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, expectedCredSpec0, extractContainerCredSpecContents(t, pod, testName))

	// now let's try to update the cred spec's content
	testConfig.CredSpecContent = expectedCredSpec1
	defer func() { testConfig.CredSpecContent = "" }()
	renderedTemplate := renderTemplate(t, testConfig, singlePodTemplate)
	success, _, _ := applyManifest(t, renderedTemplate)
	assert.False(t, success)

	// and same for its name
	testConfig.CredSpecNames[0] = "new-credspec"
	renderedTemplate = renderTemplate(t, testConfig, singlePodTemplate)
	success, _, _ = applyManifest(t, renderedTemplate)
	assert.False(t, success)
}

func TestPossibleToUpdatePodWithExistingGMSASettings(t *testing.T) {
	testName := "possible-to-update-pod-with-existing-gmsa-settings"
	credSpecTemplates := []string{"credspec-0", "credspec-1"}
	singlePodTemplate := "single-pod-with-gmsa"
	templates := []string{"credspecs-users-rbac-role", "service-account", "sa-rbac-binding", singlePodTemplate}

	testConfig, tearDownFunc := integrationTestSetup(t, testName, credSpecTemplates, templates)
	defer tearDownFunc()

	// let's check that the pod has come up correctly, and has the correct GMSA cred inlined
	pod, err := kubeClient(t).CoreV1().Pods(testConfig.Namespace).Get(testName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, expectedCredSpec0, extractPodCredSpecContents(t, pod))

	// now let's update this pod
	testConfig.Image = "nginx"
	defer func() { testConfig.Image = "" }()
	renderedTemplate := renderTemplate(t, testConfig, singlePodTemplate)
	success, _, _ := applyManifest(t, renderedTemplate)
	assert.True(t, success)
}

/* Helpers */

type testConfig struct {
	TestName           string
	Namespace          string
	TmpDir             string
	CredSpecNames      []string
	CredSpecContent    string
	ClusterRoleName    string
	ServiceAccountName string
	RoleBindingName    string
	Image              string
}

// integrationTestSetup creates a new namespace to play in, and returns a function to
// tear it down afterwards.
// It also applies the given templates
func integrationTestSetup(t *testing.T, name string, credSpecTemplates, templates []string) (*testConfig, func()) {
	if _, err := os.Stat(tmpRoot); os.IsNotExist(err) {
		if err = os.Mkdir(tmpRoot, os.ModePerm); err != nil {
			t.Fatal(err)
		}
	}
	tmpDir, err := ioutil.TempDir(tmpRoot, name+"-")
	if err != nil {
		t.Fatal(err)
	}

	namespace := createNamespace(t, "")

	credSpecNames := make([]string, len(credSpecTemplates))
	for i := range credSpecTemplates {
		credSpecNames[i] = name + "-cred-spec-" + strconv.Itoa(i)
	}

	testConfig := &testConfig{
		TestName:  name,
		Namespace: namespace,
		TmpDir:    tmpDir,

		CredSpecNames:      credSpecNames,
		ClusterRoleName:    name + "-credspecs-users",
		ServiceAccountName: name + "-service-account",
		RoleBindingName:    name + "-use-credspecs",
	}

	templatePaths := make([]string, len(credSpecTemplates)+len(templates))
	for i, template := range append(credSpecTemplates, templates...) {
		templatePaths[i] = renderTemplate(t, testConfig, template)
		applyManifestOrFail(t, templatePaths[i])
	}

	tearDownFunc := func() {
		// helps speed us test when working locally against a throw-away cluster
		// deleting namespaces seems to be a rather heavy operation
		if _, present := os.LookupEnv("K8S_GMSA_ADMISSION_WEBHOOK_INTEGRATION_TEST_SKIP_CLEANUP"); present {
			return
		}

		for _, templatePath := range templatePaths {
			deleteManifest(t, templatePath)
		}

		deleteNamespace(t, namespace)
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Fatal(err)
		}
	}

	return testConfig, tearDownFunc
}

// renderTemplate renders a template, and returns its path.
func renderTemplate(t *testing.T, testConfig *testConfig, name string) string {
	if name[len(name)-len(ymlExtension):] != ymlExtension {
		name += ymlExtension
	}

	contents, err := ioutil.ReadFile(path.Join("templates", name))
	if err != nil {
		t.Fatal(err)
	}

	tplName := fmt.Sprintf("%s-%s", testConfig.Namespace, name)
	tpl, err := template.New(tplName).Parse(string(contents))
	if err != nil {
		t.Fatal(err)
	}

	renderedTemplate, err := os.Create(path.Join(testConfig.TmpDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer renderedTemplate.Close()

	if err = tpl.Execute(renderedTemplate, *testConfig); err != nil {
		t.Fatal(err)
	}

	return renderedTemplate.Name()
}

func extractPodCredSpecContents(t *testing.T, pod *corev1.Pod) string {
	if pod.Spec.SecurityContext == nil ||
		pod.Spec.SecurityContext.WindowsOptions == nil ||
		pod.Spec.SecurityContext.WindowsOptions.GMSACredentialSpec == nil {
		t.Fatalf("No pod cred spec")
	}
	return *pod.Spec.SecurityContext.WindowsOptions.GMSACredentialSpec
}

func extractContainerCredSpecContents(t *testing.T, pod *corev1.Pod, containerName string) string {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			if container.SecurityContext == nil ||
				container.SecurityContext.WindowsOptions == nil ||
				container.SecurityContext.WindowsOptions.GMSACredentialSpec == nil {
				t.Fatalf("No cred spec for container %q", containerName)
			}
			return *container.SecurityContext.WindowsOptions.GMSACredentialSpec
		}
	}

	t.Fatalf("Did not find any container named %q", containerName)
	panic("won't happen, but required by the compiler")
}
