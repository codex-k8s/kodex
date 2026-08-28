export interface RunGraphViewportSize {
  width: number;
  height: number;
}

export interface RunGraphView {
  x: number;
  y: number;
  scale: number;
}

export interface RunGraphPoint {
  x: number;
  y: number;
}

export function clampRunGraphScale(
  scale: number,
  minimumScale: number,
  maximumScale: number,
): number {
  return Math.min(maximumScale, Math.max(minimumScale, scale));
}

export function fitRunGraphView(
  graph: RunGraphViewportSize,
  viewport: RunGraphViewportSize,
  minimumScale: number,
  maximumScale: number,
  padding = 32,
): RunGraphView {
  if (!graph.width || !graph.height || !viewport.width || !viewport.height) {
    return { x: 0, y: 0, scale: 1 };
  }

  const availableWidth = Math.max(1, viewport.width - padding * 2);
  const availableHeight = Math.max(1, viewport.height - padding * 2);
  const scale = clampRunGraphScale(
    Math.min(availableWidth / graph.width, availableHeight / graph.height),
    minimumScale,
    maximumScale,
  );

  return {
    x: (viewport.width - graph.width * scale) / 2,
    y: (viewport.height - graph.height * scale) / 2,
    scale,
  };
}

export function zoomRunGraphAtPoint(
  view: RunGraphView,
  requestedScale: number,
  point: RunGraphPoint,
  minimumScale: number,
  maximumScale: number,
): RunGraphView {
  const scale = clampRunGraphScale(requestedScale, minimumScale, maximumScale);
  const ratio = scale / view.scale;

  return {
    x: point.x - (point.x - view.x) * ratio,
    y: point.y - (point.y - view.y) * ratio,
    scale,
  };
}
