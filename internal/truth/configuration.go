package truth

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
)

func configurationReferences(spec *corev1.PodSpec, namespace, podName string) []ConfigurationReference {
	refs := make([]ConfigurationReference, 0)
	if spec.ServiceAccountName != "" {
		refs = append(refs, ConfigurationReference{Pod: podName, Location: "pod.spec.serviceAccountName", ReferenceType: "identity", Kind: "ServiceAccount", Namespace: namespace, Name: spec.ServiceAccountName})
	}
	for _, container := range append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...) {
		for _, envFrom := range container.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				refs = append(refs, ConfigurationReference{Pod: podName, Container: container.Name, Location: "envFrom", ReferenceType: "environment", Kind: "ConfigMap", Namespace: namespace, Name: envFrom.ConfigMapRef.Name})
			}
			if envFrom.SecretRef != nil {
				refs = append(refs, ConfigurationReference{Pod: podName, Container: container.Name, Location: "envFrom", ReferenceType: "environment", Kind: "Secret", Namespace: namespace, Name: envFrom.SecretRef.Name})
			}
		}
		for _, env := range container.Env {
			if env.ValueFrom == nil {
				continue
			}
			if env.ValueFrom.ConfigMapKeyRef != nil {
				refs = append(refs, ConfigurationReference{Pod: podName, Container: container.Name, Location: "env.valueFrom", ReferenceType: "environment-key", Kind: "ConfigMap", Namespace: namespace, Name: env.ValueFrom.ConfigMapKeyRef.Name})
			}
			if env.ValueFrom.SecretKeyRef != nil {
				refs = append(refs, ConfigurationReference{Pod: podName, Container: container.Name, Location: "env.valueFrom", ReferenceType: "environment-key", Kind: "Secret", Namespace: namespace, Name: env.ValueFrom.SecretKeyRef.Name})
			}
		}
	}
	for _, volume := range spec.Volumes {
		if volume.ConfigMap != nil {
			refs = append(refs, ConfigurationReference{Pod: podName, Location: "volume/" + volume.Name, ReferenceType: "volume", Kind: "ConfigMap", Namespace: namespace, Name: volume.ConfigMap.Name})
		}
		if volume.Secret != nil {
			refs = append(refs, ConfigurationReference{Pod: podName, Location: "volume/" + volume.Name, ReferenceType: "volume", Kind: "Secret", Namespace: namespace, Name: volume.Secret.SecretName})
		}
		if volume.Projected != nil {
			for _, source := range volume.Projected.Sources {
				if source.ConfigMap != nil {
					refs = append(refs, ConfigurationReference{Pod: podName, Location: "volume/" + volume.Name + "/projected", ReferenceType: "projected-volume", Kind: "ConfigMap", Namespace: namespace, Name: source.ConfigMap.Name})
				}
				if source.Secret != nil {
					refs = append(refs, ConfigurationReference{Pod: podName, Location: "volume/" + volume.Name + "/projected", ReferenceType: "projected-volume", Kind: "Secret", Namespace: namespace, Name: source.Secret.Name})
				}
			}
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		left, right := configurationReferenceKey(refs[i]), configurationReferenceKey(refs[j])
		if left == right {
			return refs[i].Pod < refs[j].Pod
		}
		return left < right
	})
	return uniqueConfigurationReferences(refs)
}

func configurationReferenceKey(ref ConfigurationReference) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s", ref.Container, ref.Location, ref.ReferenceType, ref.Kind, ref.Namespace, ref.Name)
}

func uniqueConfigurationReferences(refs []ConfigurationReference) []ConfigurationReference {
	result := make([]ConfigurationReference, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		key := configurationReferenceKey(ref)
		if !seen[key] {
			seen[key] = true
			result = append(result, ref)
		}
	}
	return result
}

func configurationObservation(ref ConfigurationReference, expected bool) Observation {
	status := StatusMatch
	value := fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
	if !expected {
		status = StatusMismatch
	}
	return Observation{Kind: ref.Kind, Subject: configurationSubject(ref), Value: value, Expected: value, Source: ref.Location, Method: "kubernetes-api", Status: status}
}

func configurationSubject(ref ConfigurationReference) string {
	if ref.Pod != "" && ref.Container != "" {
		return fmt.Sprintf("pod/%s/container/%s/%s", ref.Pod, ref.Container, ref.Name)
	}
	if ref.Pod != "" {
		return fmt.Sprintf("pod/%s/%s", ref.Pod, ref.Name)
	}
	return fmt.Sprintf("deployment/%s", ref.Name)
}
