package command

const (
	PrepareRoleImageGitWriteBack             Kind = "PREPARE_ROLE_IMAGE_GIT_WRITE_BACK"
	PrepareIntegrationDefinitionGitWriteBack Kind = "PREPARE_INTEGRATION_DEFINITION_GIT_WRITE_BACK"
	ApproveManagedConfigurationGitWriteBack  Kind = "APPROVE_MANAGED_CONFIGURATION_GIT_WRITE_BACK"
	RejectManagedConfigurationGitWriteBack   Kind = "REJECT_MANAGED_CONFIGURATION_GIT_WRITE_BACK"
	CancelManagedConfigurationGitWriteBack   Kind = "CANCEL_MANAGED_CONFIGURATION_GIT_WRITE_BACK"
)

type ConfigurationWriteBackInput struct {
	ConfigurationRef, ProposalRef, Content, ApprovalDigest string
	ExpectedSourceVersion                                  int64
}
