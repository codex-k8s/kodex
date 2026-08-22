package platform

import _ "embed"

var (
	//go:embed sql/outbox_checkoutbox_1.sql
	queryOutboxCheckoutbox1 string
	//go:embed sql/outbox_claimoutbox_1.sql
	queryOutboxClaimoutbox1 string
	//go:embed sql/outbox_markoutboxpublished_1.sql
	queryOutboxMarkoutboxpublished1 string
	//go:embed sql/outbox_markoutboxfailed_1.sql
	queryOutboxMarkoutboxfailed1 string
)
