package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// zeroSHA256 is the all-zero predecessor digest for the first publication only; it is not a successful
// digest of arbitrary missing content.
// zeroSHA256 — нулевой хеш предшественника только первой публикации; это не успешный хеш произвольного
// отсутствующего содержимого.
const zeroSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

// PublisherSnapshotValidity sets the 180-day publication/key window used for planned forward-only
// rotation; explicit previous-key retirement deadlines override it where supplied.
// PublisherSnapshotValidity задаёт окно публикации и ключей в 180 дней для плановой последовательной
// ротации; явно переданные сроки вывода предыдущего ключа заменяют его.
const PublisherSnapshotValidity = 180 * 24 * time.Hour

// PublisherKey supplies an issuer/workload-bound key with lifecycle generation, purpose and audiences to
// publication; the key value may contain private material although emitted documents use public-only
// JWKs.
// PublisherKey передаёт публикации ключ с привязкой к подписанту и нагрузке, поколением, назначением и
// аудиториями; значение может содержать закрытый материал, хотя документы получают лишь открытые JWK.
type PublisherKey struct {
	Issuer     string
	WorkloadID string
	Status     string
	Generation uint64
	Purpose    string
	Audiences  []string
	Key        internalrpcauth.ES256Key
}

// PublisherBuildOptions carries independently rooted signer inputs, exact policy/source/history and
// authorization/proof keys plus an optional previous-key deadline; construction does not publish or
// persist them.
// PublisherBuildOptions содержит независимо укоренённые входы подписанта, точные политику, источник и
// историю, ключи контекстов и доказательств и необязательный срок предыдущего ключа; само создание не
// публикует и не сохраняет их.
type PublisherBuildOptions struct {
	ManifestSigner             internalrpcauth.ES256Key
	ManifestSignerGeneration   uint64
	ManifestRootPublicJWKFile  string
	ManifestRootMetadataFile   string
	ManifestTrustBundleJWSFile string
	PolicyFile                 string
	SourceRevision             uint64
	KeySetRevision             uint64
	History                    []model.RevisionDigest
	AuthorizationKeys          []PublisherKey
	AuthorityProofKeys         []PublisherKey
	SourceRegistryDigestSHA256 string
	Now                        time.Time
	PreviousKeyNotAfter        *time.Time
}

// PublisherBuildResult returns the full signed snapshot, its verified canonical payload/digest,
// proof-trust bytes and policy revision; delivery and durable acceptance are separate operations.
// PublisherBuildResult возвращает полный подписанный снимок, проверенное каноническое содержимое и хеш,
// байты доверия доказательствам и ревизию политики; доставка и устойчивое принятие выполняются отдельно.
type PublisherBuildResult struct {
	SnapshotCompactJWS string
	SnapshotPayload    []byte
	SourceDigestSHA256 string
	ProofTrustJSON     []byte
	PolicyRevision     uint64
}

// VerifyPublisherSnapshotCompact verifies exact type/key and canonical signed payload with the
// full-snapshot byte budget, returning a copy; it does not validate domain lifetime/history or
// persistent eligibility.
// VerifyPublisherSnapshotCompact проверяет точные тип и ключ и каноническое подписанное содержимое с
// бюджетом полного снимка, возвращая копию; доменные срок, история и устойчивый допуск здесь не
// проверяются.
func VerifyPublisherSnapshotCompact(
	compact string,
	key internalrpcauth.ES256Key,
) ([]byte, error) {
	verified, err := internalrpcauth.VerifyCanonicalJSONWithLimit(
		compact,
		key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: snapshotProtectedType, KeyID: key.KeyID,
		},
		maxSnapshotBytes,
	)
	if err != nil {
		return nil, errors.New("verify complete authority snapshot")
	}
	return append([]byte(nil), verified.CanonicalPayload...), nil
}

// publisherPolicyDocument is the versioned input policy envelope read by the builder; strict decoding is
// followed by explicit publisher-profile checks.
// publisherPolicyDocument — версионированная входная оболочка политики сборщика; за строгим
// декодированием следуют явные проверки профиля публикации.
type publisherPolicyDocument struct {
	Version        int    `json:"v"`
	PolicyRevision uint64 `json:"policy_revision"`
	Policy         policy `json:"policy"`
}

