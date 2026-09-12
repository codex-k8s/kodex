package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"io"
)

//go:embed sql/rotation__repair_legacy_provenance.sql
var legacyRepairSQL string

type legacyRepairProof struct {
	SourceRevision int64  `json:"sourceRevision"`
	SnapshotDigest string `json:"snapshotDigestSHA256"`
	InputPreimage  string `json:"inputPreimage"`
}

func readLegacyRepairProof(args []string) (legacyRepairProof, error) {
	var proof legacyRepairProof
	if len(args) != 5 || args[1] != "--proof-file" || args[3] != "--confirm" || args[4] != "REPAIR-STAGING-LEGACY-PROVENANCE" {
		return proof, errors.New("legacy provenance arguments rejected")
	}
	raw, err := securefile.Read(args[2], 768<<10)
	if err != nil {
		return proof, errors.New("legacy provenance proof unavailable")
	}
	defer clear(raw)
	if internalrpcauth.DecodeCanonicalJSON(raw, &proof) != nil || proof.SourceRevision < 1 || proof.SourceRevision > 9007199254740991 || !rotationDigestPattern.MatchString(proof.SnapshotDigest) || len(proof.InputPreimage) == 0 || len(proof.InputPreimage) > 524288 {
		return legacyRepairProof{}, errors.New("legacy provenance proof rejected")
	}
	return proof, nil
}

func repairLegacyProvenance(ctx context.Context, db *sql.DB, proof legacyRepairProof, output io.Writer) error {
	var accepted bool
	if err := db.QueryRowContext(ctx, legacyRepairSQL, proof.SourceRevision, proof.SnapshotDigest, proof.InputPreimage).Scan(&accepted); err != nil {
		return errors.New("legacy provenance persistence failed")
	}
	if !accepted {
		return errors.New("legacy provenance binding rejected")
	}
	return json.NewEncoder(output).Encode(struct {
		Status         string `json:"status"`
		SourceRevision int64  `json:"sourceRevision"`
	}{"PASS", proof.SourceRevision})
}
