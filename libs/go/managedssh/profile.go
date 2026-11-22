package managedssh

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"
)

// Profile is an operator-installed immutable helper configuration, not an authority document accepted from an employee or connection checkbox.
// Profile является устанавливаемой оператором immutable helper configuration, а не authority document от сотрудника либо connection checkbox.
type Profile struct {
	Version             uint8          `json:"version"`
	Revision            uint64         `json:"revision"`
	ValidUntil          int64          `json:"valid_until_unix"`
	UID                 uint32         `json:"uid"`
	GID                 uint32         `json:"gid"`
	Groups              []uint32       `json:"groups"`
	ExecutableDigest    string         `json:"executable_sha256"`
	ShellDigest         string         `json:"account_shell_sha256"`
	SystemctlDigest     string         `json:"systemctl_sha256,omitempty"`
	JournalctlDigest    string         `json:"journalctl_sha256,omitempty"`
	ControlPolicyDigest string         `json:"control_policy_sha256,omitempty"`
	StateDirectory      string         `json:"state_directory"`
	ReceiptLimit        uint16         `json:"receipt_limit"`
	Roots               []RootScope    `json:"roots"`
	Services            []ServiceScope `json:"services"`
}

// RootScope assigns one disjoint helper-managed namespace; ownership, exclusive-writer deployment and kernel path enforcement are verified separately.
// RootScope назначает один disjoint helper-managed namespace; ownership, exclusive-writer deployment и kernel path enforcement проверяются отдельно.
type RootScope struct {
	Alias string `json:"alias"`
	Path  string `json:"path"`
}

// ServiceScope assigns one operator-owned system unit running as a distinct non-root UID; cross-identity changes always require EACH consent.
// ServiceScope назначает operator-owned system unit с отдельным непривилегированным UID; изменения через другую identity всегда требуют EACH consent.
type ServiceScope struct {
	Alias      string `json:"alias"`
	Unit       string `json:"unit"`
	UnitDigest string `json:"unit_sha256"`
	UID        uint32 `json:"uid"`
}

// EncodeProfile checks the declarative pins without claiming that the operator installation is present, trusted, ready or isolated from another account.
// EncodeProfile проверяет declarative pins без утверждения о наличии, доверии, готовности либо изоляции operator installation от другой account.
func EncodeProfile(profile Profile) ([]byte, error) {
	if profile.Validate() != nil {
		return nil, ErrProtocol
	}
	return canonical(profile)
}

// DecodeProfile admits only the exact canonical pinned configuration; installed-file ownership and current expiry remain runtime checks.
// DecodeProfile допускает только exact canonical pinned configuration; installed-file ownership и current expiry проверяются runtime.
func DecodeProfile(raw []byte) (Profile, error) {
	var profile Profile
	if decodeCanonical(raw, &profile) != nil || profile.Validate() != nil {
		return Profile{}, ErrProtocol
	}
	return profile, nil
}

// Digest returns the complete profile revision hash; root/service selection or expiry changes invalidate every old request pin.
// Digest возвращает hash полной profile revision; изменения root/service selection либо expiry аннулируют старый request pin.
func (profile Profile) Digest() (string, error) {
	raw, err := EncodeProfile(profile)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// Validate checks closed finite configuration and disjoint absolute namespaces, without treating these assertions as proof of remote operator authority.
// Validate проверяет closed finite configuration и disjoint absolute namespaces, не рассматривая assertions как доказательство remote operator authority.
func (profile Profile) Validate() error {
	identity := Identity{UID: profile.UID, GID: profile.GID, Groups: profile.Groups, Executable: profile.ExecutableDigest}
	if profile.Version != Version || profile.Revision == 0 || profile.ValidUntil <= 0 || !identity.valid() || !Digest(profile.ShellDigest) || !absoluteDirectory(profile.StateDirectory) || profile.ReceiptLimit == 0 || profile.ReceiptLimit > 4096 || len(profile.Roots) > 16 || len(profile.Services) > 16 {
		return ErrProtocol
	}
	if len(profile.Services) > 0 && (!Digest(profile.SystemctlDigest) || !Digest(profile.JournalctlDigest) || !Digest(profile.ControlPolicyDigest)) || len(profile.Services) == 0 && (profile.SystemctlDigest != "" || profile.JournalctlDigest != "" || profile.ControlPolicyDigest != "") {
		return ErrProtocol
	}
	paths := []string{profile.StateDirectory}
	previous := ""
	for _, root := range profile.Roots {
		if !Alias(root.Alias) || root.Alias <= previous || !absoluteDirectory(root.Path) {
			return ErrProtocol
		}
		previous = root.Alias
		for _, other := range paths {
			if other == root.Path || strings.HasPrefix(other, root.Path+"/") || strings.HasPrefix(root.Path, other+"/") {
				return ErrProtocol
			}
		}
		paths = append(paths, root.Path)
	}
	previous = ""
	for _, service := range profile.Services {
		if !Alias(service.Alias) || service.Alias <= previous || len(service.Unit) > 64 || !strings.HasSuffix(service.Unit, ".service") || !Alias(strings.TrimSuffix(service.Unit, ".service")) || !Digest(service.UnitDigest) || service.UID == 0 || service.UID == profile.UID {
			return ErrProtocol
		}
		previous = service.Alias
	}
	return nil
}

// absoluteDirectory requires a canonical non-root absolute spelling without symlink claims; installed descriptor checks provide the actual filesystem boundary.
// absoluteDirectory требует canonical non-root absolute spelling без утверждений о symlinks; installed descriptor checks обеспечивают фактическую filesystem boundary.
func absoluteDirectory(value string) bool {
	return strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value && RelativePath(strings.TrimPrefix(value, "/"))
}
