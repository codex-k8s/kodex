// Package query contains runtime-manager read filters and paging helpers.
package query

// PageRequest is a stable repository paging contract.
type PageRequest struct {
	Limit int32
	Token string
}

// PageResult describes list continuation state.
type PageResult struct {
	NextToken string
}
