// Package credentialmaterializer stores integration credentials in one configured Kubernetes Secret
// and returns only location, version and content-digest metadata to the control plane.
// Пакет credentialmaterializer сохраняет учётные данные интеграций в одном настроенном Secret Kubernetes
// и возвращает control-plane только метаданные размещения, версии и дайджеста содержимого.
package credentialmaterializer

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

// Kubernetes materializes credentials into one configured existing Secret through an injected Kubernetes client.
// It returns versioned metadata rather than exposing credential bytes to its caller.
// Kubernetes размещает учётные данные в одном настроенном существующем Secret через переданный клиент Kubernetes.
// Вызывающему коду возвращаются версионированные метаданные, а не байты учётных данных.
type Kubernetes struct {
	client     kubernetes.Interface
	namespace  string
	secretName string
}

// InCluster builds a Kubernetes client from the in-cluster identity with the supplied HTTP timeout,
// then delegates namespace and Secret-name validation to New.
// InCluster создаёт клиент Kubernetes с внутрикластерной идентичностью и переданным таймаутом HTTP,
// затем передаёт проверку пространства имён и имени Secret конструктору New.
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

// New requires a Kubernetes client and valid namespace and Secret labels;
// construction performs no API requests and does not create the Secret.
// New требует клиент Kubernetes и корректные метки пространства имён и Secret;
// конструктор не обращается к API и не создаёт Secret.
func New(client kubernetes.Interface, namespace, secretName string) (*Kubernetes, error) {
	if client == nil || !validDNSLabel(namespace) || !validDNSLabel(secretName) {
		return nil, errors.New("integration credential materializer configuration is invalid")
	}
	return &Kubernetes{client: client, namespace: namespace, secretName: secretName}, nil
}

// Materialize copies and hashes a nonempty credential, then updates one key of an existing Secret with bounded conflict retries.
// Equal existing bytes are reusable; different bytes under that key fail with a conflict. Other keys are retained, and the private input copy is cleared on return.
// Materialize копирует и хеширует непустые учётные данные, затем обновляет один ключ существующего Secret с ограниченными повторами при конфликте.
// Совпадающие байты можно использовать повторно; иное значение того же ключа вызывает конфликт. Остальные ключи сохраняются, частная копия входа очищается при выходе.
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

// result constructs a descriptor from the configured location, returned UID/resource version and content digest.
// It includes no credential value and performs no additional Kubernetes read.
// result составляет дескриптор из настроенного размещения, возвращённых UID/версии ресурса и дайджеста содержимого.
// Значение учётных данных в него не входит; дополнительного чтения Kubernetes нет.
func (materializer *Kubernetes) result(uid types.UID, resourceVersion, key string, digest [sha256.Size]byte) platformservice.MaterializedCredential {
	return platformservice.MaterializedCredential{
		SecretRef:             fmt.Sprintf("%s/%s#%s", materializer.namespace, materializer.secretName, key),
		SecretUID:             string(uid),
		SecretResourceVersion: resourceVersion,
		ContentSHA256:         hex.EncodeToString(digest[:]),
	}
}

// cloneData deep-copies the Secret data map and every value slice, reserving capacity for one added key.
// Subsequent key insertion cannot mutate the fetched object's data map.
// cloneData глубоко копирует карту данных Secret и каждый срез значений, оставляя ёмкость для одного нового ключа.
// Последующее добавление ключа не меняет карту данных полученного объекта.
func cloneData(input map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(input)+1)
	for key, value := range input {
		result[key] = append([]byte(nil), value...)
	}
	return result
}

// validDNSLabel checks namespace and Secret label syntax through the shared resource-name parser;
// Kubernetes ownership, credentials and materialization effects remain separate checks.
// validDNSLabel проверяет синтаксис меток пространства и Secret общим разбором имён ресурсов;
// владение Kubernetes, реквизиты и эффекты материализации проверяются отдельно.
func validDNSLabel(value string) bool {
	return runtimecontract.ValidDNSLabel(value)
}

// validDataKey permits 1..253 ASCII letters, digits, hyphens, underscores and dots for a Secret data key.
// It is deliberately distinct from the lowercase DNS-label grammar used for resource names.
// validDataKey разрешает от 1 до 253 букв ASCII, цифр, дефисов, подчёркиваний и точек в ключе данных Secret.
// Эта грамматика намеренно отличается от строчных DNS-меток имён ресурсов.
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