// BuildForPublisher verifies the rooted manifest signer, reads policy bytes and checks their length
// before decoding, binds public key audiences/history, then signs and reads back the snapshot and builds
// source-pinned proof trust without delivery or storage writes.
// BuildForPublisher проверяет укоренённого подписанта, читает байты политики и проверяет их длину до
// декодирования, связывает аудитории и историю открытых ключей, затем подписывает и перепроверяет снимок
// и строит доверие доказательствам с привязкой к источнику без доставки и записи в хранилище.
func BuildForPublisher(options PublisherBuildOptions) (PublisherBuildResult, error) {
	if options.ManifestSigner.Private == nil ||
		options.ManifestSignerGeneration == 0 ||
		options.SourceRevision == 0 ||
		options.KeySetRevision == 0 ||
		!snapshotDigestPattern.MatchString(options.SourceRegistryDigestSHA256) {
		return PublisherBuildResult{}, errors.New("authority snapshot publisher inputs are invalid")
	}
	now := options.Now.UTC().Truncate(time.Second)
	if now.IsZero() {
		now = time.Now().UTC().Truncate(time.Second)
	}
	if err := VerifyPublisherManifestSigner(
		options.ManifestSigner,
		options.ManifestSignerGeneration,
		options.ManifestRootPublicJWKFile,
		options.ManifestRootMetadataFile,
		options.ManifestTrustBundleJWSFile,
		now,
	); err != nil {
		return PublisherBuildResult{}, err
	}
	policyRaw, err := os.ReadFile(options.PolicyFile)
	if err != nil || len(policyRaw) == 0 || len(policyRaw) > maxSnapshotBytes {
		return PublisherBuildResult{}, errors.New("read authority publisher policy")
	}
	policyRaw = trimSingleTrailingNewline(policyRaw)
	var policyDocument publisherPolicyDocument
	if err := internalrpcauth.DecodeStrictJSON(
		policyRaw,
		&policyDocument,
	); err != nil ||
		policyDocument.Version != model.ContractVersion ||
		policyDocument.PolicyRevision == 0 ||
		policyDocument.Policy.AuthorityABIVersion != model.AuthorityABIVersion ||
		policyDocument.Policy.TrustDomain != "kodex.local" ||
		policyDocument.Policy.DefaultDecision != "DENY" ||
		len(policyDocument.Policy.OperationBindings) == 0 ||
		len(policyDocument.Policy.ProofProducers) == 0 {
		return PublisherBuildResult{}, errors.New("authority publisher policy rejected")
	}
	authorizationKeys, proofKeys, err := bindPublisherKeyAudiences(
		options.AuthorizationKeys,
		options.AuthorityProofKeys,
		policyDocument.Policy,
	)
	if err != nil {
		return PublisherBuildResult{}, err
	}
	issuers, err := publisherIssuerSets(
		authorizationKeys,
		now,
		options.PreviousKeyNotAfter,
	)
	if err != nil {
		return PublisherBuildResult{}, err
	}
	predecessor, history, err := publisherHistory(
		options.SourceRevision,
		options.History,
	)
	if err != nil {
		return PublisherBuildResult{}, err
	}
	value := document{
		Version:          model.ContractVersion,
		SourceRevision:   options.SourceRevision,
		KeySetRevision:   options.KeySetRevision,
		PolicyRevision:   policyDocument.PolicyRevision,
		SignerGeneration: options.ManifestSignerGeneration,
		PublishedAt:      now.Unix(),
		ValidFrom:        now.Add(-5 * time.Second).Unix(),
		ValidUntil:       now.Add(PublisherSnapshotValidity).Unix(),
		Predecessor:      predecessor,
		History:          history,
		Issuers:          issuers,
		Policy:           policyDocument.Policy,
	}
	compact, err := internalrpcauth.SignCanonicalJSONWithLimit(
		value,
		options.ManifestSigner,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: snapshotProtectedType, KeyID: options.ManifestSigner.KeyID,
		},
		maxSnapshotBytes,
	)
	if err != nil {
		return PublisherBuildResult{}, errors.New("sign complete authority snapshot")
	}
	verifiedPayload, err := VerifyPublisherSnapshotCompact(
		compact,
		options.ManifestSigner.PublicOnly(),
	)
	if err != nil {
		return PublisherBuildResult{}, errors.New("read back signed authority snapshot")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(value)
	if err != nil {
		return PublisherBuildResult{}, errors.New("digest signed authority snapshot")
	}
	expectedPayload, err := internalrpcauth.CanonicalJSON(value)
	if err != nil {
		return PublisherBuildResult{}, errors.New(
			"encode signed authority snapshot readback",
		)
	}
	if !bytes.Equal(verifiedPayload, expectedPayload) {
		return PublisherBuildResult{}, errors.New("signed authority snapshot readback mismatch")
	}
	proofOptions := options
	proofOptions.AuthorityProofKeys = proofKeys
	proofTrust, err := publisherProofTrust(
		proofOptions,
		now,
		digest,
	)
	if err != nil {
		return PublisherBuildResult{}, err
	}
	return PublisherBuildResult{
		SnapshotCompactJWS: compact,
		SnapshotPayload:    verifiedPayload,
		SourceDigestSHA256: digest,
		ProofTrustJSON:     proofTrust,
		PolicyRevision:     policyDocument.PolicyRevision,
	}, nil
}

