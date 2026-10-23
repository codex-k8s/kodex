package admissioncontroller

import (
	"errors"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// inClusterClient loads the Pod's standard in-cluster identity, sets the request timeout and creates a typed Kubernetes client.
// It returns safe construction errors and does not issue API requests or widen the identity's permissions.
// inClusterClient загружает стандартную внутрикластерную идентичность Pod, задаёт таймаут запросов и создаёт типизированный клиент Kubernetes.
// Возвращаются безопасные ошибки создания; запросы API не выполняются и права идентичности не расширяются.
func inClusterClient(timeout time.Duration) (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load image admission Kubernetes configuration")
	}
	config.Timeout = timeout
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.New("create image admission Kubernetes client")
	}
	return client, nil
}
