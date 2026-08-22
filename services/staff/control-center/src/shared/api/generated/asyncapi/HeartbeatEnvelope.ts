
interface HeartbeatEnvelope {
  reservedType: 'HEARTBEAT';
  serverTime: string;
  latestSequence: number;
}
export { HeartbeatEnvelope };