// bindPublisherKeyAudiences derives exact sorted audiences separately from operation callers and proof
// producers, rejects delegated authorization issuers, and returns new key slices without overwriting
// input audience slices.
// bindPublisherKeyAudiences раздельно выводит точные отсортированные аудитории из вызывающих операции и
// производителей доказательств, отклоняет делегированных подписантов контекстов и возвращает новые срезы
// ключей без перезаписи исходных аудиторий.
func bindPublisherKeyAudiences(
	authorizationKeys []PublisherKey,
	proofKeys []PublisherKey,
	policyValue policy,
) ([]PublisherKey, []PublisherKey, error) {
	authorizationAudiences := make(map[string]map[string]struct{})
	for _, binding := range policyValue.OperationBindings {
		if binding.CallerSPIFFEID == "" ||
			binding.Issuer != binding.CallerSPIFFEID ||
			binding.Audience == "" {
			return nil, nil, errors.New("authority operation binding issuer rejected")
		}
		if authorizationAudiences[binding.CallerSPIFFEID] == nil {
			authorizationAudiences[binding.CallerSPIFFEID] = make(map[string]struct{})
		}
		authorizationAudiences[binding.CallerSPIFFEID][binding.Audience] = struct{}{}
	}
	proofAudiences := make(map[string]map[string]struct{})
	for _, producer := range policyValue.ProofProducers {
		if producer.AuthorityProofIssuer == "" || producer.AuthorityProofAudience == "" {
			return nil, nil, errors.New("authority proof producer identity rejected")
		}
		if proofAudiences[producer.AuthorityProofIssuer] == nil {
			proofAudiences[producer.AuthorityProofIssuer] = make(map[string]struct{})
		}
		proofAudiences[producer.AuthorityProofIssuer][producer.AuthorityProofAudience] = struct{}{}
	}
	bind := func(values []PublisherKey, expected map[string]map[string]struct{}) ([]PublisherKey, error) {
		result := make([]PublisherKey, len(values))
		for index, value := range values {
			audiences := expected[value.Issuer]
			if len(audiences) == 0 {
				return nil, errors.New("publisher key has no policy audience")
			}
			value.Audiences = make([]string, 0, len(audiences))
			for audience := range audiences {
				value.Audiences = append(value.Audiences, audience)
			}
			sort.Strings(value.Audiences)
			result[index] = value
		}
		return result, nil
	}
	boundAuthorization, err := bind(authorizationKeys, authorizationAudiences)
	if err != nil {
		return nil, nil, err
	}
	boundProof, err := bind(proofKeys, proofAudiences)
	if err != nil {
		return nil, nil, err
	}
	return boundAuthorization, boundProof, nil
}

// VerifyPublisherManifestSigner signs a local probe and reads pinned root/bundle files to require the
// exact rooted CURRENT public key and generation; it performs file I/O but no external publication.
// VerifyPublisherManifestSigner подписывает локальный пробный документ и читает закреплённые корневые
// файлы и набор доверия, требуя точные открытый CURRENT и поколение; файловый ввод есть, внешней
// публикации нет.
func VerifyPublisherManifestSigner(
	signer internalrpcauth.ES256Key,
	signerGeneration uint64,
	rootPublicJWKFile string,
	rootMetadataFile string,
	trustBundleJWSFile string,
	now time.Time,
) error {
	if signer.Private == nil ||
		signerGeneration == 0 ||
		rootPublicJWKFile == "" ||
		rootMetadataFile == "" ||
		trustBundleJWSFile == "" {
		return errors.New("authority manifest signer preflight is invalid")
	}
	dummy, err := internalrpcauth.SignCanonicalJSON(
		struct {
			Version int `json:"v"`
		}{Version: model.ContractVersion},
		signer,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: snapshotProtectedType, KeyID: signer.KeyID,
		},
	)
	if err != nil {
		return errors.New("sign manifest signer verification probe")
	}
	selected, generation, err := loadManifestVerificationKeyForType(
		LoadOptions{
			ManifestRootPublicJWKFile:  rootPublicJWKFile,
			ManifestRootMetadataFile:   rootMetadataFile,
			ManifestTrustBundleJWSFile: trustBundleJWSFile,
		},
		dummy,
		now,
		snapshotProtectedType,
		internalrpcauth.MaxCompactJWSBytes,
	)
	if err != nil ||
		generation != signerGeneration ||
		!samePublicKey(selected, signer) {
		return errors.New(
			"manifest signer does not match independently rooted CURRENT key",
		)
	}
	return nil
}

