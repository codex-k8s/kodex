import { describe, expect, it } from "vitest";
import { selectDiscoveryProviderAccount } from "./discovery-state";

describe("выбор provider fixtures discovery", () => {
  const first = "pacc_first0001";
  const second = "pacc_second001";
  it("назначает два разных account независимо от stable key", () => {
    expect(selectDiscoveryProviderAccount([second, first], "", "")).toBe(first);
    expect(selectDiscoveryProviderAccount([second, first], "", first)).toBe(
      second,
    );
  });
  it("не меняет сохранённый выбор при появлении третьего account", () => {
    expect(
      selectDiscoveryProviderAccount(
        ["pacc_aaaa0001", second, first],
        second,
        first,
      ),
    ).toBe(second);
  });
  it("не подменяет исчезнувший сохранённый account", () => {
    expect(
      selectDiscoveryProviderAccount(["pacc_aaaa0001", first], second, first),
    ).toBe("");
  });
  it("закрыто отклоняет один distinct account и совпавшие pins", () => {
    expect(selectDiscoveryProviderAccount([first, first], "", "")).toBe("");
    expect(selectDiscoveryProviderAccount([first, second], first, first)).toBe(
      "",
    );
  });
});
