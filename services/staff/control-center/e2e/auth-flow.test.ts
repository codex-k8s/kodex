import { describe, expect, test } from "vitest";

import { isRecoverableBlankFrontendDocument } from "./auth-flow";

const frontendOrigin = "https://control.kodex.example";

describe("восстановление документа E2E-авторизации", () => {
  test("восстанавливает пустой корневой документ без #app", () => {
    expect(
      isRecoverableBlankFrontendDocument(
        {
          appChildElementCount: undefined,
          bodyText: "",
          origin: frontendOrigin,
          pathname: "/",
        },
        frontendOrigin,
      ),
    ).toBe(true);
  });

  test("восстанавливает пустой OIDC callback с незапущенным #app", () => {
    expect(
      isRecoverableBlankFrontendDocument(
        {
          appChildElementCount: 0,
          bodyText: "  \n",
          origin: frontendOrigin,
          pathname: "/auth/callback",
        },
        frontendOrigin,
      ),
    ).toBe(true);
  });

  test.each([
    {
      appChildElementCount: undefined,
      bodyText: "",
      origin: "https://identity.kodex.example",
      pathname: "/",
    },
    {
      appChildElementCount: undefined,
      bodyText: "",
      origin: frontendOrigin,
      pathname: "/projects",
    },
    {
      appChildElementCount: 1,
      bodyText: "",
      origin: frontendOrigin,
      pathname: "/",
    },
    {
      appChildElementCount: 0,
      bodyText: "Войти",
      origin: frontendOrigin,
      pathname: "/",
    },
  ])("не перезагружает действующую или чужую страницу %#", (snapshot) => {
    expect(isRecoverableBlankFrontendDocument(snapshot, frontendOrigin)).toBe(
      false,
    );
  });
});
