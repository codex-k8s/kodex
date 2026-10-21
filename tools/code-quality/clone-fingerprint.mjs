import crypto from "node:crypto";
import path from "node:path";

const digestPattern = /^[0-9a-f]{64}$/;

function checkedLocation(location) {
  if (
    !location ||
    typeof location.path !== "string" ||
    !location.path ||
    path.isAbsolute(location.path) ||
    location.path.split("/").some((part) => part === "." || part === "..") ||
    !Number.isSafeInteger(location.start) ||
    !Number.isSafeInteger(location.end) ||
    location.start < 1 ||
    location.end < location.start
  ) {
    throw Error("Clone fingerprint has an invalid source location");
  }
  return [location.path, location.start, location.end];
}

// Хеш охватывает каждое совпадение с кратностью и координатами. Дата,
// порядок workers и направление пары не меняют идентичность набора.
export function cloneSetSHA256(clones) {
  if (!Array.isArray(clones)) {
    throw Error("Clone fingerprint requires a normalized clone list");
  }
  const rows = clones.map((clone) => {
    if (
      !clone ||
      typeof clone.format !== "string" ||
      !clone.format ||
      typeof clone.kind !== "string" ||
      !clone.kind ||
      !Number.isSafeInteger(clone.tokens) ||
      clone.tokens < 0 ||
      !Number.isSafeInteger(clone.lines) ||
      clone.lines < 0 ||
      typeof clone.fragmentSHA256 !== "string" ||
      !digestPattern.test(clone.fragmentSHA256)
    ) {
      throw Error("Clone fingerprint has an invalid detector group");
    }
    const locations = [
      checkedLocation(clone.first),
      checkedLocation(clone.second),
    ].map((location) => JSON.stringify(location)).sort();
    return JSON.stringify([
      clone.format,
      clone.kind,
      clone.tokens,
      clone.lines,
      ...locations,
      clone.fragmentSHA256,
    ]);
  });
  rows.sort();
  return crypto
    .createHash("sha256")
    .update(JSON.stringify(["jscpd-clone-set-v1", rows]))
    .digest("hex");
}
