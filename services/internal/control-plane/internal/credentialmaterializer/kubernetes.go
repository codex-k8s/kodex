// Package credentialmaterializer сохраняет значения credentials интеграций в один защищённый Kubernetes Secret.
package credentialmaterializer

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

type Kubernetes struct {
	client     kubernetes.Interface
	namespace  string
	secretName string
}

func InCluster(namespace, secretName string, timeout time.Duration) (*Kubernetes, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load integration credential Kubernetes configuration")
	}
	config.Timeout = timeout
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.New("create integration credential Kubernetes client")
	}
	return New(client, namespace, secretName)
}

func New(client kubernetes.Interface, namespace, secretName string) (*Kubernetes, error) {
	if client == nil || !validDNSLabel(namespace) || !validDNSLabel(secretName) {
		return nil, errors.New("integration credential materializer configuration is invalid")
	}
	return &Kubernetes{client: client, namespace: namespace, secretName: secretName}, nil
}

func (materializer *Kubernetes) Materialize(ctx context.Context, key string, value []byte) (platformservice.MaterializedCredential, error) {
	if !validDataKey(key) || len(value) == 0 {
		return platformservice.MaterializedCredential{}, errors.New("integration credential materialization input is invalid")
	}
	credential := append([]byte(nil), value...)
	defer clear(credential)
	digest := sha256.Sum256(credential)
	var result platformservice.MaterializedCredential
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := materializer.client.CoreV1().Secrets(materializer.namespace).Get(ctx, materializer.secretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current, exists := secret.Data[key]; exists {
			if subtle.ConstantTimeCompare(current, credential) != 1 {
				return platformservice.ErrCredentialMaterializationConflict
			}
			result = materializer.result(secret.UID, secret.ResourceVersion, key, digest)
			return nil
		}
		secret.Data = cloneData(secret.Data)
		secret.Data[key] = append([]byte(nil), credential...)
		updated, err := materializer.client.CoreV1().Secrets(materializer.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		result = materializer.result(updated.UID, updated.ResourceVersion, key, digest)
		return nil
	})
	if err != nil {
		if errors.Is(err, platformservice.ErrCredentialMaterializationConflict) {
			return platformservice.MaterializedCredential{}, err
		}
		return platformservice.MaterializedCredential{}, errors.New("materialize integration credential")
	}
	return result, nil
}

func (materializer *Kubernetes) result(uid types.UID, resourceVersion, key string, digest [sha256.Size]byte) platformservice.MaterializedCredential {
	return platformservice.MaterializedCredential{
		SecretRef:             fmt.Sprintf("%s/%s#%s", materializer.namespace, materializer.secretName, key),
		SecretUID:             string(uid),
		SecretResourceVersion: resourceVersion,
		ContentSHA256:         hex.EncodeToString(digest[:]),
	}
}

func cloneData(input map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(input)+1)
	for key, value := range input {
		result[key] = append([]byte(nil), value...)
	}
	return result
}

func validDNSLabel(value string) bool {
	if value == "" || len(value) > 63 || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func validDataKey(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}
