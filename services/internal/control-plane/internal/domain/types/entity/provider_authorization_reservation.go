package entity

// ProviderAuthorizationReservation binds a prospective external authorization effect to an already
// persisted owner attempt and reserved version. Applied records reservation application rather than
// successful provider authorization or permission to repeat the effect.
// ProviderAuthorizationReservation связывает предполагаемый внешний эффект авторизации с заранее
// сохранённой попыткой владельца и зарезервированной версией. Applied фиксирует применение
// резервирования, а не успешную авторизацию провайдера или разрешение повторить эффект.
type ProviderAuthorizationReservation struct {
	AttemptRef      string
	ReservedVersion int64
	Applied         bool
}
