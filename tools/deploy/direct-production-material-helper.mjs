#!/usr/bin/env node

import { createHash, createPrivateKey, createPublicKey, generateKeyPairSync, hkdfSync, sign } from "node:crypto";
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
  if (publicJWK.kty !== "EC" || publicJWK.crv !== "P-256" ||
      typeof publicJWK.x !== "string" || typeof publicJWK.y !== "string") {
    fail("private ES256 JWK is invalid");
  }
  const kid = createHash("sha256")
    .update(JSON.stringify({ crv: publicJWK.crv, kty: publicJWK.kty, x: publicJWK.x, y: publicJWK.y }))
    .digest("hex");
  return {
    kty: publicJWK.kty,
    crv: publicJWK.crv,
    x: publicJWK.x,
    y: publicJWK.y,
    alg: "ES256",
    kid,
    use: "sig",
    key_ops: ["verify"],
  };
}

function canonicalPrivateJWK(privateJWK) {
  const publicJWK = canonicalPublicJWK(privateJWK);
  if (typeof privateJWK.d !== "string" || privateJWK.d.length === 0) {
    fail("private ES256 JWK is invalid");
  }
  return {
    kty: publicJWK.kty,
    crv: publicJWK.crv,
    x: publicJWK.x,
    y: publicJWK.y,
    d: privateJWK.d,
    alg: "ES256",
    kid: publicJWK.kid,
    use: "sig",
    key_ops: ["sign"],
  };
}

function validatePrivateJWK(path) {
  const privateJWK = JSON.parse(readFileSync(path, "utf8"));
  const canonical = canonicalPrivateJWK(privateJWK);
  const actualKeys = Object.keys(privateJWK).sort();
  const expectedKeys = Object.keys(canonical).sort();
  if (JSON.stringify(actualKeys) !== JSON.stringify(expectedKeys) ||
      actualKeys.some((key) => JSON.stringify(privateJWK[key]) !== JSON.stringify(canonical[key]))) {
    fail("private ES256 JWK is not canonical");
  }
}

function writeJSON(path, value) {
  writeFileSync(path, `${JSON.stringify(value)}\n`, { mode: 0o600 });
}

function canonicalJSON(value) {
  if (value === null || typeof value === "boolean" || typeof value === "string") {
    return JSON.stringify(value);
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) fail("canonical JSON contains a non-finite number");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map((item) => canonicalJSON(item)).join(",")}]`;
  }
  if (typeof value === "object") {
    const entries = Object.keys(value).sort().map(
      (key) => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`,
    );
    return `{${entries.join(",")}}`;
  }
  fail("canonical JSON contains an unsupported value");
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

function decodeCompactJWS(path) {
  const compact = readFileSync(path, "utf8").trim();
  const parts = compact.split(".");
  if (parts.length !== 3 || parts.some((part) => !/^[A-Za-z0-9_-]+$/.test(part))) {
    fail("compact JWS is invalid");
  }
  return {
    header: JSON.parse(Buffer.from(parts[0], "base64url").toString("utf8")),
    payload: JSON.parse(Buffer.from(parts[1], "base64url").toString("utf8")),
  };
}

function sha256JSON(value) {
  return createHash("sha256").update(JSON.stringify(value)).digest("hex");
}

function signCanonicalES256(payload, protectedHeader, privateJWK) {
  const encodedHeader = encode(canonicalJSON(protectedHeader));
  const encodedPayload = encode(canonicalJSON(payload));
  const signingInput = `${encodedHeader}.${encodedPayload}`;
  const signature = sign("sha256", Buffer.from(signingInput), {
    key: createPrivateKey({ key: privateJWK, format: "jwk" }),
    dsaEncoding: "ieee-p1363",
  });
  if (signature.length !== 64) fail("ES256 signature is invalid");
  return `${signingInput}.${signature.toString("base64url")}`;
}

