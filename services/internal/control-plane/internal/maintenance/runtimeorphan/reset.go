package runtimeorphan

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reset проверяет обе identity до первого удаления; runtime исчезает первым.
func (k *Kubernetes) Reset(ctx context.Context) error {
	system, err := k.Client.CoreV1().Namespaces().Get(ctx, "kodex-system", metav1.GetOptions{})
	if err != nil {
		return ErrGuard
	}
	s, err := namespace(system.ObjectMeta, "kodex-system")
	if err != nil {
		return err
	}
	runtime, err := k.Client.CoreV1().Namespaces().Get(ctx, "kodex-runtime", metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return ErrGuard
	}
	if err == nil {
		r, e := namespace(runtime.ObjectMeta, "kodex-runtime")
		if e != nil || r.Profile != s.Profile {
			return ErrGuard
		}
	} else {
		runtime = nil
	}
	identity, err := k.Client.CoreV1().Namespaces().Get(ctx, "identity", metav1.GetOptions{})
	if err != nil || identity.Labels["kodex.dev/capability"] != "identity" {
		return ErrGuard
	}
	list, err := k.Client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil || list.Continue != "" {
		return ErrGuard
	}
	preserved := map[string]string{}
	for _, n := range list.Items {
		if n.Name != "kodex-system" && n.Name != "kodex-runtime" {
			preserved[n.Name] = string(n.UID)
		}
	}
	targets := []metav1.ObjectMeta{}
	if runtime != nil {
		targets = append(targets, runtime.ObjectMeta)
	}
	targets = append(targets, system.ObjectMeta)
	for _, target := range targets {
		body, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": map[string]string{"uid": string(target.UID), "resourceVersion": target.ResourceVersion}})
		r, err := http.NewRequestWithContext(ctx, http.MethodDelete, k.Endpoint+"/api/v1/namespaces/"+target.Name, bytes.NewReader(body))
		if err != nil {
			return ErrGuard
		}
		r.Header.Set("Content-Type", "application/json")
		response, err := k.HTTP.Do(r)
		if err != nil {
			return ErrGuard
		}
		response.Body.Close()
		if response.StatusCode != 200 && response.StatusCode != 202 {
			return ErrGuard
		}
		ticker := time.NewTicker(time.Second)
		for {
			current, err := k.Client.CoreV1().Namespaces().Get(ctx, target.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				break
			}
			if err != nil || current.UID != target.UID {
				ticker.Stop()
				return ErrGuard
			}
			select {
			case <-ctx.Done():
				ticker.Stop()
				return ErrGuard
			case <-ticker.C:
			}
		}
		ticker.Stop()
		for name, uid := range preserved {
			current, err := k.Client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
			if err != nil || string(current.UID) != uid {
				return ErrGuard
			}
		}
	}
	return nil
}
