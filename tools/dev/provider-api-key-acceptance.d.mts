interface AuthenticatedCookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  expires: number;
  httpOnly: boolean;
  secure: boolean;
  sameSite: "Strict";
}

export function authorizeProviderAPIKeyFixture(options: {
  origin: string;
  storage?: unknown;
  storagePath?: string;
  accountRef: string;
  apiKey: string;
  idempotencyKey?: string;
  fetchAPI?: typeof fetch;
  onSessionCookies?: (cookies: AuthenticatedCookie[]) => Promise<void>;
}): Promise<void>;
