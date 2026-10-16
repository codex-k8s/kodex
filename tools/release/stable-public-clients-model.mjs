import { fingerprint } from "./stable-inventory.mjs";
import { publicIdentityProfile } from "./stable-public-identity-model.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
const equal = (a, b) => fingerprint(a) === fingerprint(b);
const clients = new Map([["kodex-control-center", "/auth/callback"], ["kodex-control-center-proxy", "/oauth2/callback"]]);
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;

// Admin API может возвращать secret и transport access hints. Они не входят
// в configuration receipt и никогда не отправляются обратно через PUT.
export function publicClientFingerprint(client) {
  const value = structuredClone(client); delete value.secret; delete value.registrationAccessToken; delete value.access;
  return fingerprint(value);
}
function validate(client) {
  requireValue(clients.has(client?.clientId) && uuid.test(client.id ?? "") && client.enabled === true && client.protocol === "openid-connect" &&
    client.publicClient === (client.clientId === "kodex-control-center") && client.standardFlowEnabled === true &&
    client.implicitFlowEnabled === false && client.directAccessGrantsEnabled === false && client.serviceAccountsEnabled === false &&
    client.attributes?.["pkce.code.challenge.method"] === "S256" &&
    ["rootUrl", "baseUrl", "adminUrl"].every(k => [undefined, ""].includes(client[k])), "PUBLIC_CLIENT_PROFILE_INVALID");
}
export function publicClientTransition(client, oldOrigin, profile) {
  validate(client); const urls = publicIdentityProfile(profile), origin = new URL(oldOrigin);
  requireValue(origin.protocol === "https:" && origin.origin === oldOrigin && !origin.username && !origin.password && !origin.port &&
    equal(client.redirectUris, [oldOrigin + clients.get(client.clientId)]) && equal(client.webOrigins, [oldOrigin]) &&
    client.attributes["post.logout.redirect.uris"] === oldOrigin + "/*", "PUBLIC_CLIENT_ORIGIN_PROFILE_INVALID");
  const before = { redirectUris: structuredClone(client.redirectUris), webOrigins: structuredClone(client.webOrigins),
    logout: client.attributes["post.logout.redirect.uris"] };
  const after = { redirectUris: [urls.origin + clients.get(client.clientId)], webOrigins: [urls.origin], logout: urls.origin + "/*" };
  const desired = structuredClone(client); desired.redirectUris = after.redirectUris; desired.webOrigins = after.webOrigins;
  desired.attributes["post.logout.redirect.uris"] = after.logout;
  return { id: client.id, clientId: client.clientId, before, after, beforeSHA256: publicClientFingerprint(client), afterSHA256: publicClientFingerprint(desired) };
}
export function publicClientPhase(client, row) {
  validate(client);
  requireValue(client.id === row.id && client.clientId === row.clientId, "PUBLIC_CLIENT_IDENTITY_CHANGED");
  const hash = publicClientFingerprint(client), phase = hash === row.beforeSHA256 ? "before" : hash === row.afterSHA256 ? "after" : null;
  requireValue(phase, "PUBLIC_CLIENT_CONFIGURATION_DRIFT"); return phase;
}
export function publicClientPut(client, row, forward) {
  requireValue(publicClientPhase(client, row) === (forward ? "before" : "after"), "PUBLIC_CLIENT_PREDECESSOR_INVALID");
  const target = forward ? row.after : row.before;
  // Остальные attributes сохраняются в памяти из свежего exact readback;
  // scopes, mapper, secret, role, grant flags и user state отсутствуют в PUT.
  return { redirectUris: structuredClone(target.redirectUris), webOrigins: structuredClone(target.webOrigins),
    attributes: { ...client.attributes, "post.logout.redirect.uris": target.logout } };
}
