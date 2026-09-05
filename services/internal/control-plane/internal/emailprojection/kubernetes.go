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
	SecretName             = "email-bridge-mailbox-projection"
	DocumentKey            = "mailboxes.json"
	maximumProjectionBytes = 900 << 10
)

var (
	ErrInvalid     = errors.New("email projection is invalid")
	ErrConflict    = errors.New("email projection revision conflict")
	ErrUnavailable = errors.New("email projection is unavailable")
)

type Receipt struct {
	Revision                           int64
	Digest, SecretUID, ResourceVersion string
}

type Kubernetes struct {
	client    kubernetes.Interface
	namespace string
}

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

func New(client kubernetes.Interface, namespace string) (*Kubernetes, error) {
	if client == nil || !api.DescriptorValid(api.Descriptor{Name: namespace, Generation: 1}) {
		return nil, ErrInvalid
	}
	return &Kubernetes{client: client, namespace: namespace}, nil
}

// CredentialKey не допускает alias между разными immutable descriptors.
func CredentialKey(descriptor api.Descriptor) (string, error) {
	if !api.DescriptorValid(descriptor) {
		return "", ErrInvalid
	}
	return descriptor.Name + "." + strconv.FormatInt(descriptor.Generation, 10), nil
}

// Publish вызывается только после принятия revision в PostgreSQL владельца.
// Credential values уже материализованы отдельно; публикация не меняет их.
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
