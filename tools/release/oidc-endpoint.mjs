// Issuer — точное значение из доверенного deployment input, не имя realm.
// URL normalization не должна незаметно менять строку сравнения JWT iss.
export function exactOIDCIssuer(value) {
  if (typeof value !== "string" || value.length > 2048 || /[\s\\%?#]/.test(value)) return false;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && url.hostname.includes(".") && !url.username && !url.password &&
      !url.port && (url.href === value || (url.pathname === "/" && url.origin === value));
  } catch { return false; }
}

export function publicOIDCClient(value) {
  return typeof value === "string" && /^[A-Za-z0-9_.-]{1,128}$/.test(value);
}
