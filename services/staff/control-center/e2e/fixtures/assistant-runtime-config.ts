// syntheticAssistantRuntimeConfig создаёт независимую тестовую конфигурацию для
// сценариев помощника; она не содержит действующих адресов или учётных данных.
export function syntheticAssistantRuntimeConfig() {
  return {
    revision: "0".repeat(64),
    environment: "synthetic",
    apiBaseUrl: "/",
    realtimeUrl: "/api/v1",
    requestTimeoutMs: 10000,
    oidc: {
      authority: "https://identity.kodex.test/realms/kodex",
      clientId: "kodex-test",
      redirectUri: "https://kodex.test/auth/callback",
      postLogoutRedirectUri: "https://kodex.test/",
      scope: "openid profile email",
    },
  };
}
