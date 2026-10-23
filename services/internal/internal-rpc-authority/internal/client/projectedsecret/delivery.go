// Package projectedsecret reads prebound workload Secret mounts without a
// Kubernetes API client; its delivery write methods always refuse mutation.
// Пакет projectedsecret читает заданные mount workload Secret без клиента
// Kubernetes API; методы записи доставки всегда отклоняют изменение.
package projectedsecret

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/secretschema"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
)

// generationKey names the provider-managed logical generation file, checked before and after application fields; it is not part of an application schema or a Kubernetes resourceVersion.
// generationKey задаёт имя управляемого провайдером файла логического поколения, проверяемого до и после прикладных полей; это не часть прикладной схемы и не Kubernetes resourceVersion.
const generationKey = "_generation"

// binding connects one registered logical resource to a fixed absolute kubelet mount directory and an ordered application key list, without carrying a Kubernetes API client or write capability.
// binding связывает зарегистрированный логический ресурс с фиксированным абсолютным каталогом kubelet mount и упорядоченным списком прикладных полей без Kubernetes API client и права записи.
type binding struct {
	directory string
	keys      []string
}

// Delivery reads only its prebound projected material and implements write methods solely as closed refusals. It owns no network connection or polling worker.
// Delivery читает только заранее связанный projected material и реализует методы записи исключительно отказом. Сетевым соединением и worker опроса не владеет.
type Delivery struct {
	bindings map[string]binding
}

// NewRuntimeDelivery binds four core and two optional resolver names to fixed mount directories and purpose key lists, rejecting empty names, nonabsolute directories or empty lists. It performs no filesystem reads and has no separate name-alias check.
// NewRuntimeDelivery связывает четыре основных и два необязательных resolver-имени с фиксированными каталогами mount и списками назначения, отклоняя пустые имена, не-абсолютные каталоги и пустые списки. Файлы не читаются; отдельной проверки совпадения имён нет.
func NewRuntimeDelivery(
	readbackCredential string,
	readbackPossession string,
	restoreCredential string,
	restoreACK string,
	resolverCredential string,
	resolverPossession string,
) (*Delivery, error) {
	bindings := map[string]binding{
		readbackCredential: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/credential",
			keys:      secretschema.ReadbackCredentialKeys(),
		},
		readbackPossession: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/possession",
			keys:      secretschema.ReadbackPossessionKeys(),
		},
		restoreCredential: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/restore/credential",
			keys:      secretschema.RestoreCredentialKeys(),
		},
		restoreACK: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/restore/ack",
			keys:      secretschema.RestoreACKKeys(),
		},
	}
	if resolverCredential != "" {
		bindings[resolverCredential] = binding{
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/resolver-credential",
			keys:      secretschema.ReadbackCredentialKeys(),
		}
	}
	if resolverPossession != "" {
		bindings[resolverPossession] = binding{
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/resolver-possession",
			keys:      secretschema.ReadbackPossessionKeys(),
		}
	}
	for name, value := range bindings {
		if name == "" || !filepath.IsAbs(value.directory) || len(value.keys) == 0 {
			return nil, errors.New("projected Secret runtime binding is invalid")
		}
	}
	return &Delivery{bindings: bindings}, nil
}

// ReadVersioned ignores its context, reads a positive decimal generation, then all allowed fields and the same generation bytes again before hashing the data. Missing generation means absence; other failures reject the read. It does not pin a directory inode across reads or independently prove kubelet snapshot identity.
// ReadVersioned не использует контекст, читает положительное десятичное поколение, затем все разрешённые поля и повторно те же байты поколения до хеширования данных. Отсутствие поколения означает отсутствие материала; остальные ошибки отклоняют чтение. Inode каталога между чтениями не закрепляется и идентичность снимка kubelet независимо не доказывается.
func (delivery *Delivery) ReadVersioned(
	_ context.Context,
	resourceName string,
) (repository.SecretMaterial, bool, error) {
	binding, ok := delivery.bindings[resourceName]
	if !ok {
		return repository.SecretMaterial{}, false, errors.New("projected Secret is outside the runtime registry")
	}
	generationRaw, err := readBounded(filepath.Join(binding.directory, generationKey), 64)
	if errors.Is(err, os.ErrNotExist) {
		return repository.SecretMaterial{}, false, nil
	}
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("read projected Secret generation")
	}
	generation, err := strconv.ParseUint(string(generationRaw), 10, 64)
	if err != nil || generation == 0 {
		return repository.SecretMaterial{}, false, errors.New("projected Secret generation is invalid")
	}
	data := make(map[string]string, len(binding.keys))
	for _, key := range binding.keys {
		raw, readErr := readBounded(filepath.Join(binding.directory, key), 1<<20)
		if readErr != nil {
			return repository.SecretMaterial{}, false, errors.New("read projected Secret field")
		}
		data[key] = string(raw)
	}
	// Повторное чтение generation закрывает смену kubelet symlink между файлами.
	confirmed, err := readBounded(filepath.Join(binding.directory, generationKey), 64)
	if err != nil || string(confirmed) != string(generationRaw) {
		return repository.SecretMaterial{}, false, errors.New("projected Secret snapshot changed during read")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(data)
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("digest projected Secret readback")
	}
	return repository.SecretMaterial{Version: generation, Data: data, Digest: digest}, true, nil
}

// CreateVersioned always rejects projected-material initialization and performs no filesystem or API write, regardless of the supplied context, resource or data.
// CreateVersioned всегда отклоняет инициализацию projected material без записи в файлы или API независимо от переданных контекста, ресурса и данных.
func (delivery *Delivery) CreateVersioned(context.Context, string, map[string]string) (repository.SecretMaterial, error) {
	return repository.SecretMaterial{}, errors.New("projected Secret delivery is read-only")
}

// WriteVersionedCAS always rejects writes through this read-only delivery boundary; an expected version does not confer a mutation capability.
// WriteVersionedCAS всегда отклоняет запись через эту границу доставки только для чтения; ожидаемая версия не даёт права изменения.
func (delivery *Delivery) WriteVersionedCAS(context.Context, string, uint64, map[string]string) (repository.SecretMaterial, error) {
	return repository.SecretMaterial{}, errors.New("projected Secret delivery is read-only")
}

// Close is intentionally a no-op because projected delivery owns no connections or background workers; it does not unmount or erase kubelet material.
// Close намеренно ничего не делает: projected delivery не владеет соединениями или фоновыми workers; материал kubelet не размонтируется и не стирается.
func (delivery *Delivery) Close() {}

// readBounded follows Stat/ReadFile resolution and checks a regular nonempty file against the limit both before and after reading. It assumes a trusted projected mount; it does not enforce ownership/mode, pin an inode or bound allocation while ReadFile is in progress.
// readBounded следует разрешению Stat и ReadFile и проверяет обычный непустой файл по пределу до и после чтения. Предполагается доверенный projected mount; владелец и режим не проверяются, inode не закрепляется и выделение памяти во время ReadFile не ограничивается.
func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("projected Secret file is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return nil, errors.New("read projected Secret file")
	}
	return raw, nil
}

var _ repository.SecretDelivery = (*Delivery)(nil)
