package integrationpackage

import (
	"bytes"
	"encoding/json"
	"io"
)

// GitHubDiscoveredRepository contains safe provider observations for a self-service selection; write rights remain unconfirmed until an actual authorized call.
// GitHubDiscoveredRepository содержит безопасные наблюдения провайдера для собственного выбора; права записи остаются неподтверждёнными до авторизованного вызова.
type GitHubDiscoveredRepository struct {
	ID        int64  `json:"id"`
	FullName  string `json:"full_name"`
	OwnerKind string `json:"owner_kind"`
	Private   bool   `json:"private"`
	Archived  bool   `json:"archived"`
	CanRead   bool   `json:"can_read"`
	CanPush   bool   `json:"can_push"`
}

// GitHubDiscoveryPage is one bounded authenticated page, not a credential, provider payload or reusable resource grant.
// GitHubDiscoveryPage — одна ограниченная аутентифицированная страница, а не credential, provider payload либо многоразовый resource grant.
type GitHubDiscoveryPage struct {
	IdentityID int64                        `json:"identity_id"`
	Login      string                       `json:"login"`
	Page       int                          `json:"page"`
	NextPage   int                          `json:"next_page"`
	Items      []GitHubDiscoveredRepository `json:"items"`
}

// Valid rejects missing identity, malformed coordinates, duplicate repositories and noncontiguous page cursors before the safe page reaches storage or UI.
// Valid отклоняет отсутствующую identity, неверные координаты, дубликаты репозиториев и несмежные курсоры до хранения либо выдачи безопасной страницы.
func (page GitHubDiscoveryPage) Valid() bool {
	if page.IdentityID < 1 || page.IdentityID > 9007199254740991 || !ValidGitHubRepositoryName(page.Login+"/identity") || page.Page < 1 || page.Page > 10000 || page.NextPage != 0 && (page.NextPage != page.Page+1 || page.NextPage > 10000) || page.Items == nil || len(page.Items) > 100 {
		return false
	}
	ids := map[int64]bool{}
	names := map[string]bool{}
	for _, repo := range page.Items {
		if repo.ID < 1 || repo.ID > 9007199254740991 || !ValidGitHubRepositoryName(repo.FullName) || repo.OwnerKind != "User" && repo.OwnerKind != "Organization" || !repo.CanRead || ids[repo.ID] || names[repo.FullName] {
			return false
		}
		ids[repo.ID] = true
		names[repo.FullName] = true
	}
	return true
}

// ParseGitHubDiscoveryPage closes on unknown fields, excess bytes or trailing values; null is the sole empty stored representation.
// ParseGitHubDiscoveryPage закрывается при неизвестных полях, превышении объёма либо дополнительных значениях; null — единственное пустое хранимое представление.
func ParseGitHubDiscoveryPage(raw []byte) (*GitHubDiscoveryPage, error) {
	if bytes.Equal(raw, []byte("null")) || len(raw) == 0 {
		return nil, nil
	}
	var page GitHubDiscoveryPage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 65536 || decoder.Decode(&page) != nil || decoder.Decode(new(any)) != io.EOF || !page.Valid() {
		return nil, errGitHubResourceScope
	}
	return &page, nil
}
