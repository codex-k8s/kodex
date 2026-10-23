// Package kubernetessecret adapts exact precreated Secret clients to the
// authority's versioned material delivery port without resource discovery.
// Пакет kubernetessecret адаптирует точные заранее созданные Secret clients
// к порту версионированной доставки authority без обнаружения ресурсов.
package kubernetessecret

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/codex-k8s/kodex/libs/go/exactkubernetessecret"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/secretschema"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

var (
	// keySetKeys is the closed application-key list for current/next private-key material, generations and source provenance. The exact Secret client owns the separate generation marker and validation.
	// keySetKeys — закрытый список прикладных ключей текущего и следующего закрытого материала, поколений и происхождения источника. Отдельным маркером поколения и проверкой владеет exact Secret client.
	keySetKeys = []string{
		"private.jwk", "current_private_jwk", "next_private_jwk",
		"current_generation", "next_generation", "source_revision",
		"source_digest_sha256", "previous_public_jwk", "previous_generation",
	}
	// manifestTrustKeys restricts manifest-trust delivery to the signed bundle and its source revision/digest; listing these fields neither verifies the signature nor authorizes a resource.
	// manifestTrustKeys ограничивает доставку доверия manifest подписанным bundle и ревизией и дайджестом источника; перечень полей не проверяет подпись и не авторизует ресурс.
	manifestTrustKeys = []string{"bundle.jws", "source_revision", "source_digest_sha256"}
	// proofTrustKeys restricts proof-trust delivery to JWKS and exact source provenance, distinct from private-key delivery; actual key parsing and trust activation belong to consumers.
	// proofTrustKeys ограничивает доставку доверия proof набором JWKS и точным происхождением источника отдельно от доставки закрытых ключей; разбор ключей и активация доверия принадлежат потребителям.
	proofTrustKeys = []string{"jwks.json", "source_revision", "source_digest_sha256"}
	// readbackCredentialKeys holds this adapter's application allowlist for the pinned readback credential and intent/digest/expiry bindings; the material's logical generation is managed separately.
	// readbackCredentialKeys хранит прикладной список полей адаптера для закреплённого readback credential и привязок намерения, дайджестов и срока; логическое поколение материала управляется отдельно.
	readbackCredentialKeys = secretschema.ReadbackCredentialKeys()
	// readbackPossessionKeys is the application's possession-material allowlist: private JWK, key ID, generation and thumbprint. Possession proof and secret access are not granted by the list.
	// readbackPossessionKeys — прикладной список полей материала владения: закрытый JWK, ID ключа, поколение и отпечаток. Перечень не выдаёт доказательство владения или доступ к секрету.
	readbackPossessionKeys = secretschema.ReadbackPossessionKeys()
	// restoreCredentialKeys binds delivered restore-role material to semantic/directive identity, compact credential, delivery receipt and issue time; consumers still verify the signed material.
	// restoreCredentialKeys связывает доставленный restore-role material с семантической идентичностью и директивой, compact credential, квитанцией доставки и временем выдачи; подписанный материал всё равно проверяют потребители.
	restoreCredentialKeys = secretschema.RestoreCredentialKeys()
	// restoreACKKeys defines the restore-ACK private material and its directive, role-credential, receipt and time bindings, separately from the role-credential allowlist.
	// restoreACKKeys задаёт закрытый материал restore ACK и его привязки директивы, role credential, квитанции и времени отдельно от списка полей role credential.
	restoreACKKeys = secretschema.RestoreACKKeys()
)

// Delivery owns one exact Secret map client per registered resource and exposes logical generation-based reads/CAS. It provides no runtime arbitrary resource discovery or Kubernetes create/delete operation.
// Delivery владеет одним exact Secret map client на зарегистрированный ресурс и предоставляет чтение и CAS по логическому поколению. Произвольное обнаружение ресурсов в runtime и Kubernetes create или delete не предоставляются.
type Delivery struct {
	clients map[string]*exactkubernetessecret.MapClient
}

