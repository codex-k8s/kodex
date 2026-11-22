package managedssh

import (
	"net/netip"
	"slices"
	"strings"
)

// QualificationSchema identifies one immutable operator deployment artifact; product callers cannot use this document to grant themselves SSH authority.
// QualificationSchema обозначает immutable operator deployment artifact; product caller не может выдать себе SSH authority этим документом.
const QualificationSchema = "kodex-ssh-qualifications/v2"

// HostKeyAlgorithm identifies the exact raw key type captured by the operator, not an SSH signature algorithm or a request to trust every key of that type.
// HostKeyAlgorithm обозначает exact raw key type, captured оператором, а не SSH signature algorithm либо доверие всем keys данного типа.
type HostKeyAlgorithm string

const (
	// ED25519HostKey pins the modern raw Ed25519 key representation.
	// ED25519HostKey фиксирует modern raw Ed25519 key representation.
	ED25519HostKey HostKeyAlgorithm = "ssh-ed25519"
	// ECDSA256HostKey pins the raw NIST P256 key representation.
	// ECDSA256HostKey фиксирует raw NIST P256 key representation.
	ECDSA256HostKey HostKeyAlgorithm = "ecdsa-sha2-nistp256"
	// ECDSA384HostKey pins the raw NIST P384 key representation.
	// ECDSA384HostKey фиксирует raw NIST P384 key representation.
	ECDSA384HostKey HostKeyAlgorithm = "ecdsa-sha2-nistp384"
	// ECDSA521HostKey pins the raw NIST P521 key representation.
	// ECDSA521HostKey фиксирует raw NIST P521 key representation.
	ECDSA521HostKey HostKeyAlgorithm = "ecdsa-sha2-nistp521"
	// RSAHostKey identifies raw RSA bytes only; the consumer must still enforce RSA3072+ and negotiate SHA2 signatures, never the similarly named SHA1 signature.
	// RSAHostKey обозначает лишь raw RSA bytes; consumer обязан проверять RSA3072+ и согласовывать SHA2 signatures, никогда одноимённую SHA1 signature.
	RSAHostKey HostKeyAlgorithm = "ssh-rsa"
)

// Valid closes the operator key-type contract to modern raw key representations; strength, exact digest and negotiated signature checks remain the consuming SSH transport's responsibility.
// Valid ограничивает operator key-type contract modern raw key representations; strength, exact digest и negotiated signature проверяются consuming SSH transport.
func (algorithm HostKeyAlgorithm) Valid() bool {
	switch algorithm {
	case ED25519HostKey, ECDSA256HostKey, ECDSA384HostKey, ECDSA521HostKey, RSAHostKey:
		return true
	default:
		return false
	}
}

// OperatorQualificationMount is the deployment-owned read-only mount; product connection configuration cannot select another path.
// OperatorQualificationMount задаёт deployment-owned read-only mount; product connection configuration не может выбрать другой path.
const OperatorQualificationMount = "/var/run/kodex/ssh-qualifications"

// OperatorQualificationFile is the only qualification file admitted by the broker transport.
// OperatorQualificationFile задаёт единственный qualification file, допускаемый broker transport.
const OperatorQualificationFile = OperatorQualificationMount + "/profiles.json"

// QualificationFileEnvironment is the fixed deployment setting for the qualified artifact file.
// QualificationFileEnvironment задаёт fixed deployment setting для qualified artifact file.
const QualificationFileEnvironment = "SECRET_BROKER_SSH_QUALIFICATION_FILE"

// QualificationSHAEnvironment is the fixed deployment setting for the complete artifact digest, independent of its shortened Kubernetes object name.
// QualificationSHAEnvironment задаёт fixed deployment setting для полного artifact digest независимо от сокращённого имени Kubernetes object.
const QualificationSHAEnvironment = "SECRET_BROKER_SSH_QUALIFICATION_SHA256"

// TransportMode separates the exclusive managed helper identity from an explicitly selected arbitrary-execute identity; the latter never implies READ classification.
// TransportMode разделяет exclusive managed helper identity и явно выбранную arbitrary-execute identity; последняя никогда не подразумевает классификацию READ.
type TransportMode string

