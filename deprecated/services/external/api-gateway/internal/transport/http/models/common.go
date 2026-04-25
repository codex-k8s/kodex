package models

// ItemsResponse is a standard JSON envelope for list endpoints.
type ItemsResponse[T any] struct {
	Items []T `json:"items"`
}

// Pagination describes one server-side list page and total size.
type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
}
