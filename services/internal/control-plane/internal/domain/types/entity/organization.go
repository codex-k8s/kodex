package entity

// Organization is a tenancy container for users, groups and projects.
type Organization struct {
	ID   string
	Slug string
	Name string
}
