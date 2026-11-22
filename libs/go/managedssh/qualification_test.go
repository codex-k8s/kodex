package managedssh

import (
	"bytes"
	"strings"
	"testing"
)

// qualificationFixture creates only synthetic public deployment metadata with a short valid lifetime and no authority over a live host.
// qualificationFixture создаёт только synthetic public deployment metadata с short valid lifetime без authority над живым host.
func qualificationFixture() Qualifications {
	return Qualifications{Schema: QualificationSchema, Profiles: []TargetQualification{{TargetRef: "ssh_target_example01", Address: "192.0.2.10", Port: 22, User: "kodex_exec", Mode: ExecuteTransport, HostKeyAlgorithm: ED25519HostKey, HostKeyDigest: strings.Repeat("a", 64), AuthenticationKey: strings.Repeat("b", 64), UID: 1001, GID: 1001, Groups: []uint32{1001}, QualifiedAt: 1000, ValidUntil: 2000, DeploymentDigest: strings.Repeat("c", 64), SSHDConfiguration: strings.Repeat("d", 64), IdentityInventory: strings.Repeat("e", 64), RuntimeReadback: strings.Repeat("f", 64), IdentityProbe: strings.Repeat("9", 64)}}}
}

// TestQualificationRejectsExpiredMixedAndSelfExpandedProfiles keeps the operator evidence boundary closed for both modes, including non-root aliases that actually name UID zero.
// TestQualificationRejectsExpiredMixedAndSelfExpandedProfiles закрывает operator evidence boundary для обоих modes, включая non-root aliases с фактическим UID zero.
func TestQualificationRejectsExpiredMixedAndSelfExpandedProfiles(t *testing.T) {
	document := qualificationFixture()
	raw, err := EncodeQualifications(document, 1500)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeQualifications(raw, 1500); err != nil {
		t.Fatal("valid execute deployment rejected")
	}
	for _, now := range []int64{900, 2000, 2001} {
		if _, err := DecodeQualifications(raw, now); err == nil {
			t.Fatal("future or expired deployment accepted")
		}
	}
	mutations := []func(*Qualifications){
		func(value *Qualifications) { value.Profiles[0].UID = 0 },
		func(value *Qualifications) { value.Profiles[0].GID = 0 },
		func(value *Qualifications) { value.Profiles[0].User = "root" },
		func(value *Qualifications) { value.Profiles[0].Address = "127.0.0.1" },
		func(value *Qualifications) { value.Profiles[0].Groups = []uint32{1001, 1001} },
		func(value *Qualifications) { value.Profiles[0].Groups = nil },
		func(value *Qualifications) { value.Profiles[0].ValidUntil = 1000 + 86401 },
		func(value *Qualifications) { value.Profiles[0].RuntimeReadback = strings.Repeat("0", 64) },
		func(value *Qualifications) { value.Profiles[0].Mode = "READ_SHELL" },
		func(value *Qualifications) { value.Profiles[0].Mode = ManagedTransport },
		func(value *Qualifications) { value.Profiles[0].Profile = &Profile{} },
		func(value *Qualifications) { value.Profiles[0].IdentityProbe = "" },
		func(value *Qualifications) { value.Profiles[0].Groups = []uint32{1002} },
		func(value *Qualifications) { value.Profiles = append(value.Profiles, value.Profiles[0]) },
	}
	for index, mutate := range mutations {
		value := qualificationFixture()
		mutate(&value)
		if _, err := EncodeQualifications(value, 1500); err == nil {
			t.Fatal("invalid deployment accepted", index)
		}
	}
	for _, invalid := range [][]byte{
		append(bytes.Clone(raw), '\n'),
		bytes.Replace(raw, []byte(`"uid":1001`), []byte(`"uid":null`), 1),
		bytes.Replace(raw, []byte(`"uid":1001`), []byte(`"UID":1001`), 1),
		bytes.Replace(raw, []byte(`"uid":1001`), []byte(`"uid":1001,"UID":0`), 1),
		bytes.Replace(raw, []byte(`"mode":"EXECUTE"`), []byte(`"mode":"EXECUTE","profile":null`), 1),
	} {
		if _, err := DecodeQualifications(invalid, 1500); err == nil {
			t.Fatal("ambiguous operator JSON accepted")
		}
	}
}