function validateAggregate(path, maximumRecords) {
  const document = JSON.parse(readFileSync(path, "utf8"));
  const keys = Object.keys(document);
  if (JSON.stringify(keys.sort()) !== JSON.stringify(["digest_sha256", "generation", "records", "schema_version"]) ||
      document.schema_version !== 1 || !Number.isSafeInteger(document.generation) || document.generation < 1 ||
      document.records === null || typeof document.records !== "object" || Array.isArray(document.records) ||
      Object.keys(document.records).length > maximumRecords || !/^[a-f0-9]{64}$/.test(document.digest_sha256)) {
    fail("exact Secret aggregate is invalid");
  }
  for (const [ref, record] of Object.entries(document.records)) {
    if (!ref || ref.length > 512 || /[\0\r\n]/.test(ref) || record === null || typeof record !== "object" ||
        !Number.isSafeInteger(record.version) || record.version < 1 ||
        !["ACTIVE", "REVOKED"].includes(record.status) || !/^[a-f0-9]{64}$/.test(record.content_sha256)) {
      fail("exact Secret aggregate record is invalid");
    }
    const recordKeys = Object.keys(record).sort();
    const expectedKeys = record.status === "ACTIVE" ? ["content_sha256", "status", "value", "version"] : ["content_sha256", "status", "version"];
    if (JSON.stringify(recordKeys) !== JSON.stringify(expectedKeys)) fail("exact Secret aggregate record key set is invalid");
    if (record.status === "ACTIVE") {
      const value = typeof record.value === "string" ? Buffer.from(record.value, "base64") : Buffer.alloc(0);
      if (typeof record.value !== "string" || record.value.length === 0 ||
          !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(record.value) ||
          value.toString("base64") !== record.value || value.length === 0 || value.length > 64 << 10 ||
          createHash("sha256").update(value).digest("hex") !== record.content_sha256) {
        fail("exact Secret aggregate record digest is invalid");
      }
    } else if (record.value !== undefined || record.content_sha256 !== "0".repeat(64)) {
      fail("exact Secret aggregate revoked record is invalid");
    }
  }
  const expectedDigest = sha256JSON({
    schema_version: document.schema_version,
    generation: document.generation,
    records: document.records,
  });
  if (expectedDigest !== document.digest_sha256) fail("exact Secret aggregate digest is invalid");
  return document;
}

