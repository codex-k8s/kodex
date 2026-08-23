package admissioncontroller

import (
	"errors"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

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
