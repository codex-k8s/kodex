// Package secretschema owns the authority unit's shared application field lists
// for API and projected Secret delivery. Each call returns independent storage;
// resource names, mount paths, generations and authorization remain caller-owned.
// Пакет secretschema владеет общими списками прикладных полей unit authority для
// доставки через API и projected Secret. Каждый вызов возвращает отдельную память;
// имена ресурсов, mount paths, поколения и авторизация остаются у вызывающих.
package secretschema

// ReadbackCredentialKeys returns the exact ordered credential/intent/digest/
// expiry field names. It excludes the provider's generation marker and neither
// validates values nor shares a mutable slice with another call.
// ReadbackCredentialKeys возвращает точные упорядоченные имена полей credential,
// намерения, дайджестов и срока. Маркер поколения провайдера не включён; значения
// не проверяются и изменяемый срез не разделяется с другим вызовом.
func ReadbackCredentialKeys() []string {
	return []string{
		"pinned_intent_id", "readback_credential_compact_jws",
		"readback_credential_jti", "readback_credential_digest_sha256",
		"intent_digest_sha256", "expires_at",
	}
}

// ReadbackPossessionKeys returns independent storage for the private-JWK,
// key-ID, generation and thumbprint field names, not key material or an access grant.
// ReadbackPossessionKeys возвращает отдельную память имён полей закрытого JWK,
// ID ключа, поколения и отпечатка, а не ключевой материал или право доступа.
func ReadbackPossessionKeys() []string {
	return []string{
		"possession_private_jwk", "possession_key_kid",
		"possession_key_generation", "possession_key_thumbprint_sha256",
	}
}

// RestoreCredentialKeys returns an independent ordered restore-role delivery
// field list preserving semantic/directive, credential, receipt and issue-time bindings.
// RestoreCredentialKeys возвращает отдельный упорядоченный список полей доставки
// restore-role с прежними привязками семантики, директивы, credential, квитанции и времени.
func RestoreCredentialKeys() []string {
	return []string{
		"semantic_digest_sha256", "issuance_directive_jti",
		"role_credential_compact_jws", "role_credential_digest_sha256",
		"delivery_receipt_jti", "issued_at",
	}
}

// RestoreACKKeys returns independent storage for the exact ordered ACK private
// material and role/directive/receipt/time field names; it does not sign an ACK.
// RestoreACKKeys возвращает отдельную память точных упорядоченных имён полей
// закрытого ACK material, роли, директивы, квитанции и времени; ACK не подписывается.
func RestoreACKKeys() []string {
	return []string{
		"semantic_digest_sha256", "issuance_directive_jti", "ack_private_jwk",
		"ack_key_kid", "ack_key_thumbprint_sha256", "role_credential_jti",
		"delivery_receipt_jti", "issued_at",
	}
}
