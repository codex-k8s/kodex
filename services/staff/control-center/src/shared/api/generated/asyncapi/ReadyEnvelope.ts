
interface ReadyEnvelope {
  reservedType: 'STREAM_READY';
  requestRef: string;
  runRef: string;
  latestSequence: number;
}
export { ReadyEnvelope };