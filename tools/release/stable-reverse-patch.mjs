import { fingerprint } from "./stable-inventory.mjs";

function requireValue(value) { if (!value) throw Error("STABLE_REVERSE_PATCH_INVALID"); }
const equal = (left, right) => fingerprint(left) === fingerprint(right);
const segment = value => String(value).replaceAll("~", "~0").replaceAll("/", "~1");
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
const named = values => values.every(v => object(v) && typeof v.name === "string") && new Set(values.map(v => v.name)).size === values.length;

// Сохраняются только изменённые значения. Неизменённые env/Secret references
// не копируются в recovery record из-за сдвига индексов соседних dev-полей.
export function reverseSpecPatch(beforeSpec, afterSpec) {
  const operations = [];
  const walk = (current, target, path) => {
    if (equal(current, target)) return;
    if (object(current) && object(target)) {
      for (const key of Object.keys(current).filter(key => !Object.hasOwn(target, key)).sort())
        operations.push({ op: "remove", path: path + "/" + segment(key) });
      for (const key of Object.keys(target).sort()) {
        const pointer = path + "/" + segment(key);
        if (!Object.hasOwn(current, key)) operations.push({ op: "add", path: pointer, value: structuredClone(target[key]) });
        else walk(current[key], target[key], pointer);
      }
    } else if (Array.isArray(current) && Array.isArray(target)) {
      const values = structuredClone(current), useNames = named(current) && named(target);
      for (let index = 0; index < target.length; index++) {
        const wanted = target[index];
        const found = values.findIndex((value, position) => position >= index &&
          (useNames ? value.name === wanted.name : equal(value, wanted)));
        if (found >= 0) {
          if (found !== index) {
            operations.push({ op: "move", from: path + "/" + found, path: path + "/" + index });
            values.splice(index, 0, values.splice(found, 1)[0]);
          }
          walk(values[index], wanted, path + "/" + index);
        } else if (!useNames && object(values[index]) && object(wanted)) {
          walk(values[index], wanted, path + "/" + index);
        } else {
          operations.push({ op: "add", path: path + "/" + index, value: structuredClone(wanted) });
          values.splice(index, 0, structuredClone(wanted));
        }
      }
      for (let index = values.length - 1; index >= target.length; index--)
        operations.push({ op: "remove", path: path + "/" + index });
    } else operations.push({ op: "replace", path, value: structuredClone(target) });
  };
  walk(afterSpec, beforeSpec, "/spec");
  requireValue(operations.length <= 512 && equal(applyReverseSpec(afterSpec, operations), beforeSpec));
  return operations;
}

export function applyReverseSpec(spec, operations) {
  requireValue(Array.isArray(operations) && operations.length <= 512);
  const document = { spec: structuredClone(spec) };
  const pointer = path => {
    requireValue(typeof path === "string" && /^\/spec(?:\/|$)/.test(path) && path.length <= 2048 && !/~(?![01])/.test(path));
    const parts = path.slice(1).split("/").map(p => p.replaceAll("~1", "/").replaceAll("~0", "~"));
    const key = parts.pop(); let parent = document;
    for (const part of parts) { requireValue(parent && Object.hasOwn(parent, part)); parent = parent[part]; }
    requireValue(parent !== null && typeof parent === "object");
    if (Array.isArray(parent)) requireValue(/^(?:0|[1-9][0-9]*)$/.test(key) && Number(key) <= parent.length);
    return { parent, key: Array.isArray(parent) ? Number(key) : key };
  };
  const remove = ({ parent, key }) => {
    requireValue(Object.hasOwn(parent, key)); const value = parent[key];
    if (Array.isArray(parent)) parent.splice(key, 1); else delete parent[key];
    return value;
  };
  const add = ({ parent, key }, value) => {
    if (Array.isArray(parent)) parent.splice(key, 0, value);
    else Object.defineProperty(parent, key, { value, writable: true, enumerable: true, configurable: true });
  };
  for (const operation of operations) {
    requireValue(operation && ["add", "remove", "replace", "move"].includes(operation.op));
    if (operation.op === "move") {
      requireValue(operation.from !== operation.path && !operation.path.startsWith(operation.from + "/"));
      const value = remove(pointer(operation.from)); add(pointer(operation.path), value);
    } else {
      const target = pointer(operation.path);
      if (operation.op === "remove") remove(target);
      else {
        requireValue(Object.hasOwn(operation, "value"));
        if (operation.op === "replace") remove(target);
        add(target, structuredClone(operation.value));
      }
    }
  }
  return document.spec;
}
