SELECT pg_advisory_xact_lock(hashtextextended(@operation || ':' || @key_sha256, 0))
