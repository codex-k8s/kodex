package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

type genesisKeyset struct {
	Version, Revision, HighWatermark, ServedGeneration uint64       `json:"-"`
	Keys                                               []genesisKey `json:"keys"`
}

func (value *genesisKeyset) UnmarshalJSON(raw []byte) error {
	var decoded struct {
		Version          uint64       `json:"version"`
		Revision         uint64       `json:"revision"`
		HighWatermark    uint64       `json:"high_watermark"`
		ServedGeneration uint64       `json:"served_generation"`
		Keys             []genesisKey `json:"keys"`
	}
	if internalrpcauth.DecodeCanonicalJSON(raw, &decoded) != nil {
		return errors.New("decode delivery readback keyset genesis")
	}
	value.Version, value.Revision = decoded.Version, decoded.Revision
	value.HighWatermark, value.ServedGeneration, value.Keys = decoded.HighWatermark, decoded.ServedGeneration, decoded.Keys
	return nil
}

type genesisKey struct {
	Generation uint64          `json:"generation"`
	Status     string          `json:"status"`
	JWK        json.RawMessage `json:"jwk"`
}

type genesisIdentity struct {
	Generation uint64 `json:"generation"`
	Status     string `json:"status"`
	KeyID      string `json:"kid"`
	Thumbprint string `json:"thumbprint_sha256"`
}

func reconcileReadbackKeysetGenesis(ctx context.Context, database migrationConnection, value config) error {
	if !value.KeysetGenesisEnabled {
		return errors.New("explicit delivery readback keyset genesis reconciliation is required")
	}
	raw, err := readFile(value.ReadbackPublicKeysetFile, 64<<10)
	if err != nil {
		return errors.New("read delivery readback keyset genesis input")
	}
	var document genesisKeyset
	if json.Unmarshal(raw, &document) != nil || document.Version != 1 || document.Revision == 0 ||
		document.HighWatermark == 0 || document.ServedGeneration != document.HighWatermark ||
		len(document.Keys) == 0 || len(document.Keys) > 4 {
		return errors.New("delivery readback keyset genesis is invalid")
	}
	identities := make([]genesisIdentity, 0, len(document.Keys))
	seenGeneration, seenKid, seenThumb := map[uint64]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	current := 0
	for _, reference := range document.Keys {
		if reference.Generation == 0 || !slices.Contains([]string{"CURRENT", "NEXT", "PREVIOUS", "RETIRED"}, reference.Status) {
			return errors.New("delivery readback keyset genesis lifecycle is invalid")
		}
		key, parseErr := internalrpcauth.ParsePublicJWK(reference.JWK)
		if parseErr != nil || key.KeyID == "" {
			return errors.New("parse delivery readback keyset genesis public JWK")
		}
		thumbprint, thumbErr := internalrpcauth.PublicJWKThumbprintSHA256(key)
		if thumbErr != nil {
			return errors.New("calculate delivery readback keyset genesis thumbprint")
		}
		if _, ok := seenGeneration[reference.Generation]; ok {
			return errors.New("delivery readback keyset genesis has duplicate generation")
		}
		if _, ok := seenKid[key.KeyID]; ok {
			return errors.New("delivery readback keyset genesis has duplicate kid")
		}
		if _, ok := seenThumb[thumbprint]; ok {
			return errors.New("delivery readback keyset genesis has duplicate public key")
		}
		seenGeneration[reference.Generation], seenKid[key.KeyID], seenThumb[thumbprint] = struct{}{}, struct{}{}, struct{}{}
		identities = append(identities, genesisIdentity{reference.Generation, reference.Status, key.KeyID, thumbprint})
		if reference.Status == "CURRENT" && reference.Generation == document.ServedGeneration {
			current++
		} else if reference.Status == "CURRENT" {
			return errors.New("delivery readback keyset genesis CURRENT generation is invalid")
		}
	}
	if current != 1 {
		return errors.New("delivery readback keyset genesis must contain one CURRENT key")
	}
	slices.SortFunc(identities, func(left, right genesisIdentity) int {
		if left.Generation < right.Generation {
			return -1
		}
		if left.Generation > right.Generation {
			return 1
		}
		return 0
	})
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if readbackDeliveryReadbackGenesis(ctx, database, document, digest, identities) == nil {
		return nil
	}
	encoded, err := json.Marshal(identities)
	if err != nil {
		return errors.New("encode delivery readback keyset genesis identities")
	}
	statement, err := operationalSQL.ReadFile("sql/delivery_readback_keyset__bootstrap_genesis.sql")
	if err != nil {
		return errors.New("load delivery readback keyset genesis command")
	}
	_, bootstrapErr := database.ExecContext(ctx, string(statement), document.Revision, document.HighWatermark,
		document.ServedGeneration, digest, encoded)
	if readbackErr := readbackDeliveryReadbackGenesis(ctx, database, document, digest, identities); readbackErr != nil {
		return errors.Join(errors.New("bootstrap delivery readback keyset genesis"), bootstrapErr, readbackErr)
	}
	return nil
}

func readbackDeliveryReadbackGenesis(ctx context.Context, database migrationConnection, document genesisKeyset,
	digest string, identities []genesisIdentity) error {
	statement, err := operationalSQL.ReadFile("sql/delivery_readback_keyset__genesis_readback.sql")
	if err != nil {
		return errors.New("load delivery readback keyset genesis readback")
	}
	var revision, highWatermark, servedGeneration uint64
	var storedDigest string
	var encoded []byte
	if err := database.QueryRowContext(ctx, string(statement)).Scan(&revision, &highWatermark, &servedGeneration,
		&storedDigest, &encoded); err != nil {
		return errors.New("read delivery readback keyset genesis")
	}
	var stored []genesisIdentity
	if json.Unmarshal(encoded, &stored) != nil || revision != document.Revision ||
		highWatermark != document.HighWatermark || servedGeneration != document.ServedGeneration ||
		storedDigest != digest || !slices.Equal(stored, identities) {
		return errors.New("delivery readback keyset genesis readback mismatch")
	}
	return nil
}
