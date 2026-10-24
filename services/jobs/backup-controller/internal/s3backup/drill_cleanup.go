package s3backup

import (
	"context"
	"errors"
	"path"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

// TargetInspectionMode separates proof of complete restored content from explicit cleanup and its partial-outcome readback.
// TargetInspectionMode отделяет доказательство полного восстановленного набора от явного cleanup и чтения его частичного исхода.
type TargetInspectionMode string

const (
	// disposableTargetError names the shared closed refusal for any target outside the current fixture contract.
	// disposableTargetError задаёт общий закрытый отказ для цели вне действующего fixture-контракта.
	disposableTargetError = "cleanup target is not the exact disposable fixture"
	// TargetComplete requires every expected restored version to remain present.
	// TargetComplete требует наличия каждой ожидаемой восстановленной версии.
	TargetComplete TargetInspectionMode = "COMPLETE_TARGET"
	// TargetCleanup permits only receipt-owned remaining versions to be deleted.
	// TargetCleanup разрешает удаление только оставшихся версий собственной квитанции.
	TargetCleanup TargetInspectionMode = "CLEANUP"
	// TargetCleanupReadback measures a partial cleanup or authoritative absence without claiming restored-content completeness.
	// TargetCleanupReadback измеряет частичный cleanup либо отсутствие, не подтверждая полноту восстановленных данных.
	TargetCleanupReadback TargetInspectionMode = "CLEANUP_READBACK"
)

// CheckEmptyDrillTarget validates the fixed disposable destination and completes bounded empty-prefix readback before restore writes; it never deletes existing objects.
// CheckEmptyDrillTarget проверяет фиксированную disposable-цель и ограниченно подтверждает пустой prefix до restore; существующие объекты не удаляются.
func (client *Client) CheckEmptyDrillTarget(ctx context.Context) error {
	if client.config.Bucket != "kodex-restore-fixture" || client.config.Endpoint != "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333" || !regexp.MustCompile(`^e2e-restore-[a-z0-9-]{1,32}$`).MatchString(client.config.Prefix) {
		return errors.New(disposableTargetError)
	}
	values, err := client.boundedLatest(ctx, client.config.Prefix+"/")
	if err != nil {
		return err
	}
	if len(values) != 0 {
		return errors.New("restore S3 target is not empty")
	}
	return nil
}

// InspectDrillTarget reads or deletes only receipt-owned versions in the fixed disposable bucket/prefix. It validates the complete bounded current set before its first delete; foreign/new versions fail closed. Uncertain deletes are never retried and every successful cleanup requires empty authoritative readback.
// InspectDrillTarget читает либо удаляет только версии из квитанции в фиксированном disposable bucket/prefix. Полный ограниченный набор проверяется до первого удаления; чужие версии закрыто отклоняются. Неизвестное удаление не повторяется, успех требует пустого readback.
func (client *Client) InspectDrillTarget(ctx context.Context, drill manifest.RestoreDrill, mode TargetInspectionMode) (int, error) {
	if mode != TargetComplete && mode != TargetCleanup && mode != TargetCleanupReadback {
		return 0, errors.New("target inspection mode is invalid")
	}
	if client.config.Bucket != "kodex-restore-fixture" || client.config.Endpoint != "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333" ||
		!regexp.MustCompile(`^e2e-restore-[a-z0-9-]{1,32}$`).MatchString(client.config.Prefix) {
		return 0, errors.New(disposableTargetError)
	}
	prefix := client.config.Prefix + "/"
	expected := make(map[string]manifest.Receipt, len(drill.Objects))
	for _, object := range drill.Objects {
		if object.Bucket != client.config.Bucket || !strings.HasPrefix(object.Key, prefix) || path.Clean(object.Key) != object.Key ||
			object.VersionID == "" || object.ETag == "" || !validDigest(object.ChecksumSHA256) || object.SizeBytes < 0 || expected[object.Key].Key != "" {
			return 0, errors.New("cleanup receipt ownership is invalid")
		}
		expected[object.Key] = object
	}
	current, err := client.boundedLatest(ctx, prefix)
	if err != nil {
		return 0, err
	}
	if mode == TargetComplete && len(current) != len(expected) {
		return 0, errors.New("restored target inventory is incomplete")
	}
	for _, object := range current {
		owner, exists := expected[object.Key]
		if !exists || owner.VersionID != object.VersionID {
			return 0, errors.New("cleanup target contains foreign versions")
		}
		actual, err := client.head(ctx, owner.Key, owner.VersionID)
		if err != nil || actual != owner {
			return 0, errors.New("cleanup target receipt readback mismatches")
		}
	}
	if mode != TargetCleanup {
		return len(current), nil
	}
	for _, object := range current {
		_, err := client.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(object.Bucket), Key: aws.String(object.Key), VersionId: aws.String(object.VersionID), IfMatch: aws.String(object.ETag)})
		if err != nil {
			return 0, errors.New("cleanup delete outcome is unknown; readback required")
		}
	}
	remaining, err := client.boundedLatest(ctx, prefix)
	if err != nil || len(remaining) != 0 {
		return 0, errors.New("cleanup target absence readback failed")
	}
	return 0, nil
}
