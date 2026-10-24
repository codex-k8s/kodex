// Package emailprojection публикует принятый владельцем EMAIL snapshot в один Secret.
package emailprojection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

const (
	// SecretName pins the existing mailbox projection Secret updated by this publisher rather than
	// accepting a caller-selected Secret target.
	// SecretName закрепляет существующий mailbox projection Secret этого publisher вместо приёма
	// выбранной caller цели Secret.
	SecretName = "email-bridge-mailbox-projection"
	// DocumentKey names the serialized mailbox configuration entry while other Secret entries retain
	// separately materialized credential values.
	// DocumentKey обозначает запись сериализованной mailbox configuration при сохранении отдельно
	// материализованных credential values в остальных Secret entries.
	DocumentKey = "mailboxes.json"
	// maximumProjectionBytes caps both the encoded configuration and aggregate
	// document-plus-existing-credential payload at900 KiB before update.
	// maximumProjectionBytes ограничивает кодированную configuration и совокупные document и existing
	// credential payload900 KiB до update.
	maximumProjectionBytes = 900 << 10
)

var (
	// ErrInvalid reports rejected configuration/render input without exposing credential values or
	// document contents.
	// ErrInvalid обозначает отклонённые configuration либо render input без раскрытия credential
	// values либо содержимого документа.
	ErrInvalid = errors.New("email projection is invalid")
	// ErrConflict reports rollback, same-revision equivocation or exact readback/credential digest
	// mismatch as a fixed error.
	// ErrConflict обозначает rollback, same-revision equivocation либо mismatch точного readback или
	// credential digest фиксированной ошибкой.
	ErrConflict = errors.New("email projection revision conflict")
	// ErrUnavailable reports unavailable Kubernetes state or required credential material using a
	// fixed error.
	// ErrUnavailable обозначает недоступное Kubernetes состояние либо обязательный credential material
	// фиксированной ошибкой.
	ErrUnavailable = errors.New("email projection is unavailable")
)

// Receipt returns observed configuration revision/digest and Secret UID/resource version without
// including any Secret values.
// Receipt возвращает наблюдаемые configuration revision и digest и Secret UID и resource version
// без Secret values.
type Receipt struct {
	Revision                           int64
	Digest, SecretUID, ResourceVersion string
}

// Kubernetes holds the injected Kubernetes client and validated projection namespace; it owns no
// background reconciliation loop.
// Kubernetes хранит внедрённый Kubernetes client и проверенный projection namespace; фоновым
// reconciliation loop не владеет.
type Kubernetes struct {
	client    kubernetes.Interface
	namespace string
}

// InCluster constructs an in-cluster client with the supplied request timeout and maps setup
// failures to unavailable before namespace validation.
// InCluster создаёт in-cluster client с переданным request timeout и отображает setup failures в
// unavailable до проверки namespace.
func InCluster(namespace string, timeout time.Duration) (*Kubernetes, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, ErrUnavailable
	}
	config.Timeout = timeout
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, ErrUnavailable
	}
	return New(client, namespace)
}

// New requires a nonnil client and descriptor-valid namespace without reading cluster resources or
// credential material.
// New требует ненулевой client и descriptor-valid namespace без чтения cluster resources либо
// credential material.
func New(client kubernetes.Interface, namespace string) (*Kubernetes, error) {
	if client == nil || !api.DescriptorValid(api.Descriptor{Name: namespace, Generation: 1}) {
		return nil, ErrInvalid
	}
	return &Kubernetes{client: client, namespace: namespace}, nil
}

// CredentialKey validates the immutable credential descriptor before composing name.generation,
// preventing path syntax and aliases across generations.
// CredentialKey проверяет immutable credential descriptor до составления name.generation, запрещая
// path syntax и aliases разных generations.
func CredentialKey(descriptor api.Descriptor) (string, error) {
	if !api.DescriptorValid(descriptor) {
		return "", ErrInvalid
	}
	return descriptor.Name + "." + strconv.FormatInt(descriptor.Generation, 10), nil
}

