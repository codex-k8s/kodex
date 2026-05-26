package service

const (
	operationCreateFlow                  = "domain.Service.CreateFlow"
	operationUpdateFlow                  = "domain.Service.UpdateFlow"
	operationCreateFlowVersion           = "domain.Service.CreateFlowVersion"
	operationActivateFlowVersion         = "domain.Service.ActivateFlowVersion"
	operationCreateRoleProfile           = "domain.Service.CreateRoleProfile"
	operationUpdateRoleProfile           = "domain.Service.UpdateRoleProfile"
	operationCreatePromptTemplate        = "domain.Service.CreatePromptTemplate"
	operationCreatePromptTemplateVersion = "domain.Service.CreatePromptTemplateVersion"
	operationActivatePromptVersion       = "domain.Service.ActivatePromptTemplateVersion"
	operationStartAgentSession           = "domain.Service.StartAgentSession"
	operationStartAgentRun               = "domain.Service.StartAgentRun"
	operationRecordRunState              = "domain.Service.RecordRunState"
	operationRecordSessionSnapshot       = "domain.Service.RecordSessionStateSnapshot"
	operationRequestAcceptance           = "domain.Service.RequestAcceptance"
	operationRecordAcceptanceResult      = "domain.Service.RecordAcceptanceResult"
	operationCreateFollowUpIntent        = "domain.Service.CreateFollowUpIntent"
	operationRecordAgentActivity         = "domain.Service.RecordAgentActivity"
)
