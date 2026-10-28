package s3backup

import (
	"net/http"
	"testing"
)

// TestExistsRejectsUnavailableRestoreMarkers keeps an inventoried terminal marker authoritative when HEAD fails, without performing any write.
// RU: TestExistsRejectsUnavailableRestoreMarkers сохраняет защиту найденной терминальной метки при ошибке HEAD без попыток записи.
func TestExistsRejectsUnavailableRestoreMarkers(t *testing.T) {
	for _, key := range []string{"restores/attempt/failure.json", "backups/backup/restore-drills/attempt.json"} {
		for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable} {
			repository, fake := testRepository(t)
			if _, err := repository.PutJSON(t.Context(), key, map[string]string{"state": "terminal"}); err != nil {
				t.Fatal(err)
			}
			guard := &inspectionNoWrites{fakeS3: fake, headStatus: status}
			repository.destination.api = guard
			if exists, err := repository.Exists(t.Context(), key); err == nil || exists {
				t.Fatalf("unavailable terminal marker became absence: status=%d", status)
			}
			if guard.writes != 0 || len(fake.objects) != 1 {
				t.Fatal("existence check changed terminal state")
			}
			repository.destination.api = fake
			if exists, err := repository.Exists(t.Context(), key); err != nil || !exists {
				t.Fatal("terminal marker did not survive metadata failure")
			}
		}
	}
}

// TestExistsRequiresExactInventoryMatch accepts confirmed absence without treating a longer prefix sibling as the requested marker.
// RU: TestExistsRequiresExactInventoryMatch допускает подтверждённое отсутствие, не принимая соседний ключ с общим префиксом за запрошенную метку.
func TestExistsRequiresExactInventoryMatch(t *testing.T) {
	repository, _ := testRepository(t)
	const key = "restores/attempt/failure.json"
	if _, err := repository.PutJSON(t.Context(), key+".other", map[string]string{"state": "terminal"}); err != nil {
		t.Fatal(err)
	}
	if exists, err := repository.Exists(t.Context(), key); err != nil || exists {
		t.Fatal("inventory prefix sibling became exact marker")
	}
}