// NewPublisherDelivery derives up to eight purpose bindings per supplied registry target, skips empty optional names and rejects name reuse with different key lists. It does not itself verify the registry signature before constructing exact clients.
// NewPublisherDelivery получает до восьми привязок назначения на цель переданного реестра, пропускает пустые необязательные имена и отклоняет повтор имени с другим списком полей. Подпись реестра сама функция до создания exact clients не проверяет.
func NewPublisherDelivery(registry model.DeliveryTargetRegistry) (*Delivery, error) {
	resources := make(map[string][]string, len(registry.Targets)*8)
	for _, target := range registry.Targets {
		for _, entry := range []struct {
			name string
			keys []string
		}{
			{target.AuthPrivateKeySecret, keySetKeys},
			{target.ManifestTrustSecret, manifestTrustKeys},
			{target.ProofTrustSecret, proofTrustKeys},
			{target.ProofPrivateKeySecret, keySetKeys},
			{target.RestoreCredentialPath, restoreCredentialKeys},
			{target.RestoreACKKeyPath, restoreACKKeys},
			{target.ReadbackCredentialPath, readbackCredentialKeys},
			{target.ReadbackPossessionKeyPath, readbackPossessionKeys},
		} {
			if entry.name == "" {
				continue
			}
			if previous, ok := resources[entry.name]; ok && !equalKeys(previous, entry.keys) {
				return nil, errors.New("Kubernetes Secret purpose is reused with another schema")
			}
			resources[entry.name] = entry.keys
		}
	}
	return New(resources)
}

// NewRuntimeDelivery assembles four core readback/restore resources and two optional resolver resources with fixed purpose schemas before delegating to New. It does not independently reject aliases between the supplied names.
// NewRuntimeDelivery собирает четыре основных ресурса readback и restore и два необязательных resolver с фиксированными схемами назначений перед передачей New. Совпадение переданных имён отдельно не отклоняется.
func NewRuntimeDelivery(
	readbackCredential string,
	readbackPossession string,
	restoreCredential string,
	restoreACK string,
	resolverCredential string,
	resolverPossession string,
) (*Delivery, error) {
	resources := map[string][]string{
		readbackCredential: readbackCredentialKeys,
		readbackPossession: readbackPossessionKeys,
		restoreCredential:  restoreCredentialKeys,
		restoreACK:         restoreACKKeys,
	}
	if resolverCredential != "" {
		resources[resolverCredential] = readbackCredentialKeys
	}
	if resolverPossession != "" {
		resources[resolverPossession] = readbackPossessionKeys
	}
	return New(resources)
}

// New accepts one to 256 registered resources, constructs exact map clients with five-second timeouts and closes already opened clients on a later construction error. Client creation loads its trust boundary; no material read or write is performed by this loop.
// New принимает от одного до 256 зарегистрированных ресурсов, создаёт exact map clients с таймаутом пять секунд и закрывает открытые клиенты при поздней ошибке создания. Создание клиента загружает его границу доверия; материал в этом цикле не читается и не пишется.
func New(resources map[string][]string) (*Delivery, error) {
	if len(resources) == 0 || len(resources) > 256 {
		return nil, errors.New("Kubernetes Secret delivery registry is invalid")
	}
	clients := make(map[string]*exactkubernetessecret.MapClient, len(resources))
	for name, keys := range resources {
		client, err := exactkubernetessecret.NewMap(exactkubernetessecret.MapConfig{
			ResourceName: name, AllowedDataKeys: keys, Timeout: 5 * time.Second,
		})
		if err != nil {
			for _, opened := range clients {
				opened.Close()
			}
			return nil, err
		}
		clients[name] = client
	}
	return &Delivery{clients: clients}, nil
}

// ReadVersioned rejects an unregistered name, reads that client's exact snapshot and treats generation zero as uninitialized absence. A nonzero snapshot becomes application strings and a canonical data digest; its logical version is not Kubernetes resourceVersion.
// ReadVersioned отклоняет незарегистрированное имя, читает точный снимок его клиента и считает нулевое поколение отсутствием инициализации. Ненулевой снимок превращается в прикладные строки и канонический дайджест данных; логическая версия не равна Kubernetes resourceVersion.
func (delivery *Delivery) ReadVersioned(
	ctx context.Context,
	resourceName string,
) (repository.SecretMaterial, bool, error) {
	client, ok := delivery.clients[resourceName]
	if !ok {
		return repository.SecretMaterial{}, false, errors.New("Kubernetes Secret is outside the delivery registry")
	}
	snapshot, err := client.Read(ctx)
	if err != nil {
		return repository.SecretMaterial{}, false, err
	}
	if snapshot.Generation == 0 {
		return repository.SecretMaterial{}, false, nil
	}
	return material(snapshot)
}

