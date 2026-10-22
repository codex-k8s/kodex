package policydeclaration

// RequestProfile records policy applicability of the request-binding mode and
// resource/version/attempt/idempotency dimensions, not values observed from an RPC.
// It supplies no defaults, authority or validation; profile owners enforce them.
// RequestProfile хранит заданную политикой применимость режима привязки и
// измерений ресурса, версии, попытки и идемпотентности, а не значения из RPC.
// Тип не задаёт умолчаний, полномочий или проверки; их обеспечивают владельцы профиля.
type RequestProfile struct {
	Mode        string `json:"mode"`
	Resource    string `json:"resource"`
	Version     string `json:"version"`
	Attempt     string `json:"attempt"`
	Idempotency string `json:"idempotency"`
}

// ContinuationProfile names the exact parent operation and full RPC method for
// a child binding. These fields do not prove accepted lineage or authorize
// delegation; consumers retain their existing edge and parent-context checks.
// ContinuationProfile называет точные родительские операцию и полный метод RPC
// дочерней привязки. Поля не доказывают принятое происхождение и не разрешают
// делегирование; потребители сохраняют проверки ребра и родительского контекста.
type ContinuationProfile struct {
	ParentOperationID string `json:"parent_operation_id"`
	ParentFullMethod  string `json:"parent_full_method"`
}

// LocalPeer carries configured UID, primary GID and shared filesystem GID pins
// for a local transport boundary. Values are not measured peer credentials and
// must not replace the consumer's transport observation and identity checks.
// LocalPeer содержит заданные UID, основной GID и общий файловый GID для
// границы локального транспорта. Это не измеренные учётные данные стороны;
// они не заменяют наблюдение транспорта и проверки личности у потребителя.
type LocalPeer struct {
	UID         uint32 `json:"uid"`
	PrimaryGID  uint32 `json:"primary_gid"`
	SharedFSGID uint32 `json:"shared_fs_gid"`
}