const (
	// ManagedTransport requires a root-controlled forced command and an exclusive helper UID with no other writers to its namespaces or receipts.
	// ManagedTransport требует root-controlled forced command и exclusive helper UID без других writers в namespaces либо receipts.
	ManagedTransport TransportMode = "MANAGED"
	// ExecuteTransport allows only separately owner-authorized exact command execution with EACH consent, irrespective of command text.
	// ExecuteTransport допускает только отдельно owner-authorized exact command execution с EACH consent независимо от текста команды.
	ExecuteTransport TransportMode = "EXECUTE"
)

// Qualifications is the bounded canonical content of the trusted operator mount; removing a profile revokes its future dispatch eligibility.
// Qualifications содержит bounded canonical content доверенного operator mount; удаление profile отзывает eligibility будущего dispatch.
type Qualifications struct {
	Schema   string                `json:"schema"`
	Profiles []TargetQualification `json:"profiles"`
}

// TargetQualification binds operator-observed deployment facts to exact owner target and SSH public keys, without carrying credentials, grants or caller-selectable commands.
// TargetQualification связывает operator-observed deployment facts с exact owner target и SSH public keys без credentials, grants либо caller-selectable commands.
type TargetQualification struct {
	TargetRef         string           `json:"target_ref"`
	Address           string           `json:"address"`
	Port              uint16           `json:"port"`
	User              string           `json:"user"`
	Mode              TransportMode    `json:"mode"`
	HostKeyDigest     string           `json:"host_key_sha256"`
	HostKeyAlgorithm  HostKeyAlgorithm `json:"host_key_algorithm"`
	AuthenticationKey string           `json:"authentication_key_sha256"`
	UID               uint32           `json:"uid"`
	GID               uint32           `json:"gid"`
	Groups            []uint32         `json:"groups"`
	QualifiedAt       int64            `json:"qualified_at_unix"`
	ValidUntil        int64            `json:"valid_until_unix"`
	DeploymentDigest  string           `json:"deployment_sha256"`
	SSHDConfiguration string           `json:"sshd_configuration_sha256"`
	IdentityInventory string           `json:"identity_inventory_sha256"`
	RuntimeReadback   string           `json:"runtime_readback_sha256"`
	IdentityProbe     string           `json:"identity_probe_sha256,omitempty"`
	Profile           *Profile         `json:"profile,omitempty"`
}

// EncodeQualifications checks the complete finite artifact at the operator's capture time; output must still be delivered through the separately trusted deployment path.
// EncodeQualifications проверяет полный finite artifact на время operator capture; output требуется доставить через отдельно доверенный deployment path.
func EncodeQualifications(document Qualifications, now int64) ([]byte, error) {
	if document.Validate(now) != nil {
		return nil, ErrProtocol
	}
	return canonical(document)
}

// DecodeQualifications rejects alternate JSON representations and every expired, ambiguous or unknown profile before any network authentication or command dispatch.
// DecodeQualifications отклоняет alternate JSON representations и каждый expired, ambiguous либо unknown profile до network authentication либо command dispatch.
func DecodeQualifications(raw []byte, now int64) (Qualifications, error) {
	var document Qualifications
	if decodeCanonical(raw, &document) != nil || document.Validate(now) != nil {
		return Qualifications{}, ErrProtocol
	}
	return document, nil
}

// Validate closes the whole artifact on malformed or duplicate entries, including profiles other than the selected target, so partially accepted configuration cannot hide a broken deployment.
// Validate закрывает весь artifact при malformed либо duplicate entries, включая другие targets: частично принятая configuration не скрывает broken deployment.
func (document Qualifications) Validate(now int64) error {
	if document.Schema != QualificationSchema || len(document.Profiles) < 1 || len(document.Profiles) > 32 || now <= 0 {
		return ErrProtocol
	}
	seen := map[string]bool{}
	targets := map[string]TargetQualification{}
	for _, profile := range document.Profiles {
		key := profile.TargetRef + "\x00" + profile.AuthenticationKey
		if profile.Validate(now) != nil || seen[key] {
			return ErrProtocol
		}
		if existing, ok := targets[profile.TargetRef]; ok && !sameQualifiedInstallation(existing, profile) {
			return ErrProtocol
		}
		seen[key], targets[profile.TargetRef] = true, profile
	}
	return nil
}