// CreateVersioned initializes a preexisting empty Secret using its observed resourceVersion and generation zero CAS. Already initialized material is reread and returned without comparing the supplied data; this is not creation of a Kubernetes resource.
// CreateVersioned инициализирует существующий пустой Secret через наблюдённый resourceVersion и CAS нулевого поколения. Уже инициализированный материал перечитывается и возвращается без сравнения переданных данных; ресурс Kubernetes не создаётся.
func (delivery *Delivery) CreateVersioned(
	ctx context.Context,
	resourceName string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	client, ok := delivery.clients[resourceName]
	if !ok {
		return repository.SecretMaterial{}, errors.New("Kubernetes Secret is outside the delivery registry")
	}
	current, err := client.Read(ctx)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	if current.Generation > 0 {
		result, _, readErr := delivery.ReadVersioned(ctx, resourceName)
		return result, readErr
	}
	updated, err := client.CompareAndSwap(ctx, current.ResourceVersion, 0, encode(data))
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	result, _, err := material(updated)
	return result, err
}

// WriteVersionedCAS requires a registered resource and nonzero expected logical version, reads its current resourceVersion and passes both pins to exact CAS. It returns the served canonical material; a failed CAS is not silently retried.
// WriteVersionedCAS требует зарегистрированный ресурс и ненулевую ожидаемую логическую версию, читает текущий resourceVersion и передаёт обе привязки exact CAS. Возвращается обслуженный канонический материал; отказ CAS скрыто не повторяется.
func (delivery *Delivery) WriteVersionedCAS(
	ctx context.Context,
	resourceName string,
	expectedVersion uint64,
	data map[string]string,
) (repository.SecretMaterial, error) {
	client, ok := delivery.clients[resourceName]
	if !ok || expectedVersion == 0 {
		return repository.SecretMaterial{}, errors.New("Kubernetes Secret CAS target is invalid")
	}
	current, err := client.Read(ctx)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	updated, err := client.CompareAndSwap(
		ctx,
		current.ResourceVersion,
		expectedVersion,
		encode(data),
	)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	result, _, err := material(updated)
	return result, err
}

// Close releases idle connections on every owned exact client. It does not cancel in-flight operations or wait for workers; callers must stop their work before disposing of delivery.
// Close освобождает простаивающие соединения всех своих exact clients. Текущие операции не отменяются и workers не ожидаются; вызывающий останавливает работу до освобождения доставки.
func (delivery *Delivery) Close() {
	for _, client := range delivery.clients {
		client.Close()
	}
}

// material copies application byte values into a string map and computes its canonical JSON SHA-256 while preserving the snapshot generation as logical version. It neither authenticates values nor explicitly erases the resulting strings.
// material копирует прикладные байты в карту строк и вычисляет канонический JSON SHA-256, сохраняя поколение снимка как логическую версию. Значения не аутентифицируются и полученные строки явно не стираются.
func material(snapshot exactkubernetessecret.MapSnapshot) (repository.SecretMaterial, bool, error) {
	data := make(map[string]string, len(snapshot.Data))
	for key, value := range snapshot.Data {
		data[key] = string(value)
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(data)
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("digest Kubernetes Secret readback")
	}
	return repository.SecretMaterial{Version: snapshot.Generation, Data: data, Digest: digest}, true, nil
}

// encode allocates a new byte slice per string value and a new result map for the exact Secret client, leaving the input map unchanged; it does not zero returned secret bytes.
// encode выделяет новый байтовый срез на строковое значение и новую результирующую карту для exact Secret client, не меняя входную карту; возвращённые секретные байты не обнуляются.
func encode(data map[string]string) map[string][]byte {
	result := make(map[string][]byte, len(data))
	for key, value := range data {
		result[key] = []byte(value)
	}
	return result
}

// equalKeys sorts copies of both lists and compares lengths and entries, preserving the callers' order and backing arrays. It compares multisets, so duplicate-key multiplicity is significant.
// equalKeys сортирует копии обоих списков и сравнивает длины и элементы, сохраняя порядок и массивы вызывающих. Сравниваются мультимножества, поэтому количество одинаковых ключей значимо.
func equalKeys(left, right []string) bool {
	leftCopy, rightCopy := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	if len(leftCopy) != len(rightCopy) {
		return false
	}
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

var _ repository.SecretDelivery = (*Delivery)(nil)