// publisherIssuerSets validates authorization-key inputs, emits public-only JWK records with
// status-dependent intervals and copied audiences, and sorts issuer/key output independently of registry
// iteration order.
// publisherIssuerSets проверяет входные ключи контекстов, выдаёт открытые JWK со сроками по статусу и
// копиями аудиторий и сортирует подписантов и ключи независимо от порядка реестра.
func publisherIssuerSets(
	values []PublisherKey,
	now time.Time,
	previousKeyNotAfter *time.Time,
) ([]issuerKeySet, error) {
	grouped := make(map[string]*issuerKeySet)
	order := make([]string, 0)
	for _, value := range values {
		if value.Purpose != "AUTHORIZATION_CONTEXT" ||
			value.Key.Public == nil ||
			value.Issuer == "" ||
			value.WorkloadID == "" ||
			value.Generation == 0 ||
			len(value.Audiences) == 0 {
			return nil, errors.New("authorization key publication record rejected")
		}
		publicJWK, err := internalrpcauth.MarshalPublicJWK(value.Key.PublicOnly())
		if err != nil {
			return nil, errors.New("encode authorization public key")
		}
		if grouped[value.Issuer] == nil {
			grouped[value.Issuer] = &issuerKeySet{
				Issuer: value.Issuer, WorkloadID: value.WorkloadID,
			}
			order = append(order, value.Issuer)
		}
		if grouped[value.Issuer].WorkloadID != value.WorkloadID {
			return nil, errors.New("authorization issuer workload mutation rejected")
		}
		notAfter, err := publisherKeyNotAfter(
			value.Status,
			now,
			previousKeyNotAfter,
		)
		if err != nil {
			return nil, err
		}
		notBefore := publisherKeyNotBefore(now, notAfter)
		grouped[value.Issuer].Keys = append(grouped[value.Issuer].Keys, keyEntry{
			Status: value.Status, Generation: value.Generation,
			Purpose:   value.Purpose,
			Audiences: append([]string(nil), value.Audiences...),
			NotBefore: notBefore.Unix(),
			NotAfter:  notAfter.Unix(),
			JWK:       json.RawMessage(publicJWK),
		})
	}
	result := make([]issuerKeySet, 0, len(order))
	for _, issuer := range order {
		sort.Slice(grouped[issuer].Keys, func(left, right int) bool {
			if grouped[issuer].Keys[left].Generation != grouped[issuer].Keys[right].Generation {
				return grouped[issuer].Keys[left].Generation < grouped[issuer].Keys[right].Generation
			}
			return grouped[issuer].Keys[left].Status < grouped[issuer].Keys[right].Status
		})
		result = append(result, *grouped[issuer])
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Issuer < result[right].Issuer
	})
	return result, nil
}

// publisherProofTrust builds a canonically encoded public proof-key trust document tied to exact
// snapshot digest/history, orders keys deterministically and decodes the result through the shared
// schema before returning.
// publisherProofTrust строит канонический документ открытых ключей доказательств с точными хешем и
// историей снимка, воспроизводимо сортирует ключи и перед возвратом декодирует результат общей схемой.
func publisherProofTrust(
	options PublisherBuildOptions,
	now time.Time,
	sourceDigest string,
) ([]byte, error) {
	if !snapshotDigestPattern.MatchString(sourceDigest) {
		return nil, errors.New("authority proof trust source digest is invalid")
	}
	_, history, err := publisherHistory(options.SourceRevision, options.History)
	if err != nil {
		return nil, err
	}
	predecessor, _, _ := publisherHistory(options.SourceRevision, options.History)
	documentValue := proofTrustDocument{
		Version:        model.ContractVersion,
		Purpose:        "AUTHORITY_PROOF_VERIFICATION",
		SourceRevision: options.SourceRevision,
		SourceDigest:   sourceDigest,
		Predecessor:    predecessor,
		History:        history,
	}
	for _, value := range options.AuthorityProofKeys {
		if value.Purpose != "AUTHORITY_PROOF" ||
			value.Key.Public == nil ||
			value.Issuer == "" ||
			value.Generation == 0 ||
			len(value.Audiences) == 0 {
			return nil, errors.New("authority proof key publication record rejected")
		}
		publicJWK, err := internalrpcauth.MarshalPublicJWK(value.Key.PublicOnly())
		if err != nil {
			return nil, errors.New("encode authority proof public key")
		}
		notAfter, err := publisherKeyNotAfter(
			value.Status,
			now,
			options.PreviousKeyNotAfter,
		)
		if err != nil {
			return nil, err
		}
		notBefore := publisherKeyNotBefore(now, notAfter)
		documentValue.Keys = append(documentValue.Keys, proofTrustKey{
			Issuer:     value.Issuer,
			Generation: value.Generation,
			Status:     value.Status,
			Purpose:    value.Purpose,
			Audiences:  append([]string(nil), value.Audiences...),
			NotBefore:  notBefore.Unix(),
			NotAfter:   notAfter.Unix(),
			JWK:        json.RawMessage(publicJWK),
		})
	}
	if len(documentValue.Keys) == 0 {
		return nil, errors.New("authority proof trust has no key")
	}
	sort.Slice(documentValue.Keys, func(left, right int) bool {
		if documentValue.Keys[left].Issuer != documentValue.Keys[right].Issuer {
			return documentValue.Keys[left].Issuer < documentValue.Keys[right].Issuer
		}
		if documentValue.Keys[left].Generation != documentValue.Keys[right].Generation {
			return documentValue.Keys[left].Generation < documentValue.Keys[right].Generation
		}
		return documentValue.Keys[left].Status < documentValue.Keys[right].Status
	})
	canonical, err := internalrpcauth.CanonicalJSON(documentValue)
	if err != nil {
		return nil, errors.New("encode authority proof trust")
	}
	if _, err := internalrpcauth.DecodeAuthorityProofTrustDocument(canonical); err != nil {
		return nil, errors.New("read back authority proof trust")
	}
	return canonical, nil
}

