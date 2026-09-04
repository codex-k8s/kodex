package platform

import _ "embed"

var (
	//go:embed sql/managed_configuration_insert_set.sql
	queryManagedConfigurationInsertSet string
	//go:embed sql/managed_configuration_lock_set.sql
	queryManagedConfigurationLockSet string
	//go:embed sql/managed_configuration_insert_revision.sql
	queryManagedConfigurationInsertRevision string
	//go:embed sql/managed_configuration_touch_set.sql
	queryManagedConfigurationTouchSet string
	//go:embed sql/managed_configuration_lock_revision.sql
	queryManagedConfigurationLockRevision string
	//go:embed sql/managed_configuration_validate_revision.sql
	queryManagedConfigurationValidateRevision string
	//go:embed sql/managed_configuration_publish_revision.sql
	queryManagedConfigurationPublishRevision string
	//go:embed sql/managed_configuration_validate_consumer.sql
	queryManagedConfigurationValidateConsumer string
	//go:embed sql/managed_configuration_rebind_consumer.sql
	queryManagedConfigurationRebindConsumer string
	//go:embed sql/managed_configuration_list_history.sql
	queryManagedConfigurationListHistory string
	//go:embed sql/managed_configuration_list_bindings.sql
	queryManagedConfigurationListBindings string
	//go:embed sql/managed_configuration_detach.sql
	queryManagedConfigurationDetach string
	//go:embed sql/managed_configuration_copy.sql
	queryManagedConfigurationCopy string
	//go:embed sql/managed_configuration_get_stt.sql
	queryManagedConfigurationGetSTT string
	//go:embed sql/managed_configuration_access_target.sql
	queryManagedConfigurationAccessTarget string
	//go:embed sql/managed_configuration_effective_prompt.sql
	queryManagedConfigurationEffectivePrompt string
	//go:embed sql/managed_configuration_get_consumer_binding.sql
	queryManagedConfigurationGetConsumerBinding string
)
