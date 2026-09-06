package entity

// ProviderAuthorizationReservation связывает внешний эффект с заранее сохранённой попыткой владельца.
type ProviderAuthorizationReservation struct {
	AttemptRef      string
	ReservedVersion int64
	Applied         bool
}
