interface AuthenticatedCookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  expires: number;
  httpOnly: boolean;
  secure: boolean;
  sameSite: "Strict" | "Lax";
}

export function replaceAuthenticatedCookies(
  context: {
    cookies(): Promise<
      Array<
        Omit<AuthenticatedCookie, "sameSite"> & {
          sameSite: "Strict" | "Lax" | "None";
        }
      >
    >;
    clearCookies(filter: {
      name: string;
      domain: string;
      path: string;
    }): Promise<void>;
    addCookies(cookies: AuthenticatedCookie[]): Promise<void>;
  },
  origin: string,
  previous: unknown,
  cookies: AuthenticatedCookie[],
): Promise<void>;
