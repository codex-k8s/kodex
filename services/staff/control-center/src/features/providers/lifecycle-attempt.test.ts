import { describe, expect, it } from "vitest";
import {
  clearProviderLifecycleAttempts,
  forgetProviderLifecycleAttempt,
  readProviderLifecycleAttempt,
  rememberProviderLifecycleAttempt,
  type ProviderLifecycleAttempt,
} from "./lifecycle-attempt";
function storage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    key: (index) => [...values.keys()][index] ?? null,
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
  };
}
const attempt: ProviderLifecycleAttempt = {
  action: "CANCEL_QUEUED",
  accountRef: "pacc_synthetic",
  version: 7,
  key: "11111111-1111-4111-8111-111111111111",
  body: {
    selectedRunRefs: ["run_synthetic_1", "run_synthetic_2"],
    blockersDigest: "a".repeat(64),
  },
};
describe("provider lifecycle recovery intent", () => {
  it("сохраняет exact order/key/OCC/digest и не заменяет незавершённую команду", () => {
    const data = storage();
    rememberProviderLifecycleAttempt(attempt, data);
    expect(readProviderLifecycleAttempt(attempt.accountRef, data)).toEqual(
      attempt,
    );
    expect(() =>
      rememberProviderLifecycleAttempt({ ...attempt, version: 8 }, data),
    ).toThrow("cannot be replaced");
    expect(() =>
      rememberProviderLifecycleAttempt(
        {
          ...attempt,
          body: {
            ...attempt.body,
            selectedRunRefs: [...attempt.body.selectedRunRefs].reverse(),
          },
        },
        data,
      ),
    ).toThrow("cannot be replaced");
    forgetProviderLifecycleAttempt(attempt.accountRef, data);
    expect(
      readProviderLifecycleAttempt(attempt.accountRef, data),
    ).toBeUndefined();
  });
  it("не принимает дубли, чужой owner и повреждённые записи", () => {
    const data = storage();
    expect(() =>
      rememberProviderLifecycleAttempt(
        {
          ...attempt,
          body: {
            ...attempt.body,
            selectedRunRefs: ["run_synthetic_1", "run_synthetic_1"],
          },
        },
        data,
      ),
    ).toThrow();
    data.setItem(
      "kodex.provider-lifecycle:other_account",
      JSON.stringify(attempt),
    );
    expect(() => readProviderLifecycleAttempt("other_account", data)).toThrow();
  });
  it("logout убирает все provider intents, не затрагивая другое хранилище", () => {
    const data = storage();
    data.setItem("other", "value");
    rememberProviderLifecycleAttempt(attempt, data);
    rememberProviderLifecycleAttempt(
      {
        action: "DELETE",
        accountRef: "pacc_second",
        version: 2,
        key: attempt.key,
      },
      data,
    );
    clearProviderLifecycleAttempts(data);
    expect(data.length).toBe(1);
    expect(data.getItem("other")).toBe("value");
  });
});
