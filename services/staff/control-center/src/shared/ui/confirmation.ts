import { reactive, readonly } from "vue";

export type ConfirmationTone = "primary" | "danger";

export interface ConfirmationRequest {
  message: string;
  title?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: ConfirmationTone;
}

interface ConfirmationState {
  open: boolean;
  message: string;
  title?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone: ConfirmationTone;
}

const state = reactive<ConfirmationState>({
  open: false,
  message: "",
  tone: "primary",
});

let settlePending: ((confirmed: boolean) => void) | undefined;

export const confirmationState = readonly(state);

export function requestConfirmation(
  request: string | ConfirmationRequest,
): Promise<boolean> {
  settlePending?.(false);
  const options = typeof request === "string" ? { message: request } : request;
  state.message = options.message;
  state.title = options.title;
  state.confirmLabel = options.confirmLabel;
  state.cancelLabel = options.cancelLabel;
  state.tone = options.tone ?? "primary";
  state.open = true;
  return new Promise<boolean>((resolve) => {
    settlePending = resolve;
  });
}

export function resolveConfirmation(confirmed: boolean): void {
  if (!state.open) return;
  state.open = false;
  const settle = settlePending;
  settlePending = undefined;
  settle?.(confirmed);
}
