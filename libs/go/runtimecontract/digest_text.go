package runtimecontract

// ValidSHA256Text accepts exactly 64 unprefixed lowercase hexadecimal bytes using the shared runtime-binding grammar.
// It preserves zero digests, performs no normalization, hashing or I/O and does not verify content, provenance, signatures or authority.
// ValidSHA256Text принимает ровно 64 строчных шестнадцатеричных байта без префикса по общей грамматике привязок выполнения.
// Нулевой дайджест допустим; нормализация, хеширование и I/O не выполняются, содержимое, происхождение, подписи и полномочия не проверяются.
func ValidSHA256Text(value string) bool {
	return sha256Pattern.MatchString(value)
}
