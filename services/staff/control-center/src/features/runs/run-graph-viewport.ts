export type RunGraphViewportCommand =
  | { type: "FIT" }
  | { type: "ZOOM_IN" }
  | { type: "ZOOM_OUT" }
  | { type: "PAN"; x: number; y: number };

export interface RunGraphViewportKey {
  key: string;
  shiftKey?: boolean;
}

export function runGraphViewportCommand(
  event: RunGraphViewportKey,
): RunGraphViewportCommand | undefined {
  if (event.key === "+" || event.key === "=") return { type: "ZOOM_IN" };
  if (event.key === "-") return { type: "ZOOM_OUT" };
  if (event.key === "0") return { type: "FIT" };

  const distance = event.shiftKey ? 96 : 42;
  switch (event.key) {
    case "ArrowLeft":
      return { type: "PAN", x: distance, y: 0 };
    case "ArrowRight":
      return { type: "PAN", x: -distance, y: 0 };
    case "ArrowUp":
      return { type: "PAN", x: 0, y: distance };
    case "ArrowDown":
      return { type: "PAN", x: 0, y: -distance };
    default:
      return undefined;
  }
}
