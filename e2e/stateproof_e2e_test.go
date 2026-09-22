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
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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

	t.Run("rollout-selects-only-current-ready-revision", func(t *testing.T) {
		oldImage := discoverRuntimeImage(t, client, ns.Name, "rollout-old", "registry.k8s.io/pause:3.10")
		newImage := discoverRuntimeImage(t, client, ns.Name, "rollout-new", "registry.k8s.io/pause:3.9")
		dep := createDeployment(t, client, ns.Name, "rollout", []corev1.Container{{Name: "api", Image: oldImage}}, nil)
		dep.Spec.MinReadySeconds = 60 // preserves the old Pod while the new Pod becomes Ready.
		if _, err := client.AppsV1().Deployments(ns.Name).Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		waitAvailable(t, client, ns.Name, dep.Name)
		runCLI(t, binary, 0, "verify", "deployment", dep.Name, "-n", ns.Name)
		dep = getDeployment(t, client, ns.Name, dep.Name)
		dep.Spec.Template.Spec.Containers[0].Image = newImage
		if _, err := client.AppsV1().Deployments(ns.Name).Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		waitForCurrentAndOldRevisionPods(t, client, ns.Name, dep.Name)
		output := runCLI(t, binary, 0, "evidence", "deployment", dep.Name, "-n", ns.Name, "--json")
		var result struct {
			Summary struct {
				Observed int `json:"observed"`
			} `json:"summary"`
			Findings []struct {
				Code string `json:"code"`
			} `json:"findings"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("decode rollout evidence: %v\n%s", err, output)
		}
		if result.Summary.Observed != 1 {
			t.Fatalf("current verification must count exactly one ready current Pod, got %d", result.Summary.Observed)
		}
		foundOld := false
		for _, finding := range result.Findings {
			if finding.Code == "old-revision-pod" {
				foundOld = true
			}
		}
		if !foundOld {
			t.Fatalf("expected old-revision evidence in %s", output)
		}
	})

	t.Run("rbac-denial-is-operational-error", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "rbac", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		kubeconfig := restrictedKubeconfig(t, client, config, ns.Name)
		output := runCLIWithEnv(t, binary, 1, []string{"KUBECONFIG=" + kubeconfig}, "verify", "deployment", dep.Name, "-n", ns.Name)
		if !strings.Contains(strings.ToLower(string(output)), "replicasets") || !strings.Contains(strings.ToLower(string(output)), "forbidden") {
			t.Fatalf("expected actionable ReplicaSet authorization error, got %s", output)
		}
	})

	t.Run("expected-digest-gate", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "gate", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		digest := digestOf(podRuntimeImage(t, client, ns.Name, dep.Name, "api"))
		output := runCLI(t, binary, 0, "verify", "workload", "deployment/"+dep.Name, "-n", ns.Name, "--expected-digest", "api="+digest, "--json")
		if !strings.Contains(string(output), "EXPECTED_DIGEST_MATCH") {
			t.Fatalf("expected gate reason in %s", output)
		}
		wrong := replaceDigest(digest)
		output = runCLI(t, binary, 2, "verify", "workload", "deployment/"+dep.Name, "-n", ns.Name, "--expected-digest", "api="+wrong, "--json")
		if !strings.Contains(string(output), "EXPECTED_DIGEST_MISMATCH") {
			t.Fatalf("expected mismatch reason in %s", output)
		}
	})

	t.Run("expected-digest-gate-multi-container-name-matching", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "gate-multi", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}, {Name: "worker", Image: "registry.k8s.io/pause:3.9"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		api := digestOf(podRuntimeImage(t, client, ns.Name, dep.Name, "api"))
		worker := digestOf(podRuntimeImage(t, client, ns.Name, dep.Name, "worker"))
		runCLI(t, binary, 0, "verify", "workload", "deployment/"+dep.Name, "-n", ns.Name, "--expected-digest", "api="+api, "--expected-digest", "worker="+worker)
		output := runCLI(t, binary, 2, "verify", "workload", "deployment/"+dep.Name, "-n", ns.Name, "--expected-digest", "api="+worker, "--expected-digest", "worker="+api, "--json")
		if strings.Count(string(output), "EXPECTED_DIGEST_MISMATCH") < 2 {
			t.Fatalf("both named containers must fail: %s", output)
		}
	})

	t.Run("expected-digest-gate-rbac-denial", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "gate-rbac", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		kubeconfig := restrictedKubeconfig(t, client, config, ns.Name)
		runCLIWithEnv(t, binary, 1, []string{"KUBECONFIG=" + kubeconfig}, "verify", "workload", "deployment/"+dep.Name, "-n", ns.Name, "--expected-digest", "api=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	})

	t.Run("attestation-lifecycle", func(t *testing.T) {
		dep := createDeployment(t, client, ns.Name, "attest-dep", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, nil)
		waitAvailable(t, client, ns.Name, dep.Name)
		pinFromRuntime(t, client, ns.Name, dep.Name, false)

		tempDir := t.TempDir()
		keyPath := filepath.Join(tempDir, "signing.key")
		pubPath := filepath.Join(tempDir, "signing.key.pub")
		attestationPath := filepath.Join(tempDir, "attestation.json")

		// 1. Keygen
		runCLI(t, binary, 0, "keygen", "--output", keyPath)

		// 2. Attest successful match
		digest := digestOf(podRuntimeImage(t, client, ns.Name, dep.Name, "api"))
		runCLI(t, binary, 0, "attest", "workload", "deployment/"+dep.Name, "-n", ns.Name, "--signing-key", keyPath, "--output", attestationPath, "--expected-digest", "api="+digest)

		// 3. Offline verify attestation
		output := runCLI(t, binary, 0, "verify-attestation", attestationPath, "--public-key", pubPath, "--json")
		if !strings.Contains(string(output), `"valid": true`) {
			t.Fatalf("expected valid attestation verification report, got: %s", output)
		}

		// 4. Verify tampered attestation fails
		data, err := os.ReadFile(attestationPath)
		if err != nil {
			t.Fatalf("read attestation: %v", err)
		}
		tamperedData := strings.Replace(string(data), `"deployment_name": "attest-dep"`, `"deployment_name": "tampered-dep"`, 1)
		tamperedPath := filepath.Join(tempDir, "tampered.json")
		if err := os.WriteFile(tamperedPath, []byte(tamperedData), 0644); err != nil {
			t.Fatalf("write tampered attestation: %v", err)
		}
		runCLI(t, binary, 2, "verify-attestation", tamperedPath, "--public-key", pubPath)

		// 5. Attest on mismatching deployment is refused
		depMismatched := createDeployment(t, client, ns.Name, "attest-mismatch", []corev1.Container{{Name: "api", Image: "registry.k8s.io/pause:3.10"}}, nil)
		waitAvailable(t, client, ns.Name, depMismatched.Name)
		// Not pinned to runtime image ID -> mismatch
		runCLI(t, binary, 2, "attest", "workload", "deployment/"+depMismatched.Name, "-n", ns.Name, "--signing-key", keyPath, "--output", filepath.Join(tempDir, "should-not-exist.json"))
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
func discoverRuntimeImage(t *testing.T, c kubernetes.Interface, ns, name, image string) string {
	t.Helper()
	dep := createDeployment(t, c, ns, name, []corev1.Container{{Name: "api", Image: image}}, nil)
	waitAvailable(t, c, ns, dep.Name)
	return podRuntimeImage(t, c, ns, name, "api")
}
func podRuntimeImage(t *testing.T, c kubernetes.Interface, ns, name, container string) string {
	t.Helper()
	pods, err := c.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + name})
	if err != nil || len(pods.Items) != 1 {
		t.Fatalf("get runtime pod: %v (%d pods)", err, len(pods.Items))
	}
	for _, status := range pods.Items[0].Status.ContainerStatuses {
		if status.Name == container && status.ImageID != "" {
			return runtimeReference(status.ImageID)
		}
	}
	t.Fatalf("runtime image ID missing for %s/%s", name, container)
	return ""
}
func waitForCurrentAndOldRevisionPods(t *testing.T, c kubernetes.Interface, ns, name string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dep := getDeployment(t, c, ns, name)
		revision := dep.Annotations["deployment.kubernetes.io/revision"]
		sets, err := c.AppsV1().ReplicaSets(ns).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		current, old := map[string]bool{}, map[string]bool{}
		for _, rs := range sets.Items {
			for _, owner := range rs.OwnerReferences {
				if owner.Kind == "Deployment" && owner.Name == name {
					if rs.Annotations["deployment.kubernetes.io/revision"] == revision {
						current[rs.Name] = true
					} else {
						old[rs.Name] = true
					}
				}
			}
		}
		pods, err := c.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + name})
		if err != nil {
			t.Fatal(err)
		}
		currentReady, oldPresent := false, false
		for _, pod := range pods.Items {
			owner := ""
			for _, ref := range pod.OwnerReferences {
				if ref.Kind == "ReplicaSet" {
					owner = ref.Name
				}
			}
			if current[owner] && podReady(&pod) {
				currentReady = true
			}
			if old[owner] && pod.DeletionTimestamp == nil {
				oldPresent = true
			}
		}
		if currentReady && oldPresent {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("rollout did not produce a ready current Pod and retained old revision Pod within %s", timeout)
}
func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
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
func digestOf(reference string) string {
	if i := strings.LastIndex(reference, "@"); i >= 0 {
		return reference[i+1:]
	}
	return ""
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
func restrictedKubeconfig(t *testing.T, c kubernetes.Interface, base *rest.Config, namespace string) string {
	t.Helper()
	sa, err := c.CoreV1().ServiceAccounts(namespace).Create(context.Background(), &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "stateproof-denied"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "stateproof-deployment-only"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get"}}}}
	if _, err := c.RbacV1().Roles(namespace).Create(context.Background(), role, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RbacV1().RoleBindings(namespace).Create(context.Background(), &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "stateproof-deployment-only"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: sa.Name, Namespace: namespace}}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: role.Name}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	token, err := c.CoreV1().ServiceAccounts(namespace).CreateToken(context.Background(), sa.Name, &authenticationv1.TokenRequest{}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "restricted-kubeconfig")
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["cluster"] = &clientcmdapi.Cluster{Server: base.Host, CertificateAuthorityData: base.CAData, InsecureSkipTLSVerify: base.Insecure}
	kubeconfig.AuthInfos["stateproof"] = &clientcmdapi.AuthInfo{Token: token.Status.Token}
	kubeconfig.Contexts["restricted"] = &clientcmdapi.Context{Cluster: "cluster", AuthInfo: "stateproof", Namespace: namespace}
	kubeconfig.CurrentContext = "restricted"
	if err := clientcmd.WriteToFile(*kubeconfig, path); err != nil {
		t.Fatal(err)
	}
	return path
}
func runCLI(t *testing.T, binary string, want int, args ...string) []byte {
	return runCLIWithEnv(t, binary, want, nil, args...)
}
func runCLIWithEnv(t *testing.T, binary string, want int, extraEnv []string, args ...string) []byte {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = append(os.Environ(), extraEnv...)
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
	return output
}
