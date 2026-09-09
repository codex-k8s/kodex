// Только локальная fixture: loopback HTTP не изменяет production parseConfig.
export function runtimeConfig() {
  return { apiBaseUrl: location.origin, requestTimeoutMs: 1000 };
}
