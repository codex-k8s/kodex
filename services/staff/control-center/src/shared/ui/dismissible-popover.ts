export type DismissiblePopoverPlacement = "bottom-start" | "bottom-end";
export type DismissiblePopoverCloseReason =
  | "escape"
  | "outside"
  | "toggle"
  | "programmatic";

export interface DismissiblePopoverPolicy {
  closeOnEscape: boolean;
  closeOnOutside: boolean;
}

export function canDismissPopover(
  reason: DismissiblePopoverCloseReason,
  policy: DismissiblePopoverPolicy,
): boolean {
  if (reason === "escape") return policy.closeOnEscape;
  if (reason === "outside") return policy.closeOnOutside;
  return true;
}

export function shouldRestorePopoverFocus(
  reason: DismissiblePopoverCloseReason,
): boolean {
  return reason !== "outside";
}

export function restorePopoverFocus(
  target: HTMLElement | null,
  enabled: boolean,
): void {
  if (enabled && target?.isConnected) target.focus();
}

export interface PopoverRect {
  top: number;
  right: number;
  bottom: number;
  left: number;
  width: number;
}

export interface PopoverPositionInput {
  anchor: PopoverRect;
  panelWidth: number;
  panelHeight: number;
  viewportWidth: number;
  viewportHeight: number;
  placement: DismissiblePopoverPlacement;
  gap?: number;
  margin?: number;
}

export interface PopoverPosition {
  left: number;
  top: number;
  maxHeight: number;
  side: "top" | "bottom";
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(Math.max(value, minimum), Math.max(minimum, maximum));
}

export function calculatePopoverPosition(
  input: PopoverPositionInput,
): PopoverPosition {
  const margin = input.margin ?? 8;
  const gap = input.gap ?? 6;
  const availableBelow = Math.max(
    0,
    input.viewportHeight - input.anchor.bottom - gap - margin,
  );
  const availableAbove = Math.max(0, input.anchor.top - gap - margin);
  const side =
    availableBelow >= Math.min(input.panelHeight, 240) ||
    availableBelow >= availableAbove
      ? "bottom"
      : "top";
  const maxHeight = side === "bottom" ? availableBelow : availableAbove;
  const unclampedTop =
    side === "bottom"
      ? input.anchor.bottom + gap
      : input.anchor.top - gap - Math.min(input.panelHeight, maxHeight);
  const preferredLeft =
    input.placement === "bottom-end"
      ? input.anchor.right - input.panelWidth
      : input.anchor.left;

  return {
    left: clamp(
      preferredLeft,
      margin,
      input.viewportWidth - input.panelWidth - margin,
    ),
    top: clamp(
      unclampedTop,
      margin,
      input.viewportHeight - Math.min(input.panelHeight, maxHeight) - margin,
    ),
    maxHeight,
    side,
  };
}
