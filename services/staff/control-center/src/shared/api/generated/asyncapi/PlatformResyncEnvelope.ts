
interface PlatformResyncEnvelope {
  reservedType: 'PLATFORM_RESYNC_REQUIRED';
  requestRef: string;
  currentSequence: number;
  reason: 'AUTHORITATIVE_READ_REQUIRED';
}
export { PlatformResyncEnvelope };