// Publish updates only the document in an existing valid seeded Secret after owner acceptance,
// preserving credential bytes and monotone revision/digest under conflict retries, then performs
// exact Secret readback.
// Publish обновляет только document существующего корректного seeded Secret после owner
// acceptance, сохраняя credential bytes и монотонные revision и digest при conflict retries, затем
// выполняет точный Secret readback.
func (publisher *Kubernetes) Publish(ctx context.Context, configuration api.Configuration, credentialDigests map[string]string) (Receipt, error) {
	if api.ValidateConfiguration(configuration) != nil {
		return Receipt{}, ErrInvalid
	}
	raw, err := json.Marshal(configuration)
	if err != nil || len(raw) > maximumProjectionBytes {
		return Receipt{}, ErrInvalid
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := publisher.client.CoreV1().Secrets(publisher.namespace).Get(ctx, SecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		var previous api.Configuration
		if api.Decode(secret.Data[DocumentKey], &previous) != nil || api.ValidateConfiguration(previous) != nil {
			return ErrConflict
		}
		if previous.Revision > configuration.Revision || previous.Revision == configuration.Revision && api.Digest(previous) != api.Digest(configuration) {
			return ErrConflict
		}
		if err := validateCredentials(configuration, secret.Data, credentialDigests); err != nil {
			return err
		}
		if bytes.Equal(secret.Data[DocumentKey], raw) {
			return nil
		}
		size := len(raw)
		for key, value := range secret.Data {
			if key != DocumentKey {
				size += len(key) + len(value)
			}
		}
		if size > maximumProjectionBytes {
			return ErrInvalid
		}
		secret.Data[DocumentKey] = raw
		_, err = publisher.client.CoreV1().Secrets(publisher.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
			return Receipt{}, err
		}
		return Receipt{}, ErrUnavailable
	}
	return publisher.Check(ctx, configuration, credentialDigests)
}

// Check checks the served configuration revision/digest, Secret identity/version and enabled
// mailbox credential digests before returning a metadata-only receipt.
// Check проверяет обслуживаемые configuration revision и digest, Secret identity и version и
// credential digests включённых mailboxes до metadata-only receipt.
func (publisher *Kubernetes) Check(ctx context.Context, configuration api.Configuration, credentialDigests map[string]string) (Receipt, error) {
	if api.ValidateConfiguration(configuration) != nil {
		return Receipt{}, ErrInvalid
	}
	secret, err := publisher.client.CoreV1().Secrets(publisher.namespace).Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil {
		return Receipt{}, ErrUnavailable
	}
	var served api.Configuration
	if api.Decode(secret.Data[DocumentKey], &served) != nil || api.ValidateConfiguration(served) != nil ||
		served.Revision != configuration.Revision || api.Digest(served) != api.Digest(configuration) ||
		secret.UID == "" || secret.ResourceVersion == "" {
		return Receipt{}, ErrConflict
	}
	if err := validateCredentials(served, secret.Data, credentialDigests); err != nil {
		return Receipt{}, err
	}
	return Receipt{Revision: served.Revision, Digest: api.Digest(served), SecretUID: string(secret.UID), ResourceVersion: secret.ResourceVersion}, nil
}

// validateCredentials requires nonempty material at each enabled endpoint CA/username/secret
// descriptor and matches its SHA-256 to the supplied digest map without changing values.
// validateCredentials требует непустой material каждого CA, username и secret descriptor
// включённого endpoint и сверяет SHA-256 с переданной digest map без изменения значений.
func validateCredentials(configuration api.Configuration, data map[string][]byte, credentialDigests map[string]string) error {
	for _, mailbox := range configuration.Mailboxes {
		if !mailbox.Enabled {
			continue
		}
		for _, endpoint := range []*api.Endpoint{&mailbox.Smtp, mailbox.Imap, mailbox.Pop} {
			if endpoint == nil {
				continue
			}
			for _, descriptor := range []api.Descriptor{endpoint.Ca, endpoint.Username, endpoint.Secret} {
				key, err := CredentialKey(descriptor)
				if err != nil || len(data[key]) == 0 {
					return ErrUnavailable
				}
				digest := sha256.Sum256(data[key])
				if credentialDigests[key] != hex.EncodeToString(digest[:]) {
					return ErrConflict
				}
			}
		}
	}
	return nil
}
