package integrationpackage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// mutateHistoricalSnapshot changes nested caller-owned reference fields, not merely top-level value copies.
// mutateHistoricalSnapshot меняет nested caller-owned reference fields, не только top-level value copies.
func mutateHistoricalSnapshot(snapshot *Package) {
	if snapshot.Spec.Credential != nil {
		snapshot.Spec.Credential.SecretKey = "mutated_secret_key"
	}
	if len(snapshot.Spec.ConfigurationFields) > 0 {
		snapshot.Spec.ConfigurationFields[0].Key = "mutated_configuration"
		snapshot.Spec.ConfigurationFields[0].AllowedValues = append(snapshot.Spec.ConfigurationFields[0].AllowedValues, "mutated")
	}
	if len(snapshot.Spec.NetworkDestinations) > 0 {
		snapshot.Spec.NetworkDestinations[0].Hostname = "mutated.invalid"
	}
	if len(snapshot.Spec.Capabilities) > 0 {
		capability := &snapshot.Spec.Capabilities[0]
		capability.Risk = "MUTATED"
		capability.AllowedApprovalPolicies = append(capability.AllowedApprovalPolicies, "MUTATED")
		if len(capability.ResourceScope.ConnectionFields) > 0 {
			capability.ResourceScope.ConnectionFields[0] = "mutated_scope"
		}
		if len(capability.InputFields) > 0 {
			capability.InputFields[0].Key = "mutated_input"
		}
		if len(capability.OutputFields) > 0 {
			capability.OutputFields[0].Key = "mutated_output"
		}
		if capability.ApprovalScope != nil {
			capability.ApprovalScope.Name = "mutated_approval_scope"
			if capability.ApprovalScope.Limits != nil {
				capability.ApprovalScope.Limits.MaxEffects = -1
			}
		}
	}
}

// TestHistoricalSnapshotCachePreservesFrozenBytesAndOwnedResults checks every source pin against its original strict YAML parse and isolates nested caller mutation.
// TestHistoricalSnapshotCachePreservesFrozenBytesAndOwnedResults сверяет каждый source pin с исходным strict YAML parse и изолирует nested caller mutation.
func TestHistoricalSnapshotCachePreservesFrozenBytesAndOwnedResults(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for key, entry := range historicalPolicyRevisions {
		for _, version := range entry.versions {
			t.Run(key+"/"+version, func(t *testing.T) {
				expected := legacyPolicyPackage(t, key, version)
				expectedBytes, err := json.Marshal(expected)
				if err != nil {
					t.Fatal(err)
				}
				first, ok := ResolveShippedRevision(shipped[key], version, expected.Digest)
				if !ok {
					t.Fatal("frozen pin rejected")
				}
				actual, err := json.Marshal(first)
				if err != nil || !bytes.Equal(actual, expectedBytes) || first.Digest != expected.Digest {
					t.Fatal("canonical cache changed frozen package bytes or digest")
				}
				mutateHistoricalSnapshot(&first)
				next, ok := ResolveShippedRevision(shipped[key], version, expected.Digest)
				actual, err = json.Marshal(next)
				if !ok || err != nil || !bytes.Equal(actual, expectedBytes) || next.Digest != expected.Digest || ValidateExecutableRevision(next, shipped[key]) != nil {
					t.Fatal("caller mutation poisoned a later owned snapshot")
				}
			})
		}
	}
}

// TestHistoricalSnapshotCacheUnknownPinsAndFreshGates cannot allocate cache entries from arbitrary versions or reuse a prior successor/digest decision.
// TestHistoricalSnapshotCacheUnknownPinsAndFreshGates запрещает cache entries от arbitrary versions и повтор прежнего successor/digest decision.
func TestHistoricalSnapshotCacheUnknownPinsAndFreshGates(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := shipped["github"]
	previous := legacyPolicyPackage(t, "github", "6.4.0")
	before := len(historicalPolicyCanonicalSnapshots)
	expected := 0
	for _, entry := range historicalPolicyRevisions {
		expected += len(entry.versions)
	}
	if before != expected {
		t.Fatal("cache is not bounded by the closed source registry")
	}
	if _, ok := ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest); !ok {
		t.Fatal("known predecessor unavailable")
	}
	for i := range 128 {
		if _, ok := ResolveShippedRevision(current, fmt.Sprintf("999.%d.0", i), previous.Digest); ok {
			t.Fatal("unknown predecessor was admitted")
		}
	}
	for _, change := range []string{"origin", "key", "adapter", "version", "digest"} {
		t.Run(change, func(t *testing.T) {
			changed := current
			digest := previous.Digest
			switch change {
			case "origin":
				changed.Metadata.Origin = OriginUI
			case "key":
				changed.Metadata.Key = "unregistered"
			case "adapter":
				changed.Spec.Adapter = "UNREGISTERED"
			case "version":
				changed.Metadata.Version = "999.0.0"
			case "digest":
				digest = strings.Repeat("0", 64)
			}
			if _, ok := ResolveShippedRevision(changed, previous.Metadata.Version, digest); ok {
				t.Fatal("warm cache bypassed a fresh successor or digest guard")
			}
		})
	}
	if len(historicalPolicyCanonicalSnapshots) != before {
		t.Fatal("unknown callers expanded the source-only cache")
	}
}

