
interface PlatformHeartbeatEnvelope {
  reservedType: 'PLATFORM_HEARTBEAT';
  serverTime: string;
  latestSequence: number;
}
export { PlatformHeartbeatEnvelope };