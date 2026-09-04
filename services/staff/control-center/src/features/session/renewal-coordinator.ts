export const sessionRenewalLeaseKey = "kodex.session.renewal.lease";
export const sessionRenewalLeaseMs = 15_000;

interface RenewalLease {
  readonly owner: string;
  readonly token: string;
  readonly leaseExpiresAt: number;
  readonly nextRenewalAt: number;
}

export interface RenewalLeaseResult {
  readonly acquired: boolean;
  readonly retryAfterMs: number;
}

export class SessionRenewalCoordinator {
  private lease?: RenewalLease;

  constructor(
    private readonly storage: Pick<
      Storage,
      "getItem" | "setItem" | "removeItem"
    >,
    private readonly owner: string,
    private readonly now: () => number = Date.now,
    private readonly token: () => string = () => crypto.randomUUID(),
  ) {}

  acquire(): RenewalLeaseResult {
    const currentTime = this.now();
    const current = this.read();
    if (current && current.nextRenewalAt > currentTime)
      return {
        acquired: false,
        retryAfterMs: current.nextRenewalAt - currentTime,
      };
    if (
      current &&
      current.owner !== this.owner &&
      current.leaseExpiresAt > currentTime
    )
      return {
        acquired: false,
        retryAfterMs: current.leaseExpiresAt - currentTime,
      };

    const candidate: RenewalLease = {
      owner: this.owner,
      token: this.token(),
      leaseExpiresAt: currentTime + sessionRenewalLeaseMs,
      nextRenewalAt: 0,
    };
    this.storage.setItem(sessionRenewalLeaseKey, JSON.stringify(candidate));
    const confirmed = this.read();
    if (
      confirmed?.owner !== candidate.owner ||
      confirmed.token !== candidate.token
    )
      return {
        acquired: false,
        retryAfterMs: Math.max(
          1,
          (confirmed?.nextRenewalAt ||
            confirmed?.leaseExpiresAt ||
            currentTime + 1) - currentTime,
        ),
      };
    this.lease = candidate;
    return { acquired: true, retryAfterMs: 0 };
  }

  complete(nextRenewalInMs: number): number {
    const currentTime = this.now();
    const nextRenewalAt = currentTime + nextRenewalInMs;
    const current = this.read();
    if (
      this.lease &&
      current?.owner === this.lease.owner &&
      current.token === this.lease.token
    )
      this.storage.setItem(
        sessionRenewalLeaseKey,
        JSON.stringify({
          ...this.lease,
          leaseExpiresAt: currentTime,
          nextRenewalAt,
        } satisfies RenewalLease),
      );
    this.lease = undefined;
    return nextRenewalAt;
  }

  release(): void {
    const current = this.read();
    if (
      this.lease &&
      current?.owner === this.lease.owner &&
      current.token === this.lease.token
    )
      this.storage.removeItem(sessionRenewalLeaseKey);
    this.lease = undefined;
  }

  private read(): RenewalLease | undefined {
    const raw = this.storage.getItem(sessionRenewalLeaseKey);
    if (!raw) return undefined;
    try {
      const value = JSON.parse(raw) as Partial<RenewalLease>;
      if (
        typeof value.owner !== "string" ||
        typeof value.token !== "string" ||
        typeof value.leaseExpiresAt !== "number" ||
        !Number.isSafeInteger(value.leaseExpiresAt) ||
        typeof value.nextRenewalAt !== "number" ||
        !Number.isSafeInteger(value.nextRenewalAt)
      )
        return undefined;
      return value as RenewalLease;
    } catch {
      return undefined;
    }
  }
}

interface SessionRevisionMessage {
  readonly type: "session-renewed";
  readonly revision: number;
  readonly completedAt: number;
  readonly nextRenewalAt: number;
}

export interface SessionRenewalReceipt {
  readonly revision: number;
  readonly completedAt: number;
  readonly nextRenewalAt: number;
}

export interface SessionRevisionChannel {
  postMessage(message: SessionRevisionMessage): void;
  addEventListener(
    type: "message",
    listener: (event: MessageEvent) => void,
  ): void;
  removeEventListener(
    type: "message",
    listener: (event: MessageEvent) => void,
  ): void;
  close(): void;
}

export class SessionRenewalBus {
  private readonly listeners = new Set<
    (receipt: SessionRenewalReceipt) => void
  >();
  private completedAtHighWatermark = 0;

  constructor(
    private readonly channel: SessionRevisionChannel | undefined,
    private revisionHighWatermark: number,
  ) {
    this.revisionHighWatermark = validRevision(revisionHighWatermark)
      ? revisionHighWatermark
      : 0;
    this.channel?.addEventListener("message", this.receive);
  }

  publish(receipt: SessionRenewalReceipt): void {
    if (
      !validReceipt(receipt) ||
      receipt.completedAt <= this.completedAtHighWatermark ||
      receipt.revision < this.revisionHighWatermark
    )
      return;
    this.accept(receipt);
    this.channel?.postMessage({ type: "session-renewed", ...receipt });
  }

  subscribe(listener: (receipt: SessionRenewalReceipt) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  close(): void {
    this.channel?.removeEventListener("message", this.receive);
    this.channel?.close();
    this.listeners.clear();
  }

  private readonly receive = (event: MessageEvent): void => {
    const message = event.data as Partial<SessionRevisionMessage> | undefined;
    if (
      message?.type !== "session-renewed" ||
      !validReceipt(message) ||
      message.completedAt <= this.completedAtHighWatermark ||
      message.revision < this.revisionHighWatermark
    )
      return;
    this.accept(message);
    for (const listener of this.listeners) listener(message);
  };

  private accept(receipt: SessionRenewalReceipt): void {
    this.completedAtHighWatermark = Math.max(
      this.completedAtHighWatermark,
      receipt.completedAt,
    );
    this.revisionHighWatermark = Math.max(
      this.revisionHighWatermark,
      receipt.revision,
    );
  }
}

function validRevision(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function validReceipt(
  value: Partial<SessionRenewalReceipt>,
): value is SessionRenewalReceipt {
  return (
    validRevision(value.revision) &&
    typeof value.completedAt === "number" &&
    Number.isSafeInteger(value.completedAt) &&
    value.completedAt > 0 &&
    typeof value.nextRenewalAt === "number" &&
    Number.isSafeInteger(value.nextRenewalAt) &&
    value.nextRenewalAt > value.completedAt
  );
}
