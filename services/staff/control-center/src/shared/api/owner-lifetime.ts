let controller = new AbortController();

export class OwnerContextChangedError extends Error {
  constructor() {
    super("Owner context changed");
    this.name = "OwnerContextChangedError";
  }
}

export function ownerRequestSignal(): AbortSignal {
  return controller.signal;
}

export function assertOwnerRequest(signal: AbortSignal): void {
  if (signal.aborted) throw new OwnerContextChangedError();
}

export function resetOwnerRequests(): void {
  controller.abort();
  controller = new AbortController();
}