// sameQualifiedInstallation permits distinct personal public keys only for one identical target deployment; a new key cannot select another endpoint, account, mode, UID, scope or helper revision.
// sameQualifiedInstallation допускает разные personal public keys только для identical target deployment; новый key не выбирает другой endpoint, account, mode, UID, scope либо helper revision.
func sameQualifiedInstallation(first, second TargetQualification) bool {
	if first.Address != second.Address || first.Port != second.Port || first.User != second.User || first.Mode != second.Mode || first.HostKeyDigest != second.HostKeyDigest || first.HostKeyAlgorithm != second.HostKeyAlgorithm || first.UID != second.UID || first.GID != second.GID || !slices.Equal(first.Groups, second.Groups) ||
		first.DeploymentDigest != second.DeploymentDigest || first.SSHDConfiguration != second.SSHDConfiguration || first.IdentityInventory != second.IdentityInventory || first.IdentityProbe != second.IdentityProbe {
		return false
	}
	if first.Profile != nil {
		if second.Profile == nil {
			return false
		}
		left, leftErr := first.Profile.Digest()
		right, rightErr := second.Profile.Digest()
		return leftErr == nil && rightErr == nil && left == right
	}
	return second.Profile == nil
}

// Validate checks exact immutable coordinates, a non-root observed identity and at most 24 hours of operator evidence; it never treats syntax or hashes as proof that the operator actually performed the readback.
// Validate проверяет exact immutable coordinates, non-root observed identity и не более24 часов operator evidence; синтаксис либо hashes не доказывают фактическое выполнение readback оператором.
func (qualification TargetQualification) Validate(now int64) error {
	address, err := netip.ParseAddr(qualification.Address)
	if err != nil || address.String() != qualification.Address || address.Is4In6() || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast() || qualification.Port == 0 || !OwnerReference(qualification.TargetRef) || !qualificationUser(qualification.User) ||
		qualification.UID == 0 || qualification.GID == 0 || len(qualification.Groups) > 32 || qualification.Groups == nil || !qualification.HostKeyAlgorithm.Valid() ||
		qualification.QualifiedAt <= 0 || qualification.QualifiedAt > now+30 || qualification.ValidUntil <= now || qualification.ValidUntil <= qualification.QualifiedAt || qualification.ValidUntil-qualification.QualifiedAt > 86400 {
		return ErrProtocol
	}
	previous := uint32(0)
	for _, group := range qualification.Groups {
		if group <= previous {
			return ErrProtocol
		}
		previous = group
	}
	for _, digest := range []string{qualification.HostKeyDigest, qualification.AuthenticationKey, qualification.DeploymentDigest, qualification.SSHDConfiguration, qualification.IdentityInventory, qualification.RuntimeReadback} {
		if !Digest(digest) {
			return ErrProtocol
		}
	}
	switch qualification.Mode {
	case ManagedTransport:
		profile := qualification.Profile
		if profile == nil || profile.Validate() != nil || profile.ValidUntil < qualification.ValidUntil || profile.UID != qualification.UID || profile.GID != qualification.GID || !slices.Equal(profile.Groups, qualification.Groups) || qualification.IdentityProbe != "" {
			return ErrProtocol
		}
	case ExecuteTransport:
		if qualification.Profile != nil || !Digest(qualification.IdentityProbe) || !slices.Contains(qualification.Groups, qualification.GID) {
			return ErrProtocol
		}
	default:
		return ErrProtocol
	}
	return nil
}

// OwnerReference accepts the common broker-identity owner's bounded opaque metadata syntax without treating an identifier supplied by a caller as proof of ownership.
// OwnerReference принимает bounded opaque metadata syntax общего broker-identity owner; caller identifier не является доказательством владения.
func OwnerReference(value string) bool {
	if len(value) < 8 || len(value) > 96 {
		return false
	}
	for _, character := range value {
		if character != '_' && character != '-' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// qualificationUser accepts only a literal non-root Unix account; its effective identity and privilege inventory must come from trusted operator observation.
// qualificationUser принимает только literal non-root Unix account; effective identity и privilege inventory должны происходить из trusted operator observation.
func qualificationUser(value string) bool {
	if len(value) < 1 || len(value) > 32 || value == "root" || value[0] != '_' && (value[0] < 'a' || value[0] > 'z') {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return character != '_' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9')
	}) == -1
}
