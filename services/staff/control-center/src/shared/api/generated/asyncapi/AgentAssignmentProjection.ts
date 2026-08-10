
interface AgentAssignmentProjection {
  agentRef: string;
  workspaceRef: string;
  roomRef?: string;
  generation: number;
}
export { AgentAssignmentProjection };