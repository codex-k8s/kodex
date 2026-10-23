// Package cache supplies bounded cache-first reads and invalidation without
// granting business authority or defining provider-specific key formats.
// A service-owned codec must validate the scope and provenance of each hit.
// RU: Пакет cache предоставляет ограниченное чтение через кэш и удаление ключа,
// не назначая бизнес-полномочия и формат ключей провайдера. При попадании
// принадлежащий сервису кодек обязан проверить область и происхождение снимка.
package cache
