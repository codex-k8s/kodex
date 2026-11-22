// Только owner-assigned incarnation и approved slot; legacy slot/EOF wire не принимается.
export interface PortForwardCoordinates {
  connectionRef: string;
  generation: number;
  slot: number;
}
export const portForwardHeaderBytes = 51;
export function validPortForwardCoordinates(
  value: PortForwardCoordinates,
): boolean {
  return (
    /^kpf_[A-Za-z0-9_-]{24}$/u.test(value.connectionRef) &&
    Number.isSafeInteger(value.generation) &&
    value.generation >= 1 &&
    Number.isInteger(value.slot) &&
    value.slot >= 0 &&
    value.slot < 16
  );
}
// Общий разбор header не выдаёт authority; направление и approved slot проверяет consumer.
function portForwardHeader(bytes: Uint8Array, slots: number) {
  if (
    !Number.isInteger(slots) ||
    slots < 1 ||
    slots > 16 ||
    bytes.length < portForwardHeaderBytes ||
    bytes.length > 32768 ||
    bytes[0] !== 75 ||
    bytes[1] !== 80 ||
    bytes[2] !== 70 ||
    bytes[3] !== 1 ||
    bytes[14] !== 28
  )
    return undefined;
  const slot = bytes[5],
    action = bytes[4];
  if (
    slot === undefined ||
    slot >= slots ||
    action === undefined ||
    ![1, 2, 3, 4].includes(action)
  )
    return undefined;
  const header = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength),
    generation = header.getBigUint64(6),
    nativeOrdinal = header.getBigUint64(43),
    connectionRef = String.fromCharCode(...bytes.subarray(15, 43));
  if (
    !/^kpf_[A-Za-z0-9_-]{24}$/u.test(connectionRef) ||
    generation < 1n ||
    generation > 9223372036854775807n ||
    nativeOrdinal > 9223372036854775807n ||
    (action === 2
      ? bytes.length === portForwardHeaderBytes
      : bytes.length !== portForwardHeaderBytes)
  )
    return undefined;
  return { connectionRef, generation, slot, action, nativeOrdinal };
}
export function validPortForwardInput(
  bytes: Uint8Array,
  eof: boolean,
): boolean {
  if (eof) return false;
  const header = portForwardHeader(bytes, 16);
  return (
    header !== undefined &&
    header.generation <= BigInt(Number.MAX_SAFE_INTEGER) &&
    header.nativeOrdinal === 0n &&
    (header.action === 2 || header.action === 3)
  );
}

export interface PortForwardOutputFrame {
  connectionRef: string;
  generation: bigint;
  slot: number;
  nativeOrdinal: bigint;
  action: "DATA" | "HALF_CLOSE" | "CLOSE";
  // Копия принадлежит consumer: bytes очищаются после accepted socket write либо closed failure.
  body: Uint8Array;
}
const invalidOutputFrame = "Kubernetes port-forward output frame is invalid";
// OUTPUT decoder сохраняет full bigint coordinates и не заимствует private body.
// Default JSON/inspection не перечисляют TCP bytes; decoding не подтверждает delivery либо remote success.
export function decodePortForwardOutput(
  bytes: Uint8Array,
  slots = 16,
): PortForwardOutputFrame {
  const header = portForwardHeader(bytes, slots);
  if (!header || header.nativeOrdinal === 0n || header.action === 1)
    throw new Error(invalidOutputFrame);
  const result: PortForwardOutputFrame = {
    connectionRef: header.connectionRef,
    generation: header.generation,
    slot: header.slot,
    nativeOrdinal: header.nativeOrdinal,
    action:
      header.action === 2
        ? "DATA"
        : header.action === 3
          ? "HALF_CLOSE"
          : "CLOSE",
    body: Uint8Array.from(bytes.subarray(portForwardHeaderBytes)),
  };
  Object.defineProperty(result, "body", { enumerable: false });
  Object.defineProperty(result, "toJSON", {
    value: () => ({
      connectionRef: result.connectionRef,
      generation: result.generation.toString(),
      slot: result.slot,
      nativeOrdinal: result.nativeOrdinal.toString(),
      action: result.action,
    }),
    enumerable: false,
  });
  return result;
}
// OPEN/CLOSE назначает отдельный owner command; этот producer допускает лишь DATA/полузакрытие.
export function encodePortForwardInput(
  connection: PortForwardCoordinates,
  body: Uint8Array,
  halfClose = false,
): Uint8Array {
  if (
    !validPortForwardCoordinates(connection) ||
    (halfClose ? body.length !== 0 : body.length === 0) ||
    body.length > 32768 - portForwardHeaderBytes
  )
    throw new Error("Kubernetes port-forward input is invalid");
  const result = new Uint8Array(portForwardHeaderBytes + body.length);
  result.set([75, 80, 70, 1, halfClose ? 3 : 2, connection.slot]);
  new DataView(result.buffer).setBigUint64(6, BigInt(connection.generation));
  result[14] = 28;
  result.set(new TextEncoder().encode(connection.connectionRef), 15);
  result.set(body, portForwardHeaderBytes);
  return result;
}