// TestQualificationManagedIdentityMustMatchInstalledProfile rejects a different helper UID, group set or shorter installed expiry even when all proof digests remain syntactically valid.
// TestQualificationManagedIdentityMustMatchInstalledProfile отклоняет другой helper UID, group set либо более короткий installed expiry при синтаксически valid proof digests.
func TestQualificationManagedIdentityMustMatchInstalledProfile(t *testing.T) {
	document := qualificationFixture()
	value := &document.Profiles[0]
	value.Mode = ManagedTransport
	value.IdentityProbe = ""
	value.Profile = &Profile{Version: Version, Revision: 1, ValidUntil: 3000, UID: value.UID, GID: value.GID, Groups: value.Groups, ExecutableDigest: strings.Repeat("a", 64), ShellDigest: strings.Repeat("8", 64), StateDirectory: "/srv/kodex/receipts", ReceiptLimit: 128, Roots: []RootScope{}, Services: []ServiceScope{}}
	if _, err := EncodeQualifications(document, 1500); err != nil {
		t.Fatal("matching managed deployment rejected")
	}
	value.Profile.UID++
	if _, err := EncodeQualifications(document, 1500); err == nil {
		t.Fatal("foreign helper identity accepted")
	}
	value.Profile.UID--
	value.Profile.ValidUntil = 1999
	if _, err := EncodeQualifications(document, 1500); err == nil {
		t.Fatal("helper expires before operator evidence")
	}
}

// TestQualificationSupportsPersonalKeysWithoutRetargeting allows independently qualified credentials on one connection while rejecting contradictory deployment facts or duplicate key authority.
// TestQualificationSupportsPersonalKeysWithoutRetargeting допускает независимо qualified credentials одного connection, отклоняя contradictory deployment facts либо duplicate key authority.
func TestQualificationSupportsPersonalKeysWithoutRetargeting(t *testing.T) {
	document := qualificationFixture()
	second := document.Profiles[0]
	second.AuthenticationKey = strings.Repeat("7", 64)
	second.RuntimeReadback = strings.Repeat("6", 64)
	document.Profiles = append(document.Profiles, second)
	if _, err := EncodeQualifications(document, 1500); err != nil {
		t.Fatal("different personal keys for identical installation rejected")
	}
	for _, mutate := range []func(*TargetQualification){
		func(profile *TargetQualification) { profile.Address = "192.0.2.11" },
		func(profile *TargetQualification) { profile.User = "another_user" },
		func(profile *TargetQualification) { profile.UID = 1002 },
		func(profile *TargetQualification) { profile.HostKeyDigest = strings.Repeat("8", 64) },
		func(profile *TargetQualification) { profile.HostKeyAlgorithm = ECDSA256HostKey },
		func(profile *TargetQualification) { profile.DeploymentDigest = strings.Repeat("8", 64) },
		func(profile *TargetQualification) { profile.AuthenticationKey = document.Profiles[0].AuthenticationKey },
	} {
		value := Qualifications{Schema: QualificationSchema, Profiles: []TargetQualification{document.Profiles[0], second}}
		mutate(&value.Profiles[1])
		if _, err := EncodeQualifications(value, 1500); err == nil {
			t.Fatal("personal key retargeted installation or duplicated authority")
		}
	}
}

// TestQualificationV2RequiresExplicitHostAlgorithm rejects historical or incomplete artifacts instead of inferring a host algorithm from the peer; the exact schema and captured key type are installation inputs.
// TestQualificationV2RequiresExplicitHostAlgorithm отклоняет historical либо incomplete artifacts вместо вывода host algorithm из peer; exact schema и captured key type являются installation inputs.
func TestQualificationV2RequiresExplicitHostAlgorithm(t *testing.T) {
	document := qualificationFixture()
	for _, algorithm := range []HostKeyAlgorithm{"", "rsa-sha2-256", "ssh-dss", "ssh-ed25519-cert-v01@openssh.com"} {
		changed := document
		changed.Profiles = append([]TargetQualification(nil), document.Profiles...)
		changed.Profiles[0].HostKeyAlgorithm = algorithm
		if changed.Validate(1100) == nil {
			t.Fatal("unknown or missing raw host algorithm accepted")
		}
	}
	document.Schema = "kodex-ssh-qualifications/v1"
	if document.Validate(1100) == nil {
		t.Fatal("historical artifact silently upgraded")
	}
}
