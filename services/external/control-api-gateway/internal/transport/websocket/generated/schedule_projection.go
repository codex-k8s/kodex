
package generated

type ScheduleProjection struct {
  TargetResourceId string
  TargetKind *ResourceKind
  TargetVersion int
  Cron string
  IntervalSeconds int
  Timezone string
  NextRunAt string
  Calendar *ScheduleCalendar
  OverlapPolicy *ScheduleOverlapPolicy
  MisfirePolicy *ScheduleMisfirePolicy
  MisfireGraceSeconds int
  DeliveryPolicy *ScheduleDeliveryPolicy
  MaximumAttempts int
  InitialBackoffSeconds int
  MaximumBackoffSeconds int
  DeadLetterAfterSeconds int
  PromptProfileId string
  PromptRevision int
  SessionPolicy *ScheduleSessionPolicy
  RoomId string
  NotificationPolicy *ScheduleNotificationPolicy
  MaximumExecutionSeconds int
  Coalesce bool
  RuntimeRevisionId string
  TargetType *ScheduleTargetType
  PlaybookRef string
  PlaybookVersion int
  PromptArtifactId string
  ExecutionSessionId string
  Ownership *ConfigurationOwnershipProjection
}