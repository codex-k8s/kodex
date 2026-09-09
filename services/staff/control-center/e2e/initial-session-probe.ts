import type { Request } from "@playwright/test";

type ProbeRequest = Pick<Request, "url" | "method" | "resourceType">;

/** Идентичность запроса фиксируется до первой попытки frontend OIDC. */
export class InitialSessionProbeObservation {
  private readonly initial = new WeakSet<ProbeRequest>();
  private transitionStarted = false;
  observed401s = 0;

  observe(request: ProbeRequest): void {
    if (!this.transitionStarted) this.initial.add(request);
  }

  startTransition(): void {
    this.transitionStarted = true;
  }

  acceptInitial401(
    request: ProbeRequest,
    status: number,
    frontendOrigin: string,
  ): boolean {
    if (
      !this.transitionStarted ||
      status !== 401 ||
      !this.initial.has(request) ||
      request.method() !== "GET" ||
      !["fetch", "xhr"].includes(request.resourceType())
    )
      return false;
    try {
      const url = new URL(request.url());
      if (
        url.origin !== frontendOrigin ||
        url.pathname !== "/api/v1/session" ||
        url.search !== "" ||
        url.hash !== ""
      )
        return false;
      this.observed401s++;
      return true;
    } catch {
      return false;
    }
  }
}
