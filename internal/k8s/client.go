package k8s

import (
	"context"
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewClient() (*kubernetes.Clientset, error) {
	return NewClientWithContext("")
}

func NewClientWithContext(contextName string) (*kubernetes.Clientset, error) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		return kubernetes.NewForConfig(config)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeConfig := os.Getenv("KUBECONFIG"); kubeConfig != "" {
		loadingRules.Precedence = []string{kubeConfig}
	}
	configOverrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		configOverrides.CurrentContext = contextName
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func GetDeployment(client kubernetes.Interface, namespace, name string) (*appsv1.Deployment, error) {
	return client.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func GetReplicaSetsForDeployment(client kubernetes.Interface, namespace string, deployment *appsv1.Deployment) ([]appsv1.ReplicaSet, error) {
	list, err := client.AppsV1().ReplicaSets(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	matches := make([]appsv1.ReplicaSet, 0)
	for _, rs := range list.Items {
		for _, owner := range rs.OwnerReferences {
			if owner.Kind == "Deployment" && owner.Name == deployment.Name {
				matches = append(matches, rs)
				break
			}
		}
	}
	return matches, nil
}

func GetDeploymentPods(client kubernetes.Interface, namespace string, deployment *appsv1.Deployment) ([]corev1.Pod, error) {
	replicaSets, err := GetReplicaSetsForDeployment(client, namespace, deployment)
	if err != nil {
		return nil, fmt.Errorf("list Deployment ReplicaSets: %w", err)
	}
	ownedReplicaSets := make(map[string]struct{}, len(replicaSets))
	for _, rs := range replicaSets {
		ownedReplicaSets[rs.Name] = struct{}{}
	}
	list, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	matches := make([]corev1.Pod, 0)
	for _, pod := range list.Items {
		for _, owner := range pod.OwnerReferences {
			if owner.Kind == "ReplicaSet" {
				if _, ok := ownedReplicaSets[owner.Name]; ok {
					matches = append(matches, pod)
					break
				}
			}
		}
	}
	return matches, nil
}

func GetDeploymentByName(client kubernetes.Interface, namespace, name string) (*appsv1.Deployment, error) {
	return client.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func GetDeploymentResults(client kubernetes.Interface, namespace, name string) (DeploymentScanResult, error) {
	dep, err := GetDeploymentByName(client, namespace, name)
	if err != nil {
		return DeploymentScanResult{}, err
	}
	podList, err := GetDeploymentPods(client, namespace, dep)
	if err != nil {
		return DeploymentScanResult{}, err
	}

	result := DeploymentScanResult{
		Deployment: *dep,
		Pods:       podList,
	}
	return result, nil
}

type DeploymentScanResult struct {
	Deployment appsv1.Deployment
	Pods       []corev1.Pod
}

func GetNamespaceDeployments(client kubernetes.Interface, ns string) ([]appsv1.Deployment, error) {
	list, err := client.AppsV1().Deployments(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func DescribeWorkloadType(client kubernetes.Interface, namespace, name string) (string, error) {
	for _, candidate := range []string{"deployment", "statefulset", "daemonset"} {
		switch candidate {
		case "deployment":
			if _, err := client.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
				return "Deployment", nil
			}
		case "statefulset":
			if _, err := client.AppsV1().StatefulSets(namespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
				return "StatefulSet", nil
			}
		case "daemonset":
			if _, err := client.AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
				return "DaemonSet", nil
			}
		}
	}
	return "", fmt.Errorf("resource %s/%s not found", namespace, name)
}

func GetStatefulSetByName(client kubernetes.Interface, namespace, name string) (*appsv1.StatefulSet, error) {
	return client.AppsV1().StatefulSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func GetControllerRevisionsForStatefulSet(client kubernetes.Interface, namespace string, sts *appsv1.StatefulSet) ([]appsv1.ControllerRevision, error) {
	list, err := client.AppsV1().ControllerRevisions(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	matches := make([]appsv1.ControllerRevision, 0)
	for _, cr := range list.Items {
		for _, owner := range cr.OwnerReferences {
			if owner.Kind == "StatefulSet" && owner.Name == sts.Name {
				matches = append(matches, cr)
				break
			}
		}
	}
	return matches, nil
}

func GetStatefulSetPods(client kubernetes.Interface, namespace string, sts *appsv1.StatefulSet) ([]corev1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	matches := make([]corev1.Pod, 0)
	for _, pod := range list.Items {
		for _, owner := range pod.OwnerReferences {
			if owner.Kind == "StatefulSet" && owner.Name == sts.Name {
				matches = append(matches, pod)
				break
			}
		}
	}
	return matches, nil
}

func GetDaemonSetByName(client kubernetes.Interface, namespace, name string) (*appsv1.DaemonSet, error) {
	return client.AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func GetControllerRevisionsForDaemonSet(client kubernetes.Interface, namespace string, ds *appsv1.DaemonSet) ([]appsv1.ControllerRevision, error) {
	list, err := client.AppsV1().ControllerRevisions(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	matches := make([]appsv1.ControllerRevision, 0)
	for _, cr := range list.Items {
		for _, owner := range cr.OwnerReferences {
			if owner.Kind == "DaemonSet" && owner.Name == ds.Name {
				matches = append(matches, cr)
				break
			}
		}
	}
	return matches, nil
}

func GetDaemonSetPods(client kubernetes.Interface, namespace string, ds *appsv1.DaemonSet) ([]corev1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	matches := make([]corev1.Pod, 0)
	for _, pod := range list.Items {
		for _, owner := range pod.OwnerReferences {
			if owner.Kind == "DaemonSet" && owner.Name == ds.Name {
				matches = append(matches, pod)
				break
			}
		}
	}
	return matches, nil
}

