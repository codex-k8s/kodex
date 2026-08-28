export type OverlayCloseReason = "button" | "escape" | "outside";

export interface OverlayDismissPolicy {
  busy: boolean;
  closeOnEscape: boolean;
  closeOnOutside: boolean;
}

export function canDismissOverlay(
  reason: OverlayCloseReason,
  policy: OverlayDismissPolicy,
): boolean {
  if (policy.busy) return false;
  if (reason === "button") return true;
  return reason === "escape" ? policy.closeOnEscape : policy.closeOnOutside;
}

export function restoreOverlayFocus(target: HTMLElement | null): void {
  if (target?.isConnected) target.focus();
}
