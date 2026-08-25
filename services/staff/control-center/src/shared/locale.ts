export type SupportedLocale = "ru" | "en";

const localeKey = "kodex.locale";

export function currentLocale(): SupportedLocale {
  const stored = window.localStorage.getItem(localeKey);
  if (stored === "ru" || stored === "en") return stored;
  return navigator.language.toLowerCase().startsWith("ru") ? "ru" : "en";
}

export function persistLocale(locale: SupportedLocale): void {
  window.localStorage.setItem(localeKey, locale);
  document.documentElement.lang = locale;
}
