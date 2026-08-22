
interface PlatformReadyEnvelope {
  reservedType: 'PLATFORM_STREAM_READY';
  requestRef: string;
  latestSequence: number;
}
export { PlatformReadyEnvelope };