const [command, ...args] = process.argv.slice(2);
switch (command) {
  case "validate-authority-bootstrap": {
    if (args.length !== 4) fail("validate-authority-bootstrap requires four input paths");
    const manifestSigner = canonicalPrivateJWK(JSON.parse(readFileSync(args[0], "utf8")));
    const manifestTrust = decodeCompactJWS(args[1]);
    const readbackSigner = canonicalPrivateJWK(JSON.parse(readFileSync(args[2], "utf8")));
    const readbackTrust = decodeCompactJWS(args[3]);
    const manifestCurrent = Array.isArray(manifestTrust.payload.keys)
      ? manifestTrust.payload.keys.filter((key) => key.status === "CURRENT") : [];
    const readbackCurrent = Array.isArray(readbackTrust.payload.keys)
      ? readbackTrust.payload.keys.filter((key) => key.status === "CURRENT") : [];
    if (manifestTrust.header.alg !== "ES256" || manifestCurrent.length !== 1 ||
        manifestCurrent[0].kid !== manifestSigner.kid || manifestCurrent[0].generation !== 1 ||
        readbackTrust.header.alg !== "ES256" || readbackCurrent.length !== 1 ||
        readbackCurrent[0].kid !== readbackSigner.kid ||
        readbackCurrent[0].credential_signer_generation !== 1) {
      fail("authority signer and independently signed trust material do not match");
    }
    break;
  }
  case "generate-empty-aggregate": {
    if (args.length !== 1) fail("generate-empty-aggregate requires an output path");
    const document = { schema_version: 1, generation: 1, records: {} };
    writeJSON(args[0], { ...document, digest_sha256: sha256JSON(document) });
    break;
  }
  case "validate-aggregate": {
    if (args.length !== 2 || !/^[1-9][0-9]*$/.test(args[1])) fail("validate-aggregate requires input and maximum record count");
    validateAggregate(args[0], Number(args[1]));
    break;
  }
  case "validate-git-aggregate": {
    if (args.length !== 2) fail("validate-git-aggregate requires aggregate and catalog paths");
    const document = validateAggregate(args[0], 32);
    const catalog = JSON.parse(readFileSync(args[1], "utf8"));
    if (catalog.version !== 1 || !Array.isArray(catalog.sources) || catalog.sources.length === 0 || catalog.sources.length > 32) {
      fail("Git source catalog is invalid");
    }
    const expected = new Map();
    for (const source of catalog.sources) {
      if (typeof source.credential_secret_ref !== "string" || !Number.isSafeInteger(source.credential_binding_version) ||
          source.credential_binding_version < 1 || expected.has(source.credential_secret_ref)) fail("Git credential registry is invalid");
      expected.set(source.credential_secret_ref, source.credential_binding_version);
    }
    if (Object.keys(document.records).length !== expected.size) fail("Git credential aggregate is incomplete");
    for (const [ref, record] of Object.entries(document.records)) {
      if (record.status !== "ACTIVE" || record.version !== expected.get(ref)) fail("Git credential aggregate registry mismatch");
    }
    break;
  }
  case "validate-oidc-snapshot": {
    if (args.length !== 3) fail("validate-oidc-snapshot requires snapshot, SHA-256 and generation paths");
    const snapshot = JSON.parse(readFileSync(args[0], "utf8"));
    const generation = readFileSync(args[2], "utf8").trim();
    if (JSON.stringify(Object.keys(snapshot).sort()) !== JSON.stringify(["algorithms", "audience", "digest_sha256", "generation", "issuer", "jwks", "schema_version"]) ||
        snapshot.schema_version !== 1 || !Number.isSafeInteger(snapshot.generation) || snapshot.generation < 1 ||
        String(snapshot.generation) !== generation || snapshot.issuer !== "https://sso.kodex.works/realms/mattercodex" ||
        snapshot.audience !== "mattercodex-integration-gateway" || JSON.stringify(snapshot.algorithms) !== '["RS256"]' ||
        snapshot.jwks === null || !Array.isArray(snapshot.jwks.keys) || snapshot.jwks.keys.length < 1 || snapshot.jwks.keys.length > 16) {
      fail("OIDC provider snapshot binding is invalid");
    }
    const seen = new Set();
    const keys = snapshot.jwks.keys.map((key) => {
      if (key === null || typeof key !== "object" || key.kty !== "RSA" || key.alg !== "RS256" || key.use !== "sig" ||
          typeof key.kid !== "string" || key.kid.length === 0 || seen.has(key.kid) ||
          typeof key.n !== "string" || key.n.length < 342 || key.e !== "AQAB" ||
          key.d !== undefined || key.x5u !== undefined || key.x5c !== undefined) fail("OIDC provider JWK is not permitted");
      seen.add(key.kid);
      return { use: key.use, kty: key.kty, kid: key.kid, alg: key.alg, n: key.n, e: key.e };
    });
    const digest = sha256JSON({
      schema_version: snapshot.schema_version,
      generation: snapshot.generation,
      issuer: snapshot.issuer,
      audience: snapshot.audience,
      algorithms: snapshot.algorithms,
      jwks: { keys },
    });
    if (snapshot.digest_sha256 !== digest || readFileSync(args[1], "utf8").trim() !== digest) {
      fail("OIDC provider snapshot digest or generation rollback rejected");
    }
    break;
  }
  case "generate-jwk": {
    if (args.length !== 1) fail("generate-jwk requires an output path");
    const { privateKey } = generateKeyPairSync("ec", { namedCurve: "prime256v1" });
    const privateJWK = privateKey.export({ format: "jwk" });
    writeJSON(args[0], canonicalPrivateJWK(privateJWK));
    break;
  }
  case "generate-restore-role-trust": {
    if (args.length !== 7) {
      fail("generate-restore-role-trust requires output, manifest signer, CURRENT signer, NEXT signer, source revision, manifest signer generation and validity seconds");
    }
    const [output, manifestPath, currentPath, nextPath, sourceRaw, manifestGenerationRaw, validityRaw] = args;
    const sourceRevision = Number(sourceRaw);
    const manifestSignerGeneration = Number(manifestGenerationRaw);
    const validitySeconds = Number(validityRaw);
    if (!Number.isSafeInteger(sourceRevision) || sourceRevision !== 1 ||
        !Number.isSafeInteger(manifestSignerGeneration) || manifestSignerGeneration < 1 ||
        !Number.isSafeInteger(validitySeconds) || validitySeconds < 86400 || validitySeconds > 366 * 86400) {
      fail("restore role trust lifecycle parameters are invalid");
    }
    const manifestSigner = canonicalPrivateJWK(JSON.parse(readFileSync(manifestPath, "utf8")));
    const current = canonicalPublicJWK(JSON.parse(readFileSync(currentPath, "utf8")));
    const next = canonicalPublicJWK(JSON.parse(readFileSync(nextPath, "utf8")));
    if (current.kid === next.kid) fail("restore role CURRENT and NEXT signers must differ");
    const publishedAt = Math.floor(Date.now() / 1000);
    const notBefore = publishedAt - 300;
    const notAfter = publishedAt + validitySeconds;
    const keys = [
      {
        status: "CURRENT",
        credential_signer_generation: 1,
        kid: current.kid,
        public_jwk: current,
        jwk_thumbprint_sha256: current.kid,
        not_before: notBefore,
        not_after: notAfter,
      },
      {
        status: "NEXT",
        credential_signer_generation: 2,
        kid: next.kid,
        public_jwk: next,
        jwk_thumbprint_sha256: next.kid,
        not_before: notBefore,
        not_after: notAfter,
      },
    ];
    const payload = {
      v: 1,
      iss: "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-publisher",
      aud: "urn:mattercodex:internal-rpc-authority-restore-controller",
      source_revision: sourceRevision,
      key_set_revision: sourceRevision,
      trust_set_digest_sha256: createHash("sha256").update(canonicalJSON(keys)).digest("hex"),
      predecessor: { revision: 0, digest_sha256: "0".repeat(64) },
      history: [],
      manifest_signer_generation: manifestSignerGeneration,
      keys,
      published_at: publishedAt,
      valid_until: notAfter,
    };
    const compact = signCanonicalES256(payload, {
      alg: "ES256",
      crit: ["mcxv"],
      kid: manifestSigner.kid,
      mcxv: 1,
      typ: "mattercodex-internal-rpc-restore-role-trust+jws",
    }, manifestSigner);
    writeFileSync(output, `${compact}\n`, { mode: 0o600 });
    break;
  }
  case "validate-private-jwk": {
    if (args.length !== 1) fail("validate-private-jwk requires an input path");
    validatePrivateJWK(args[0]);
    break;
  }
  case "canonicalize-json": {
    if (args.length !== 2) fail("canonicalize-json requires input and output paths");
    const value = JSON.parse(readFileSync(args[0], "utf8"));
    writeFileSync(args[1], canonicalJSON(value), { mode: 0o600 });
    break;
  }
  case "validate-canonical-json": {
    if (args.length !== 1) fail("validate-canonical-json requires an input path");
    const raw = readFileSync(args[0], "utf8");
    if (raw !== canonicalJSON(JSON.parse(raw))) fail("JSON document is not canonical");
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
    const ed25519Profile = header.alg === "EdDSA" &&
      typeof header.kid === "string" && header.kid.length > 0;
    const authorityES256Profile = header.alg === "ES256" &&
      typeof header.kid === "string" && header.kid.length > 0 &&
      typeof header.typ === "string" && header.typ.startsWith("mattercodex-internal-rpc-") &&
      JSON.stringify(header.crit) === '["mcxv"]' && header.mcxv === 1;
    if (!ed25519Profile && !authorityES256Profile) fail("compact JWS header is invalid");
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
    if (args.length !== 5) fail("validate-nats-server requires operator, system account and application account paths");
    const operator = decodeJWTFile(args[0]);
    const systemAccount = decodeJWTFile(args[1]);
    const systemAccountPublic = readFileSync(args[2], "utf8").trim();
    const account = decodeJWTFile(args[3]);
    const accountPublic = readFileSync(args[4], "utf8").trim();
    if (operator.nats?.type !== "operator" || systemAccount.nats?.type !== "account" ||
        operator.nats?.system_account !== systemAccountPublic || systemAccount.sub !== systemAccountPublic ||
        systemAccount.iss !== operator.sub || account.nats?.type !== "account" || account.sub !== accountPublic ||
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
