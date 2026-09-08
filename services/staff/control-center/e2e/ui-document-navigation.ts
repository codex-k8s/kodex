const documentID = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/;
export type NavigationAction = "GOTO" | "RELOAD" | "CLOSE";
type Navigation = {
  action: NavigationAction;
  start: number;
  startOrder: number;
  from?: number;
  to?: number;
  end?: number;
  endOrder?: number;
  complete: boolean;
  ambiguous: boolean;
};
// Только private identity существующего Request и документа; URL/header не нужны.
export class DocumentNavigation<T extends object> {
  private readonly documents = new Set<string>();
  private current?: number;
  private readonly requests = new Map<
    T,
    {
      document?: number;
      mainFrame: boolean;
      started: number;
      terminal?: number;
      terminalOrder?: number;
      intent?: number;
    }
  >();
  private readonly navigations = new Map<number, Navigation>();
  private overflow = 0;
  private order = 0;
  document(id: string, at = Date.now()): void {
    if (!documentID.test(id) || this.documents.has(id)) return;
    if (this.documents.size >= 512) {
      this.overflow++;
      return;
    }
    this.documents.add(id);
    const from = this.current;
    this.current = this.documents.size;
    for (const navigation of this.navigations.values())
      if (
        navigation.end === undefined &&
        navigation.from === from &&
        navigation.start <= at &&
        !navigation.ambiguous
      )
        navigation.to = this.current;
  }
  request(request: T, mainFrame: boolean, at = Date.now()): void {
    if (this.requests.has(request)) return;
    if (this.requests.size >= 4096) {
      this.overflow++;
      return;
    }
    this.requests.set(request, {
      document: this.current,
      mainFrame,
      started: at,
    });
  }
  terminal(request: T, at = Date.now()): void {
    const value = this.requests.get(request);
    if (value && value.terminal === undefined) {
      value.terminal = at;
      value.terminalOrder = ++this.order;
    }
  }
  begin(action: NavigationAction, at = Date.now()): number {
    if (this.navigations.size >= 512) {
      this.overflow++;
      return 0;
    }
    const active = [...this.navigations.values()].filter(
      (value) => value.end === undefined,
    );
    active.forEach((value) => {
      value.ambiguous = true;
    });
    const id = this.navigations.size + 1;
    this.navigations.set(id, {
      action,
      start: at,
      startOrder: ++this.order,
      from: this.current,
      complete: false,
      ambiguous: active.length > 0,
    });
    for (const value of this.requests.values())
      if (
        value.mainFrame &&
        value.document !== undefined &&
        value.document === this.current &&
        value.started <= at &&
        value.terminal === undefined &&
        (value.intent === undefined ||
          this.navigations.get(value.intent)?.end !== undefined)
      )
        value.intent = id;
    return id;
  }
  end(
    id: number,
    complete: boolean,
    at = Date.now(),
    sameDocument = false,
  ): void {
    const value = this.navigations.get(id);
    if (!value || value.end !== undefined) return;
    value.end = at;
    value.endOrder = ++this.order;
    value.complete = complete;
    if (!sameDocument && value.action !== "CLOSE" && value.to === undefined)
      this.current = undefined;
  }
  evidence(request: T) {
    const value = this.requests.get(request);
    const intent =
      value?.intent === undefined
        ? undefined
        : this.navigations.get(value.intent);
    const committed =
      !!intent &&
      intent.complete &&
      !intent.ambiguous &&
      intent.from !== undefined &&
      (intent.action === "CLOSE" ||
        (intent.to !== undefined && intent.to !== intent.from));
    return {
      documentMainFrameObserved: value?.mainFrame ?? false,
      documentEpochKnown: value?.document !== undefined,
      documentRequestObserved:
        !!value && value.mainFrame && value.document !== undefined,
      documentNavigationIntent: !!intent,
      documentNavigationCommitted: committed,
      documentNavigationAmbiguous: intent?.ambiguous ?? false,
      documentNavigationWindow:
        !!intent &&
        value?.terminal !== undefined &&
        intent.end !== undefined &&
        value.terminalOrder !== undefined &&
        intent.endOrder !== undefined &&
        value.terminalOrder >= intent.startOrder &&
        value.terminalOrder <= intent.endOrder &&
        value.terminal >= intent.start &&
        value.terminal <= intent.end,
    };
  }
  confirmed(request: T): boolean {
    const value = this.evidence(request);
    return (
      !this.overflow &&
      value.documentRequestObserved &&
      value.documentNavigationCommitted &&
      value.documentNavigationWindow
    );
  }
  overflowCount(): number {
    return this.overflow;
  }
}