// publisherKeyNotAfter uses the explicit UTC deadline for PREVIOUS keys when nonzero, otherwise the
// existing publication window; non-PREVIOUS statuses also receive that window and are validated by their
// owning paths.
// publisherKeyNotAfter использует явный ненулевой срок UTC для PREVIOUS, иначе существующее окно
// публикации; остальные статусы также получают это окно и проверяются владеющими путями.
func publisherKeyNotAfter(
	status string,
	now time.Time,
	previousKeyNotAfter *time.Time,
) (time.Time, error) {
	if status != "PREVIOUS" {
		return now.Add(PublisherSnapshotValidity), nil
	}
	if previousKeyNotAfter == nil || previousKeyNotAfter.IsZero() {
		// Preserve the existing protocol_version=1 missing-deadline fallback;
		// the composed rotation path supplies a PostgreSQL-assigned deadline.
		// Сохраняется существующий запасной путь protocol_version=1 без срока;
		// составной путь ротации передаёт срок, назначенный PostgreSQL.
		return now.Add(PublisherSnapshotValidity), nil
	}
	return previousKeyNotAfter.UTC(), nil
}

// publisherKeyNotBefore normally starts one minute before publication, but moves before notAfter for
// already-retired previous keys so their serialized interval remains ordered.
// publisherKeyNotBefore обычно начинает срок за минуту до публикации, но для уже выведенного предыдущего
// ключа сдвигает начало до notAfter, сохраняя порядок сериализованного интервала.
func publisherKeyNotBefore(now, notAfter time.Time) time.Time {
	notBefore := now.Add(-time.Minute)
	if !notBefore.Before(notAfter) {
		return notAfter.Add(-time.Minute)
	}
	return notBefore
}

// publisherHistory returns a zero bootstrap predecessor or copies and validates a bounded predecessor
// history, using its last exact revision/digest as the next publication's predecessor.
// publisherHistory возвращает нулевого начального предшественника либо копирует и проверяет ограниченную
// историю, используя последнюю точную ревизию и хеш как предшественника новой публикации.
func publisherHistory(
	sourceRevision uint64,
	values []model.RevisionDigest,
) (revisionDigest, []revisionDigest, error) {
	if sourceRevision == 1 {
		if len(values) != 0 {
			return revisionDigest{}, nil, errors.New("bootstrap snapshot history is not empty")
		}
		return revisionDigest{Revision: 0, DigestSHA256: zeroSHA256}, nil, nil
	}
	if len(values) == 0 || len(values) > 32 {
		return revisionDigest{}, nil, errors.New("snapshot predecessor history is unavailable")
	}
	history := make([]revisionDigest, 0, len(values))
	for _, value := range values {
		history = append(history, revisionDigest{
			Revision: value.Revision, DigestSHA256: value.DigestSHA256,
		})
	}
	predecessor := history[len(history)-1]
	if err := validateHistory(sourceRevision, predecessor, history); err != nil {
		return revisionDigest{}, nil, err
	}
	return predecessor, history, nil
}
