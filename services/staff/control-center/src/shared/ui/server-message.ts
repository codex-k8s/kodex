import { useI18n } from "vue-i18n";
import { serverMessageTokens } from "./server-message-catalog";
export function serverMessageKey(value: string): string | undefined {
  if (!value.startsWith("i18n:")) return undefined;
  const key = value.slice(5);
  return serverMessageTokens.has(key) ? `serverMessages.${key}` : undefined;
}
export function useServerMessage(): (value: string) => string {
  const { t } = useI18n();
  return (value) => {
    const key = serverMessageKey(value);
    if (key) return t(key);
    return value.startsWith("i18n:") ? t("serverMessages.unsupported") : value;
  };
}
