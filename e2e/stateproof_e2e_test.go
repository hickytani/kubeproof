//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const timeout = 3 * time.Minute

// This test deliberately creates fixtures through the Kubernetes API. That
// mutation is test-harness-only; the StateProof binary remains read-only.
func TestStateProofAgainstRealKubernetesAPI(t *testing.T) {
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatalf("REAL E2E REQUIRES KUBECONFIG: %v", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := client.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "stateproof-e2e-"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.CoreV1().Namespaces().Delete(context.Background(), ns.Name, metav1.DeleteOptions{}) })
	binary := buildBinary(t)

	t.Run("insufficient-tag-evidence-and-digest-match", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "match", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		runCLI(t, binary, 3, "evidence", "deployment", dep.Name, "-n", ns.Name, "--json")
		pinFromRuntime(t, client, ns.Name, dep.Name, false)
		runCLI(t, binary, 0, "verify", "deployment", dep.Name, "-n", ns.Name)
		runCLI(t, binary, 0, "evidence", "deployment", dep.Name, "-n", ns.Name, "--json")
		runCLI(t, binary, 0, "explain", "deployment", dep.Name, "-n", ns.Name)
		runCLI(t, binary, 0, "scan", ns.Name)
	})

	t.Run("digest-mismatch", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "mismatch", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		pinFromRuntime(t, client, ns.Name, dep.Name, false)
		dep = getDeployment(t, client, ns.Name, dep.Name)
		dep.Spec.Template.Spec.Containers[0].Image = replaceDigest(dep.Spec.Template.Spec.Containers[0].Image)
		if _, err := client.AppsV1().Deployments(ns.Name).Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		runCLI(t, binary, 2, "evidence", "deployment", dep.Name, "-n", ns.Name, "--json")
	})

	t.Run("container-swap", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "swap", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}, {Name: "sidecar", Image: "registry.k8s.io/pause:3.9"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		pinFromRuntime(t, client, ns.Name, dep.Name, false)
		dep = getDeployment(t, client, ns.Name, dep.Name)
		dep.Spec.Template.Spec.Containers[0].Image, dep.Spec.Template.Spec.Containers[1].Image = dep.Spec.Template.Spec.Containers[1].Image, dep.Spec.Template.Spec.Containers[0].Image
		if _, err := client.AppsV1().Deployments(ns.Name).Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		runCLI(t, binary, 2, "verify", "deployment", dep.Name, "-n", ns.Name)
	})

	t.Run("init-container", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "init", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, []corev1.Container{{Name: "init", Image: "busybox:1.36.1", Command: []string{"sh", "-c", "true"}}})
		waitAvailable(t, client, ns.Name, dep.Name)
		pinFromRuntime(t, client, ns.Name, dep.Name, true)
		runCLI(t, binary, 0, "verify", "deployment", dep.Name, "-n", ns.Name)
	})
}

func buildBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stateproof")
	command := exec.Command("go", "build", "-o", path, ".")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return path
}
func createDeployment(t *testing.T, c kubernetes.Interface, ns, name string, containers, init []corev1.Container) *appsv1.Deployment {
	t.Helper()
	replicas := int32(1)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}}, Spec: corev1.PodSpec{Containers: containers, InitContainers: init}}}}
	result, err := c.AppsV1().Deployments(ns).Create(context.Background(), dep, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func waitAvailable(t *testing.T, c kubernetes.Interface, ns, name string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dep := getDeployment(t, c, ns, name)
		if dep.Status.AvailableReplicas == 1 {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("Deployment %s did not become available within %s", name, timeout)
}
func getDeployment(t *testing.T, c kubernetes.Interface, ns, name string) *appsv1.Deployment {
	t.Helper()
	dep, err := c.AppsV1().Deployments(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return dep
}
func pinFromRuntime(t *testing.T, c kubernetes.Interface, ns, name string, includeInit bool) {
	t.Helper()
	dep := getDeployment(t, c, ns, name)
	dep.Spec.Paused = true
	pods, err := c.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + name})
	if err != nil || len(pods.Items) != 1 {
		t.Fatalf("get runtime pod: %v (%d pods)", err, len(pods.Items))
	}
	pod := pods.Items[0]
	byName := map[string]string{}
	for _, s := range pod.Status.ContainerStatuses {
		byName[s.Name] = runtimeReference(s.ImageID)
	}
	for i := range dep.Spec.Template.Spec.Containers {
		dep.Spec.Template.Spec.Containers[i].Image = byName[dep.Spec.Template.Spec.Containers[i].Name]
	}
	if includeInit {
		for _, s := range pod.Status.InitContainerStatuses {
			byName[s.Name] = runtimeReference(s.ImageID)
		}
		for i := range dep.Spec.Template.Spec.InitContainers {
			dep.Spec.Template.Spec.InitContainers[i].Image = byName[dep.Spec.Template.Spec.InitContainers[i].Name]
		}
	}
	if _, err := c.AppsV1().Deployments(ns).Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}
func runtimeReference(id string) string {
	if i := strings.Index(id, "://"); i >= 0 {
		return id[i+3:]
	}
	return id
}
func replaceDigest(image string) string {
	if len(image) == 0 {
		return image
	}
	last := image[len(image)-1:]
	if last == "0" {
		return image[:len(image)-1] + "1"
	}
	return image[:len(image)-1] + "0"
}
func runCLI(t *testing.T, binary string, want int, args ...string) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	actual := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			actual = exit.ExitCode()
		} else {
			t.Fatalf("run CLI: %v", err)
		}
	}
	if actual != want {
		var document map[string]any
		_ = json.Unmarshal(output, &document)
		t.Fatalf("want exit %d, got %d\n%s\njson=%v", want, actual, output, document)
	}
	fmt.Printf("STATEPROOF E2E scenario %s: PASS\n", strings.Join(args[:2], " "))
}
