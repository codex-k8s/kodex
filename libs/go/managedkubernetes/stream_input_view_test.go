package managedkubernetes

import "testing"

// TestStreamInputViewResourceKeepsCollectionAndPodScopes prevents Pod assumptions from hiding watches or metadata from widening stream scope.
// TestStreamInputViewResourceKeepsCollectionAndPodScopes не позволяет Pod assumptions скрыть watches либо metadata расширить stream scope.
func TestStreamInputViewResourceKeepsCollectionAndPodScopes(t *testing.T) {
	for _, scope := range []string{"namespace", "cluster", "all"} {
		t.Run(scope, func(t *testing.T) {
			address := ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "example"}
			operation := "kubernetes.watch"
			namespace := "example"
			if scope == "cluster" {
				address.Namespaced = false
				address.Namespace = ""
				namespace = "@cluster"
			}
			if scope == "all" {
				address.Namespace = ""
				address.AllNamespaces = true
				namespace = "@all-namespaces"
				operation = "kubernetes.watch.all-namespaces"
			}
			if ValidateStreamInputViewResource(operation, namespace, "", "", address) != nil {
				t.Fatal("valid collection hidden")
			}
			for _, variant := range []string{"sentinel", "pod", "name", "uid", "operation"} {
				t.Run(variant, func(t *testing.T) {
					candidate := address
					op, ns, pod := operation, namespace, ""
					switch variant {
					case "sentinel":
						ns = "other"
					case "pod":
						pod = "web"
					case "name":
						candidate.Name = "web"
					case "uid":
						candidate.UID = "30000000-0000-4000-8000-000000000001"
						candidate.Name = "web"
					case "operation":
						op = "kubernetes.exec"
					}
					if ValidateStreamInputViewResource(op, ns, pod, "", candidate) == nil {
						t.Fatal("mixed collection metadata accepted")
					}
				})
			}
		})
	}
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "example", Name: "web", UID: "30000000-0000-4000-8000-000000000001", ResourceVersion: "18"}
	if ValidateStreamInputViewResource("kubernetes.exec", "example", "web", "app", address) != nil {
		t.Fatal("Pod incarnation rejected")
	}
	address.UID = ""
	if ValidateStreamInputViewResource("kubernetes.exec", "example", "web", "app", address) == nil {
		t.Fatal("missing Pod incarnation accepted")
	}
}

// TestStreamInputViewLogsKeepsUnknownSelection distinguishes an unstored HTTP logs selection from
// privileged transports that must retain their exact container. It never fabricates a default name.
// TestStreamInputViewLogsKeepsUnknownSelection отличает unstored HTTP logs selection от privileged
// transports с обязательным exact container. Default name не выдумывается.
func TestStreamInputViewLogsKeepsUnknownSelection(t *testing.T) {
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "example", Name: "web", UID: "30000000-0000-4000-8000-000000000001", ResourceVersion: "18"}
	for _, container := range []string{"", "app"} {
		if ValidateStreamInputViewResource("kubernetes.logs", "example", "web", container, address) != nil {
			t.Fatal("HTTP logs selection hidden")
		}
	}
	for _, operation := range []string{"kubernetes.exec", "kubernetes.attach", "kubernetes.cp.upload", "kubernetes.cp.download", "kubernetes.port-forward"} {
		if ValidateStreamInputViewResource(operation, "example", "web", "", address) == nil {
			t.Fatal("privileged container witness omitted", operation)
		}
	}
	if ValidateStreamInputViewResource("kubernetes.logs", "example", "web", "wrong/name", address) == nil {
		t.Fatal("malformed explicit logs container accepted")
	}
	address.UID = ""
	if ValidateStreamInputViewResource("kubernetes.logs", "example", "web", "", address) == nil {
		t.Fatal("HTTP logs lost original Pod identity")
	}
}
