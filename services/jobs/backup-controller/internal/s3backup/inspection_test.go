package s3backup

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// inspectionPages overrides only inventory pages while retaining the exact fake object store for readback.
// inspectionPages заменяет только страницы inventory, сохраняя точное поддельное хранилище для readback.
type inspectionPages struct {
	*fakeS3
	pages []*s3.ListObjectVersionsOutput
	calls int
}

// ListObjectVersions supplies deterministic pagination, repeating the final page to exercise bounded loop failures.
// ListObjectVersions выдаёт предсказуемые страницы и повторяет последнюю для проверки ограниченного отказа цикла.
func (fake *inspectionPages) ListObjectVersions(_ context.Context, input *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	if aws.ToInt32(input.MaxKeys) != 512 {
		panic("bounded page size is absent")
	}
	index := fake.calls
	fake.calls++
	if index >= len(fake.pages) {
		index = len(fake.pages) - 1
	}
	return fake.pages[index], nil
}

// TestInspectionPaginationRejectsPartialOrForeignInventory covers cursor loops, missing markers, nonlatest/duplicate/out-of-prefix keys and successful exact EOF.
// TestInspectionPaginationRejectsPartialOrForeignInventory проверяет цикл, неполные markers, старые/чужие/повторные ключи и успешный точный EOF.
func TestInspectionPaginationRejectsPartialOrForeignInventory(t *testing.T) {
	for _, defect := range []string{"", "loop", "missing-marker", "foreign", "nonlatest", "duplicate", "delete-marker", "oversized"} {
		t.Run(defect, func(t *testing.T) {
			repository, fake := testRepository(t)
			version := types.ObjectVersion{Key: aws.String("kodex/backups/attempt/attempt.json"), VersionId: aws.String("v1"), IsLatest: aws.Bool(true), ETag: aws.String("etag"), Size: aws.Int64(1)}
			page := &s3.ListObjectVersionsOutput{Versions: []types.ObjectVersion{version}}
			switch defect {
			case "loop":
				page.IsTruncated = aws.Bool(true)
				page.NextKeyMarker = aws.String("next")
				page.NextVersionIdMarker = aws.String("v2")
			case "missing-marker":
				page.IsTruncated = aws.Bool(true)
			case "foreign":
				page.Versions[0].Key = aws.String("foreign/backups/item")
			case "nonlatest":
				page.Versions[0].IsLatest = aws.Bool(false)
			case "duplicate":
				page.Versions = append(page.Versions, version)
			case "delete-marker":
				page.DeleteMarkers = []types.DeleteMarkerEntry{{Key: version.Key}}
			case "oversized":
				for len(page.Versions) < 513 {
					page.Versions = append(page.Versions, version)
				}
			}
			pages := &inspectionPages{fakeS3: fake, pages: []*s3.ListObjectVersionsOutput{page}}
			repository.destination.api = pages
			values, err := repository.ListVerified(context.Background())
			if defect == "" {
				if err != nil || len(values) != 0 {
					t.Fatal("exact empty verified EOF failed", err)
				}
			} else if err == nil || values != nil {
				t.Fatal("partial inventory accepted")
			}
			if pages.calls > 8 {
				t.Fatal("unbounded page loop")
			}
		})
	}
}
