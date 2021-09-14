package integrationtests

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/require"
	"gotest.tools/poll"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func clientConfig(t *testing.T) *rest.Config {
	kubeConfigPath, err := homedir.Expand(kubeconfig())
	if err != nil {
		t.Fatal(err)
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	return config
}

func kubeClient(t *testing.T) kubernetes.Interface {
	config := clientConfig(t)
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func dynamicClient(t *testing.T) dynamic.Interface {
	config := clientConfig(t)
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

// getNodes returns the nodes present in the cluster.
func getNodes(t *testing.T) []corev1.Node {
	client := kubeClient(t)

	nodeList, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	require.Nil(t, err, "Unable to list nodes")
	return nodeList.Items
}

// nodeHasMasterTaint returns true iff node has the canonical master taint.
func nodeHasMasterTaint(node corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == "node-role.kubernetes.io/master" && taint.Effect == "NoSchedule" {
			return true
		}
	}
	return false
}

// waitForPodToComeUp waits for a pod matching `selector` to come up in `namespace`, and returns it.
func waitForPodToComeUp(t *testing.T, namespace, selector string, pollOps ...poll.SettingOp) *corev1.Pod {
	fetcher := func(client kubernetes.Interface, listOptions metav1.ListOptions) ([]interface{}, error) {
		podList, err := client.CoreV1().Pods(namespace).List(context.Background(), listOptions)
		if err == nil {
			result := make([]interface{}, len(podList.Items))
			for i, item := range podList.Items {
				result[i] = item
			}
			return result, nil
		}
		return nil, err
	}

	rawPod := waitForKubeObject(t, fetcher, namespace, selector, "pod", pollOps...)
	pod := rawPod.(corev1.Pod)
	return &pod
}

// waitForReplicaSet waits for a replica set matching `selector` to come up in `namespace`, and returns it.
func waitForReplicaSet(t *testing.T, namespace, selector string, pollOps ...poll.SettingOp) *appsv1.ReplicaSet {
	fetcher := func(client kubernetes.Interface, listOptions metav1.ListOptions) ([]interface{}, error) {
		rsList, err := client.AppsV1().ReplicaSets(namespace).List(context.Background(), listOptions)
		if err == nil {
			result := make([]interface{}, len(rsList.Items))
			for i, item := range rsList.Items {
				result[i] = item
			}
			return result, nil
		}
		return nil, err
	}

	rawReplicaSet := waitForKubeObject(t, fetcher, namespace, selector, "replica set", pollOps...)
	replicaSet := rawReplicaSet.(appsv1.ReplicaSet)
	return &replicaSet
}

// waitForReplicaSetGen1 waits for a given replica set to have its `Status.ObservedGeneration` field grow to > 0
// Comes in handy to wait for k8s to reach a decision for a given replicaset
func waitForReplicaSetGen1(t *testing.T, namespace, selector string, pollOps ...poll.SettingOp) *appsv1.ReplicaSet {
	replicaSet := waitForReplicaSet(t, namespace, selector, pollOps...)
	var err error

	client := kubeClient(t)

	pollingFunc := func(_ poll.LogT) poll.Result {
		replicaSet, err = client.AppsV1().ReplicaSets(namespace).Get(context.Background(), replicaSet.Name, metav1.GetOptions{})
		if err != nil {
			return poll.Error(err)
		}

		if replicaSet.Status.ObservedGeneration == 0 {
			return poll.Continue("replicaset %s is still at generation 0", replicaSet.Name)
		}
		return poll.Success()
	}

	poll.WaitOn(t, pollingFunc, pollOps...)

	return replicaSet
}

// waitForKubeObject waits for fetcher to return a list of one object matching `selector` in `namespace`, and returns it.
func waitForKubeObject(t *testing.T, fetcher func(kubernetes.Interface, metav1.ListOptions) ([]interface{}, error), namespace, selector, displayableName string, pollOps ...poll.SettingOp) (object interface{}) {
	client := kubeClient(t)
	listOptions := metav1.ListOptions{LabelSelector: selector}

	pollingFunc := func(_ poll.LogT) poll.Result {
		list, err := fetcher(client, listOptions)

		if err != nil {
			return poll.Error(err)
		}

		switch len(list) {
		case 0:
			return poll.Continue("no %s matching %s in namespace %s", displayableName, selector, namespace)
		case 1:
			object = list[0]
			return poll.Success()
		default:
			err = fmt.Errorf("expected no more than 1 %s matching %s in namespace %s, got %v", displayableName, selector, namespace, len(list))
			return poll.Error(err)
		}
	}

	poll.WaitOn(t, pollingFunc, pollOps...)

	return
}

const testNamespacePrefix = "gmsa-webhook-test-"

// value from https://github.com/kubernetes/kubernetes/blob/5e58841cce77d4bc13713ad2b91fa0d961e69192/pkg/volume/util/attach_limit.go#L53-L54
// removed so we didn't need to import the entire kubernetes source which is technically not supported
const resourceNameLengthLimit = 63

// createNamespace creates a new namespace, and fails the test if it already exists.
// if passed an empty string, it picks a random name and returns it.
func createNamespace(t *testing.T, name string) string {
	if name == "" {
		name = testNamespacePrefix + randomHexString(t, resourceNameLengthLimit-len(testNamespacePrefix))
	}

	runKubectlCommandOrFail(t, "create", "namespace", name)

	return name
}

func deleteNamespace(t *testing.T, name string) {
	runKubectlCommandOrFail(t, "delete", "namespace", name)
}

func applyManifestOrFail(t *testing.T, path string) {
	runKubectlCommandOrFail(t, "apply", "-f", path)
}

func applyManifest(t *testing.T, path string) (success bool, stdout string, stderr string) {
	return runKubectlCommand(t, "apply", "-f", path)
}

func deleteManifest(t *testing.T, path string) {
	runKubectlCommandOrFail(t, "delete", "-f", path)
}

func runKubectlCommandOrFail(t *testing.T, args ...string) {
	runCommandOrFail(t, kubectl(), args...)
}

func runKubectlCommand(t *testing.T, args ...string) (success bool, stdout string, stderr string) {
	return runCommand(t, kubectl(), args...)
}

func kubectl() string {
	return fromEnv("KUBECTL", "kubectl")
}

func kubeconfig() string {
	return fromEnv("KUBECONFIG", "~/.kube/config")
}

func fromEnv(key, defaultValue string) (value string) {
	if fromEnv, present := os.LookupEnv(key); present && fromEnv != "" {
		value = fromEnv
	} else {
		value = defaultValue
	}
	return
}
