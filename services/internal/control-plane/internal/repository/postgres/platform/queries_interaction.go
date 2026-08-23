package platform

import _ "embed"

var (
	//go:embed sql/interaction_list_sources.sql
	queryInteractionListSources string
	//go:embed sql/interaction_claim_deliveries.sql
	queryInteractionClaimDeliveries string
	//go:embed sql/interaction_complete_delivery_resolve.sql
	queryInteractionCompleteDeliveryResolve string
	//go:embed sql/interaction_complete_delivery_update.sql
	queryInteractionCompleteDeliveryUpdate string
	//go:embed sql/interaction_find_gate_delivery.sql
	queryInteractionFindGateDelivery string
	//go:embed sql/interaction_list_inbound_grants.sql
	queryInteractionListInboundGrants string
	//go:embed sql/interaction_find_message_receipt.sql
	queryInteractionFindMessageReceipt string
	//go:embed sql/interaction_insert_message_receipt.sql
	queryInteractionInsertMessageReceipt string
	//go:embed sql/interaction_resolve_connection.sql
	queryInteractionResolveConnection string
	//go:embed sql/interaction_enqueue_gate_deliveries.sql
	queryInteractionEnqueueGateDeliveries string
	//go:embed sql/interaction_enqueue_terminal_deliveries.sql
	queryInteractionEnqueueTerminalDeliveries string
	//go:embed sql/interaction_cancel_pending_gate_deliveries.sql
	queryInteractionCancelPendingGateDeliveries string
	//go:embed sql/interaction_list_failed_incidents.sql
	queryInteractionListFailedIncidents string
	//go:embed sql/interaction_list_run_incidents.sql
	queryInteractionListRunIncidents string
	//go:embed sql/interaction_count_active_adapters.sql
	queryInteractionCountActiveAdapters string
)
