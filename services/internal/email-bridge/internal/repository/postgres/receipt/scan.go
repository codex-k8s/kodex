package receipt

import (
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/jackc/pgx/v5"
)

// scanRecord maps the shared explicit receipt/audit column order used by reserve and get queries, preserving the scanner error and partial value for caller handling.
// scanRecord преобразует общий явный порядок колонок квитанции и audit в reserve/get, сохраняя ошибку scanner и частичное значение для обработки вызывающим.
func scanRecord(row pgx.Row) (port.Record, error) {
	var result port.Record
	err := row.Scan(&result.ID, &result.Key, &result.Digest, &result.Status, &result.Resource, &result.UID, &result.UIDValidity, &result.Folder, &result.ContentDigest, &result.Audit.Actor, &result.Audit.Agent, &result.Audit.Grant, &result.Audit.Operation, &result.Audit.ConfigurationRevision, &result.Audit.CredentialGeneration, &result.Audit.GateApproved, &result.ReportVersion)
	return result, err
}
