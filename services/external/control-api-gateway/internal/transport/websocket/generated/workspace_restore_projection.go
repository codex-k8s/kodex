
package generated

type WorkspaceRestoreProjection struct {
  BackupRef string
  BackupVersion int
  MembershipSha256 string
  MemberCount int
  State *WorkspaceRestoreState
  Attempt int
  Generation int
  Partial bool
  TerminalReasonCode string
}