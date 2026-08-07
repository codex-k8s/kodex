
interface SessionProjection {
  agentId: string;
  providerAccountBindingId: string;
  conversationId?: string;
  archiveRef?: string;
  lastTurnSequence: number;
}
export { SessionProjection };