// TestHistoricalSnapshotCacheConcurrentInitializationAndOwnership exercises the same closed cache constructor cold, then concurrent actual resolves with independent nested mutations.
// TestHistoricalSnapshotCacheConcurrentInitializationAndOwnership проверяет cold closed cache constructor и concurrent actual resolves с independent nested mutations.
func TestHistoricalSnapshotCacheConcurrentInitializationAndOwnership(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := shipped["github"]
	expected := legacyPolicyPackage(t, "github", "6.4.0")
	canonical, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	cold := newHistoricalPolicyCanonicalSnapshots()
	read := cold[historicalPolicySnapshotPin{key: "github", version: "6.4.0"}]
	failures := make(chan string, 8)
	var joined sync.WaitGroup
	for range 8 {
		joined.Go(func() {
			for range 4 {
				stored, ok := read()
				if !ok || stored.canonical != string(canonical) || stored.digest != expected.Digest {
					failures <- "concurrent initialization changed canonical bytes"
					return
				}
				snapshot, err := Parse([]byte(stored.canonical))
				if err != nil || snapshot.Digest != expected.Digest {
					failures <- "fresh canonical parse failed"
					return
				}
				resolved, ok := ResolveShippedRevision(current, expected.Metadata.Version, expected.Digest)
				if !ok {
					failures <- "concurrent actual resolve failed"
					return
				}
				mutateHistoricalSnapshot(&snapshot)
				mutateHistoricalSnapshot(&resolved)
			}
		})
	}
	joined.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	final, ok := ResolveShippedRevision(current, expected.Metadata.Version, expected.Digest)
	actual, err := json.Marshal(final)
	if !ok || err != nil || !bytes.Equal(actual, canonical) {
		t.Fatal("concurrent mutation escaped owned results")
	}
}

// TestHistoricalSnapshotCacheNeverValidatesCallerInput proves warm built-ins do not replace request-local strict parsing or executable-revision checks.
// TestHistoricalSnapshotCacheNeverValidatesCallerInput доказывает, что warm built-ins не заменяют request-local strict parsing либо executable-revision checks.
func TestHistoricalSnapshotCacheNeverValidatesCallerInput(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := shipped["github"]
	previous := legacyPolicyPackage(t, "github", "6.4.0")
	known, ok := ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest)
	if !ok {
		t.Fatal("known snapshot unavailable")
	}
	raw, err := json.Marshal(known)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		append([]byte(`{"kind":"IntegrationPackage",`), raw[1:]...),
		bytes.Replace(raw, []byte(`"kind":`), []byte(`"Kind":`), 1),
		append([]byte(`{"unregistered":true,`), raw[1:]...),
		append(append([]byte(nil), raw...), []byte(`{}`)...),
	} {
		if _, err := Parse(bad); err == nil {
			t.Fatal("warm snapshot bypassed malformed request-local package parsing")
		}
	}
	known.Spec.Description = "Synthetic modified package metadata"
	changed, err := json.Marshal(known)
	if err != nil {
		t.Fatal("encode independently canonicalized candidate", err)
	}
	changedCandidate, err := Parse(changed)
	if err != nil || !canonicalPackage(changedCandidate) {
		t.Fatal("changed package fixture did not pass canonical validation", err)
	}
	if ValidateExecutableRevision(changedCandidate, current) == nil {
		t.Fatal("self-consistent candidate did not match its frozen source digest")
	}
	known.Spec.Capabilities[0].Risk = "UNREGISTERED"
	if ValidateExecutableRevision(known, current) == nil {
		t.Fatal("warm snapshot bypassed mutated candidate validation")
	}
}
