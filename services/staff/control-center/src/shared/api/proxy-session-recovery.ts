// AuthOnly oauth2-proxy 7.15.3 отвечает plain 401 до BFF. Это не отзыв
// browser family. Endpoint закреплён infra/management-surfaces/routes.yaml.
export const ingressProxyRecoveryCode = "INGRESS_PROXY_AUTHORIZATION_REQUIRED";
export const ingressProxyRecoveryKey = "kodex.session.proxy-recovery";
const proxySignInPath = "/oauth2/sign_in";
const referencePattern = /^[A-Za-z0-9_-]{8,128}$/;
const recoveringLocations = new WeakSet();

interface RecoveryContext {
  readonly location: Pick<Location, "origin" | "pathname" | "search">;
  readonly storage: Pick<Storage, "getItem" | "setItem">;
  readonly navigate: (path: string) => void;
}

export function isIngressProxyUnauthorized(
  value: unknown,
  response: Response,
  origin: string,
): boolean {
  if (
    response.status !== 401 ||
    response.redirected ||
    value !== "Unauthorized\n" ||
    response.headers.get("Content-Type")?.split(";", 1)[0]?.trim() !==
      "text/plain"
  )
    return false;
  try {
    const target = new URL(response.url);
    const owner = new URL(origin);
    return (
      owner.protocol === "https:" &&
      target.origin === owner.origin &&
      !target.username &&
      !target.password &&
      !target.hash &&
      /^\/api\/v1(?:\/|$)/.test(target.pathname)
    );
  } catch {
    return false;
  }
}

function returnPath(location: RecoveryContext["location"]): string {
  // Только существующие UI-зоны app/router/index.ts; не API, callback либо
  // caller-provided redirect. Не переносим OAuth code/state и прочие query.
  const path = location.pathname;
  if (
    path.length > 2048 ||
    !/^\/(?:projects|organization|agents|workflows|automations|environments|secrets|members|configurations|context|files|runs|integrations|decisions|administration|onboarding)(?:\/|$)/.test(
      path,
    ) ||
    !/^\/[A-Za-z0-9_/-]*$/.test(path)
  )
    return "/";
  const input = new URLSearchParams(location.search);
  const query = new URLSearchParams();
  for (const key of ["draftRef", "planRef"] as const) {
    const values = input.getAll(key);
    const value = values[0];
    if (
      values.length === 1 &&
      typeof value === "string" &&
      referencePattern.test(value)
    )
      query.set(key, value);
  }
  const tabs = input.getAll("tab");
  const tab = tabs[0];
  if (
    tabs.length === 1 &&
    typeof tab === "string" &&
    /^[a-z-]{1,48}$/.test(tab)
  )
    query.set("tab", tab);
  const suffix = query.toString();
  return suffix ? `${path}?${suffix}` : path;
}

export function recoverIngressProxySession(
  value: unknown,
  response: Response,
  context?: RecoveryContext,
): boolean {
  if (!context && typeof window === "undefined") return false;
  const location = context?.location ?? window.location;
  if (!isIngressProxyUnauthorized(value, response, location.origin))
    return false;
  if (recoveringLocations.has(location)) return true;
  try {
    const storage = context?.storage ?? window.sessionStorage;
    // Один переход до validated owner session readback, в том числе после
    // загрузки нового документа. Никакого повторного API/mutation запроса.
    if (!storage.getItem(ingressProxyRecoveryKey)) {
      const target = returnPath(location);
      storage.setItem(ingressProxyRecoveryKey, "1");
      const destination = new URL(proxySignInPath, location.origin);
      destination.searchParams.set("rd", target);
      const navigate =
        context?.navigate ?? ((path: string) => window.location.replace(path));
      // Запоздалый metadata ACK старого документа может очистить storage;
      // он не разрешает второй переход в том же document lifetime.
      recoveringLocations.add(location);
      navigate(destination.pathname + destination.search);
    }
  } catch {
    // Недоступный storage/navigation не превращает proxy401 в отзыв BFF.
    // Ошибка остаётся закрытой и non-retryable; автоматический цикл запрещён.
  }
  return true;
}

export function clearIngressProxyRecovery(
  storage?: Pick<Storage, "removeItem">,
): void {
  try {
    if (!storage && typeof window === "undefined") return;
    const target = storage ?? window.sessionStorage;
    target.removeItem(ingressProxyRecoveryKey);
  } catch {
    // Marker не является authority. Недоступный storage не отменяет
    // валидный BFF readback и не раскрывается в диагностике.
  }
}
