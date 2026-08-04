
package generated

type ScheduleProjection struct {
  TargetResourceId string
  TargetKind *ResourceKind
  TargetVersion int
  Cron string
  Timezone string
  NextRunAt string
  MaximumAttempts int
  Ownership *ConfigurationOwnershipProjection
}