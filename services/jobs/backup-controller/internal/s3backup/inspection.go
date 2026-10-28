package s3backup

import (
	"context"
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

// inspectionBackupID restricts metadata locators to canonical immutable backup IDs.
// inspectionBackupID ограничивает адреса метаданных каноническими идентификаторами копий.
var inspectionBackupID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}$`)

// ListVerified reads at most eight bounded inventory pages and sixteen verified manifests in this exact repository prefix. Repeated cursors, changed keys, nonlatest versions or budget exhaustion fail without partial output; it never repairs or mutates storage.
// ListVerified читает не более восьми ограниченных страниц и шестнадцати подтверждённых манифестов точного префикса. Повтор курсора, неверный ключ, старая версия и исчерпание бюджета закрывают весь результат; записи и исправления не выполняются.
func (repository *Repository) ListVerified(ctx context.Context) ([]manifest.Manifest, error) {
	prefix := repository.key("backups") + "/"
	objects, err := repository.destination.boundedLatest(ctx, prefix)
	if err != nil {
		return nil, err
	}
	keys, ids := map[string]bool{}, map[string]bool{}
	for _, object := range objects {
		keys[object.Key] = true
		parts := strings.Split(strings.TrimPrefix(object.Key, prefix), "/")
		if len(parts) == 2 && parts[1] == "manifest.json" {
			if !inspectionBackupID.MatchString(parts[0]) || len(ids) >= 16 {
				return nil, errors.New("backup metadata catalog exceeds bounds")
			}
			ids[parts[0]] = true
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	result := make([]manifest.Manifest, 0, len(ordered))
	for _, id := range ordered {
		if !keys[repository.key(path.Join("backups", id, "verification.json"))] {
			continue
		}
		value, _, err := repository.LoadVerifiedManifest(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

// boundedLatest completes an exact prefix inventory within eight pages of 512 versions, rejecting repeated cursors, duplicate/nonlatest keys and delete markers before returning any result.
// boundedLatest полностью читает точный префикс за восемь страниц по 512 версий; повтор курсора, дубли, старые версии и delete markers закрывают весь результат.
func (client *Client) boundedLatest(ctx context.Context, prefix string) ([]manifest.Receipt, error) {
	input := &s3.ListObjectVersionsInput{Bucket: aws.String(client.config.Bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(512)}
	keys, cursors := map[string]bool{}, map[string]bool{}
	result := make([]manifest.Receipt, 0)
	for page := 0; page < 8; page++ {
		output, err := client.api.ListObjectVersions(ctx, input)
		if err != nil || output == nil || len(output.DeleteMarkers) != 0 || len(output.Versions) > 512 {
			return nil, errors.New("backup metadata inventory is unavailable")
		}
		for _, version := range output.Versions {
			key := aws.ToString(version.Key)
			if !strings.HasPrefix(key, prefix) || keys[key] || !aws.ToBool(version.IsLatest) || aws.ToString(version.VersionId) == "" {
				return nil, errors.New("backup metadata inventory is inconsistent")
			}
			keys[key] = true
			result = append(result, manifest.Receipt{Bucket: client.config.Bucket, Key: key, VersionID: aws.ToString(version.VersionId), ETag: strings.Trim(aws.ToString(version.ETag), "\""), SizeBytes: aws.ToInt64(version.Size)})
		}
		if !aws.ToBool(output.IsTruncated) {
			return result, nil
		}
		key, version := aws.ToString(output.NextKeyMarker), aws.ToString(output.NextVersionIdMarker)
		cursor := key + "\x00" + version
		if key == "" || version == "" || cursors[cursor] {
			return nil, errors.New("backup metadata cursor is invalid")
		}
		cursors[cursor] = true
		input.KeyMarker, input.VersionIdMarker = output.NextKeyMarker, output.NextVersionIdMarker
	}
	return nil, errors.New("backup metadata pagination budget exhausted")
}
