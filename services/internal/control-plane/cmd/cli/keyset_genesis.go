package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

type keysetDocument struct {
	Version, Revision, HighWatermark, ServedGeneration uint64      `json:"-"`
	Keys                                               []keysetKey `json:"keys"`
}

func (value *keysetDocument) UnmarshalJSON(raw []byte) error {
	type wire struct {
		Version          uint64      `json:"version"`
		Revision         uint64      `json:"revision"`
		HighWatermark    uint64      `json:"high_watermark"`
		ServedGeneration uint64      `json:"served_generation"`
		Keys             []keysetKey `json:"keys"`
	}
	var decoded wire
	if internalrpcauth.DecodeCanonicalJSON(raw, &decoded) != nil {
		return errors.New("decode keyset genesis document")
	}
	value.Version, value.Revision = decoded.Version, decoded.Revision
	value.HighWatermark, value.ServedGeneration, value.Keys = decoded.HighWatermark, decoded.ServedGeneration, decoded.Keys
	return nil
}

type keysetKey struct {
	Generation uint64          `json:"generation"`
	Status     string          `json:"status"`
	JWK        json.RawMessage `json:"jwk"`
}

type keysetIdentity struct {
	Generation uint64 `json:"generation"`
	Status     string `json:"status"`
	KeyID      string `json:"kid"`
	Thumbprint string `json:"thumbprint_sha256"`
}

type keysetGenesisSpec struct {
	name, producerID, keysetFile, bootstrapSQL, readbackSQL string
}

func reconcileKeysetGeneses(ctx context.Context, database migrationConnection, config migrationConfig) error {
	if !config.KeysetGenesisEnabled {
		return errors.New("explicit keyset genesis reconciliation is required")
	}
	for _, spec := range []keysetGenesisSpec{
		{
			name: "Mattermost event", producerID: "control-plane.interaction-gateway",
			keysetFile:   config.MattermostEventPublicKeysetFile,
			bootstrapSQL: "sql/mattermost_event_keyset__bootstrap_genesis.sql",
			readbackSQL:  "sql/mattermost_event_keyset__genesis_readback.sql",
		},
		{
			name: "interaction delivery readback", keysetFile: config.InteractionReadbackPublicKeysetFile,
			bootstrapSQL: "sql/interaction_readback_keyset__bootstrap_genesis.sql",
			readbackSQL:  "sql/interaction_readback_keyset__genesis_readback.sql",
		},
	} {
		if err := reconcileKeysetGenesis(ctx, database, spec); err != nil {
			return err
		}
	}
	return nil
}

func reconcileKeysetGenesis(ctx context.Context, database migrationConnection, spec keysetGenesisSpec) error {
	raw, err := readRuntimeFile(spec.keysetFile, 64<<10)
	if err != nil {
		return fmt.Errorf("read %s keyset genesis input", spec.name)
	}
	document, identities, digest, err := parseKeysetGenesis(raw)
	if err != nil {
		return fmt.Errorf("parse %s keyset genesis input: %w", spec.name, err)
	}
	encoded, err := json.Marshal(identities)
	if err != nil {
		return fmt.Errorf("encode %s key identities", spec.name)
	}
	if keysetGenesisReadback(ctx, database, spec, document, digest, identities) == nil {
		return nil
	}
	statement, err := operationalSQL.ReadFile(spec.bootstrapSQL)
	if err != nil {
		return fmt.Errorf("load %s keyset genesis command", spec.name)
	}
	var execErr error
	if spec.producerID == "" {
		_, execErr = database.ExecContext(ctx, string(statement), document.Revision, document.HighWatermark,
			document.ServedGeneration, digest, encoded)
	} else {
		_, execErr = database.ExecContext(ctx, string(statement), spec.producerID, document.Revision,
			document.HighWatermark, document.ServedGeneration, digest, encoded)
	}
	if readbackErr := keysetGenesisReadback(ctx, database, spec, document, digest, identities); readbackErr != nil {
		return errors.Join(fmt.Errorf("bootstrap %s keyset genesis", spec.name), execErr, readbackErr)
	}
	return nil
}

