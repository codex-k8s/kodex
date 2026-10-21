// Package transportprofile содержит wire-имена явно выбираемых RPC-профилей.
// Пакет не загружает ключи и не связывает plaintext client с JWS-реализацией.
package transportprofile

// TrustedCluster names the explicitly selected internal RPC transport profile; it does not load credentials or authorize a caller by itself.
// TrustedCluster обозначает явно выбранный транспортный профиль внутреннего RPC; сама константа не загружает учётные данные и не разрешает вызов.
const TrustedCluster = "trusted-cluster"
