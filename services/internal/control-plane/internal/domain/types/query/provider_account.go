package query

// ProviderAccountBlockers selects a paged set of usage blockers for a provider
// account; the filter fields do not authorize access to another account.
// ProviderAccountBlockers выбирает страницу блокирующих использований учётной
// записи провайдера; поля фильтра не разрешают доступ к чужой записи.
type ProviderAccountBlockers struct {
	AccountRef, Kind, Query string
	Page                    Page
}
