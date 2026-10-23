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

// legacyRepairSQL embeds the explicit provenance-repair command used by the existing administrative CLI. The declaration neither applies a migration nor executes a repair on startup.
// legacyRepairSQL встраивает явную команду восстановления происхождения для существующего административного CLI. Объявление не применяет миграцию и не запускает восстановление при старте.
//
//go:embed sql/rotation__repair_legacy_provenance.sql
var legacyRepairSQL string

// legacyRepairProof carries the source revision, snapshot digest and exact canonical publication-input preimage for a one-off administrative repair. Field presence alone proves neither history ownership nor matching persisted provenance; parsing and database binding checks are separate.
// legacyRepairProof хранит исходную ревизию, дайджест снимка и точный канонический прообраз входа публикации для однократного административного восстановления. Присутствие полей не доказывает владение историей или совпадение устойчивого происхождения; разбор и привязки БД проверяются отдельно.
type legacyRepairProof struct {
	SourceRevision int64  `json:"sourceRevision"`
	SnapshotDigest string `json:"snapshotDigestSHA256"`
	InputPreimage  string `json:"inputPreimage"`
}

// readLegacyRepairProof requires the exact five repair arguments and staging confirmation, reads at most one MiB through securefile and clears that raw buffer on return. Canonical JSON must carry a positive JavaScript-safe revision, a lowercase sixty-four-hex digest and a nonempty bounded preimage; the returned string is not erased by clearing the input buffer, and the parser does not authorize or execute repair.
// readLegacyRepairProof требует точные пять аргументов восстановления и подтверждение staging, читает не более одного МиБ через securefile и очищает этот исходный буфер при возврате. Канонический JSON должен содержать положительную безопасную для JavaScript ревизию, дайджест из шестидесяти четырёх шестнадцатеричных символов нижнего регистра и непустой ограниченный прообраз; очистка входного буфера не стирает возвращённую строку, разборщик не авторизует и не выполняет восстановление.
func readLegacyRepairProof(args []string) (legacyRepairProof, error) {
	var proof legacyRepairProof
	if len(args) != 5 || args[1] != "--proof-file" || args[3] != "--confirm" || args[4] != "REPAIR-STAGING-LEGACY-PROVENANCE" {
		return proof, errors.New("legacy provenance arguments rejected")
	}
	raw, err := securefile.Read(args[2], 1<<20)
	if err != nil {
		return proof, errors.New("legacy provenance proof unavailable")
	}
	defer clear(raw)
	if internalrpcauth.DecodeCanonicalJSON(raw, &proof) != nil || proof.SourceRevision < 1 || proof.SourceRevision > 9007199254740991 || !rotationDigestPattern.MatchString(proof.SnapshotDigest) || len(proof.InputPreimage) == 0 || len(proof.InputPreimage) > 1048576 {
		return legacyRepairProof{}, errors.New("legacy provenance proof rejected")
	}
	return proof, nil
}

// repairLegacyProvenance submits the exact proof fields to the owner database function, requires its accepted flag and JSON-encodes PASS with the source revision. It does not retry or create authority from proof payload; an output failure after the database call does not roll back an accepted repair.
// repairLegacyProvenance передаёт точные поля доказательства функции владельца в БД, требует её признак принятия и кодирует PASS с исходной ревизией в JSON. Повтора и создания полномочий из payload нет; ошибка вывода после вызова БД не откатывает принятое восстановление.
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
