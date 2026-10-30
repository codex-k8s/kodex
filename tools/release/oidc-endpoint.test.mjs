import { test } from "node:test";
import assert from "node:assert/strict";
import { exactOIDCIssuer, publicOIDCClient } from "./oidc-endpoint.mjs";
import { renderAuthorityPolicyEnvironment, verifyAuthorityPolicyEnvironment } from "./authority-policy-environment.mjs";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../../deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json", import.meta.url), "utf8");
test("точные issuer произвольного IdP и bundled realm поддержаны независимо от пути", () => {
  for (const issuer of ["https://id.example.invalid", "https://id.example.invalid/", "https://id.example.invalid/auth/realms/test", "https://id.example.invalid/realms/kodex"]) {
    assert.equal(exactOIDCIssuer(issuer), true);
    const environment = { oidcIssuer: issuer, oidcAudience: "kodex-control-api" };
    const rendered = renderAuthorityPolicyEnvironment(source, environment);
    assert.equal(verifyAuthorityPolicyEnvironment(rendered.raw, environment).oidcProducers, 3);
    assert.throws(() => verifyAuthorityPolicyEnvironment(rendered.raw, { ...environment, oidcIssuer: issuer + "/other" }));
  }
});
test("небезопасные и неканонические issuer закрыто отклоняются", () => {
  for (const issuer of [null, "http://id.example.invalid", "https://x:fixture@id.example.invalid", "https://id.example.invalid/a/../b", "https://id.example.invalid/a?", "https://id.example.invalid/#", "https://ID.example.invalid/a", "https://id.example.invalid/%2f", "https://id.example.invalid:8443/a", "https://id.example.invalid/a\n"]) assert.equal(exactOIDCIssuer(issuer), false);
});
test("public client — явный идентификатор без secret и URI", () => {
  for (const value of ["kodex-control-center", "test-console"]) assert.equal(publicOIDCClient(value), true);
  for (const value of [null, "", "https://id.example.invalid", "test\n", "x".repeat(129)]) assert.equal(publicOIDCClient(value), false);
});