func keysetGenesisReadback(ctx context.Context, database migrationConnection, spec keysetGenesisSpec,
	document keysetDocument, digest string, identities []keysetIdentity) error {
	statement, err := operationalSQL.ReadFile(spec.readbackSQL)
	if err != nil {
		return fmt.Errorf("load %s keyset genesis readback", spec.name)
	}
	var revision, highWatermark, servedGeneration uint64
	var storedDigest string
	var encoded []byte
	var queryErr error
	if spec.producerID == "" {
		queryErr = database.QueryRowContext(ctx, string(statement)).Scan(
			&revision, &highWatermark, &servedGeneration, &storedDigest, &encoded)
	} else {
		queryErr = database.QueryRowContext(ctx, string(statement), spec.producerID).Scan(
			&revision, &highWatermark, &servedGeneration, &storedDigest, &encoded)
	}
	var stored []keysetIdentity
	if queryErr != nil || json.Unmarshal(encoded, &stored) != nil || revision != document.Revision ||
		highWatermark != document.HighWatermark || servedGeneration != document.ServedGeneration ||
		storedDigest != digest || !slices.Equal(stored, identities) {
		return fmt.Errorf("%s keyset genesis readback mismatch", spec.name)
	}
	return nil
}

func parseKeysetGenesis(raw []byte) (keysetDocument, []keysetIdentity, string, error) {
	var document keysetDocument
	if json.Unmarshal(raw, &document) != nil || document.Version != 1 || document.Revision == 0 ||
		document.HighWatermark == 0 || document.ServedGeneration != document.HighWatermark ||
		len(document.Keys) == 0 || len(document.Keys) > 4 {
		return keysetDocument{}, nil, "", errors.New("keyset genesis document is invalid")
	}
	identities := make([]keysetIdentity, 0, len(document.Keys))
	seenGeneration, seenKeyID, seenThumbprint := map[uint64]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	current := 0
	for _, reference := range document.Keys {
		if reference.Generation == 0 || !slices.Contains([]string{"CURRENT", "NEXT", "PREVIOUS", "RETIRED"}, reference.Status) {
			return keysetDocument{}, nil, "", errors.New("keyset genesis lifecycle is invalid")
		}
		key, parseErr := internalrpcauth.ParsePublicJWK(reference.JWK)
		if parseErr != nil || key.KeyID == "" {
			return keysetDocument{}, nil, "", errors.New("parse keyset genesis public JWK")
		}
		thumbprint, thumbErr := internalrpcauth.PublicJWKThumbprintSHA256(key)
		if thumbErr != nil {
			return keysetDocument{}, nil, "", errors.New("calculate keyset genesis public JWK thumbprint")
		}
		if _, ok := seenGeneration[reference.Generation]; ok {
			return keysetDocument{}, nil, "", errors.New("keyset genesis has duplicate generation")
		}
		if _, ok := seenKeyID[key.KeyID]; ok {
			return keysetDocument{}, nil, "", errors.New("keyset genesis has duplicate kid")
		}
		if _, ok := seenThumbprint[thumbprint]; ok {
			return keysetDocument{}, nil, "", errors.New("keyset genesis has duplicate public key")
		}
		seenGeneration[reference.Generation], seenKeyID[key.KeyID], seenThumbprint[thumbprint] = struct{}{}, struct{}{}, struct{}{}
		identities = append(identities, keysetIdentity{reference.Generation, reference.Status, key.KeyID, thumbprint})
		if reference.Status == "CURRENT" && reference.Generation == document.ServedGeneration {
			current++
		} else if reference.Status == "CURRENT" {
			return keysetDocument{}, nil, "", errors.New("keyset genesis CURRENT generation is invalid")
		}
	}
	if current != 1 {
		return keysetDocument{}, nil, "", errors.New("keyset genesis must contain one CURRENT key")
	}
	slices.SortFunc(identities, func(left, right keysetIdentity) int {
		if left.Generation < right.Generation {
			return -1
		}
		if left.Generation > right.Generation {
			return 1
		}
		return 0
	})
	sum := sha256.Sum256(raw)
	return document, identities, hex.EncodeToString(sum[:]), nil
}
