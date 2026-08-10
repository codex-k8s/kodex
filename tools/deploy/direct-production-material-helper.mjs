#!/usr/bin/env node

import { createHash, createPrivateKey, createPublicKey, generateKeyPairSync, hkdfSync } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";

function fail(message) {
  process.stderr.write(`Direct production material helper failed: ${message}\n`);
  process.exit(1);
}

function encode(value) {
  return Buffer.from(value).toString("base64url");
}

function canonicalPublicJWK(privateJWK) {
  const publicJWK = createPublicKey({ key: privateJWK, format: "jwk" }).export({ format: "jwk" });
  const kid = createHash("sha256")
    .update(JSON.stringify({ crv: publicJWK.crv, kty: publicJWK.kty, x: publicJWK.x }))
    .digest("hex");
  return { ...publicJWK, alg: "EdDSA", kid, use: "sig" };
}

function writeJSON(path, value) {
  writeFileSync(path, `${JSON.stringify(value)}\n`, { mode: 0o600 });
}

function decodeJWT(value) {
  const parts = value.trim().split(".");
  if (parts.length !== 3 || parts.some((part) => !/^[A-Za-z0-9_-]+$/.test(part))) fail("JWT is invalid");
  return JSON.parse(Buffer.from(parts[1], "base64url").toString("utf8"));
}

function decodeJWTFile(path) {
  const value = readFileSync(path, "utf8").split(/\r?\n/).find((line) => line.split(".").length === 3);
  if (!value) fail("JWT file is invalid");
  return decodeJWT(value);
}

const [command, ...args] = process.argv.slice(2);
switch (command) {
  case "generate-jwk": {
    if (args.length !== 1) fail("generate-jwk requires an output path");
    const { privateKey } = generateKeyPairSync("ed25519");
    const privateJWK = privateKey.export({ format: "jwk" });
    const publicJWK = canonicalPublicJWK(privateJWK);
    writeJSON(args[0], { ...privateJWK, alg: "EdDSA", kid: publicJWK.kid, use: "sig" });
    break;
  }
  case "public-jwk": {
    if (args.length !== 2) fail("public-jwk requires input and output paths");
    const privateJWK = JSON.parse(readFileSync(args[0], "utf8"));
    writeJSON(args[1], canonicalPublicJWK(privateJWK));
    break;
  }
  case "public-jwks": {
    if (args.length < 2) fail("public-jwks requires output and at least one input path");
    const [output, ...inputs] = args;
    const keys = inputs.map((path) => canonicalPublicJWK(JSON.parse(readFileSync(path, "utf8"))));
    writeJSON(output, { keys });
    break;
  }
  case "generate-payload-keyset": {
    if (args.length !== 2) fail("generate-payload-keyset requires root and output paths");
    const root = Buffer.from(readFileSync(args[0], "utf8").trim(), "hex");
    if (root.length !== 32) fail("material root is invalid");
    const key = Buffer.from(hkdfSync("sha256", root, Buffer.alloc(0), "integration-gateway-payload-keyset/g1", 32));
    writeJSON(args[1], { active: "g1", keys: { g1: key.toString("base64") } });
    break;
  }
  case "derive-hex": {
    if (args.length !== 3) fail("derive-hex requires root, label and output paths");
    const root = Buffer.from(readFileSync(args[0], "utf8").trim(), "hex");
    if (root.length !== 32 || args[1].length === 0) fail("HKDF input is invalid");
    const value = Buffer.from(hkdfSync("sha256", root, Buffer.alloc(0), args[1], 32));
    writeFileSync(args[2], value.toString("hex"), { mode: 0o600 });
    break;
  }
  case "ed25519-public-hex": {
    if (args.length !== 2) fail("ed25519-public-hex requires seed and output paths");
    const seed = Buffer.from(readFileSync(args[0], "utf8").trim(), "hex");
    if (seed.length !== 32) fail("Ed25519 seed is invalid");
    const prefix = Buffer.from("302e020100300506032b657004220420", "hex");
    const privateKey = createPrivateKey({ key: Buffer.concat([prefix, seed]), format: "der", type: "pkcs8" });
    const publicJWK = createPublicKey(privateKey).export({ format: "jwk" });
    writeFileSync(args[1], Buffer.from(publicJWK.x, "base64url").toString("hex"), { mode: 0o600 });
    break;
  }
  case "validate-jws": {
    if (args.length !== 1) fail("validate-jws requires an input path");
    const value = readFileSync(args[0], "utf8").trim();
    const parts = value.split(".");
    if (parts.length !== 3 || parts.some((part) => !/^[A-Za-z0-9_-]+$/.test(part))) fail("compact JWS is invalid");
    const header = JSON.parse(Buffer.from(parts[0], "base64url").toString("utf8"));
    if (header.alg !== "EdDSA" || typeof header.kid !== "string" || header.kid.length === 0) fail("compact JWS header is invalid");
    JSON.parse(Buffer.from(parts[1], "base64url").toString("utf8"));
    if (Buffer.from(parts[2], "base64url").length !== 64) fail("compact JWS signature is invalid");
    break;
  }
  case "validate-nats-creds": {
    if (args.length !== 4) fail("validate-nats-creds requires input, name, publish and subscribe sets");
    const value = readFileSync(args[0], "utf8");
    const jwtMatch = value.match(/BEGIN NATS USER JWT-----\s*([A-Za-z0-9_.-]+)\s*-+END NATS USER JWT/);
    if (!jwtMatch || !/BEGIN USER NKEY SEED/.test(value)) fail("NATS credentials file is invalid");
    const claims = decodeJWT(jwtMatch[1]);
    const expectedPublish = args[2].split(",").filter(Boolean).sort();
    const expectedSubscribe = args[3].split(",").filter(Boolean).sort();
    const actualPublish = [...(claims.nats?.pub?.allow ?? [])].sort();
    const actualSubscribe = [...(claims.nats?.sub?.allow ?? [])].sort();
    if (claims.name !== args[1] || JSON.stringify(actualPublish) !== JSON.stringify(expectedPublish) ||
        JSON.stringify(actualSubscribe) !== JSON.stringify(expectedSubscribe)) fail("NATS user permissions are invalid");
    break;
  }
  case "validate-nats-server": {
    if (args.length !== 3) fail("validate-nats-server requires operator JWT, account JWT and public account paths");
    const operator = decodeJWTFile(args[0]);
    const account = decodeJWTFile(args[1]);
    const accountPublic = readFileSync(args[2], "utf8").trim();
    if (operator.nats?.type !== "operator" || account.nats?.type !== "account" || account.sub !== accountPublic ||
        account.iss !== operator.sub) fail("NATS operator/account binding is invalid");
    break;
  }
  case "extract-jwt": {
    if (args.length !== 2) fail("extract-jwt requires input and output paths");
    const value = readFileSync(args[0], "utf8").split(/\r?\n/).find((line) => line.split(".").length === 3);
    if (!value) fail("JWT file is invalid");
    decodeJWT(value);
    writeFileSync(args[1], value, { mode: 0o600 });
    break;
  }
  default:
    fail("unsupported command");
}
