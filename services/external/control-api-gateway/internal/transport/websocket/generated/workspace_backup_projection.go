
package generated

type WorkspaceBackupProjection struct {
  Scope *WorkspaceBackupScope
  MemberCount int
  MembershipSha256 string
  State *WorkspaceBackupState
  Attempt int
  Generation int
  TerminalReasonCode string
  RetainUntil string
}