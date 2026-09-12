import { createHash } from "node:crypto";

const issuerPlaceholder = "__KODEX_OIDC_ISSUER__";
const digest = (raw) => createHash("sha256").update(raw).digest("hex");
function requireValue(ok, code) { if (!ok) throw new Error(code); }

// План получает public OIDC config только из выбранного CP, а не из свободного
// CLI значения. Все ссылки сохраняются для повторного readback перед CAS.
export function readAuthorityPolicyEnvironment(deployment, configMaps) {
  requireValue(deployment?.kind === "Deployment" && deployment.metadata?.name === "control-plane" && deployment.metadata.namespace === "kodex-system" && deployment.metadata.uid && deployment.metadata.resourceVersion && deployment.metadata.labels?.["app.kubernetes.io/part-of"] === "kodex" && deployment.metadata.labels?.["kodex.dev/environment"] !== "production" && (deployment.metadata.labels?.["kodex.dev/environment"] === "staging" || deployment.metadata.labels?.["kodex.dev/local-profile"] === "hot-reload"),"AUTHORITY_POLICY_CP_IDENTITY_REJECTED");
  const containers = deployment.spec?.template?.spec?.containers?.filter(c=>c.name==="control-plane");
  requireValue(containers?.length === 1 && Array.isArray(configMaps),"AUTHORITY_POLICY_CP_CONFIG_REJECTED");
  const evidence = {deploymentUID:deployment.metadata.uid,resourceVersion:deployment.metadata.resourceVersion,configMaps:[]};
  const values = {};
  for (const [name, fallback] of [["CONTROL_PLANE_OIDC_ISSUER",null],["CONTROL_PLANE_OIDC_AUDIENCE","kodex-control-api"]]) {
    const fields = (containers[0].env??[]).filter(e=>e.name===name);
    requireValue(fields.length<=1,"AUTHORITY_POLICY_DUPLICATE_CONFIG");
    if (fields.length===0) { requireValue(fallback!==null && (containers[0].envFrom??[]).length===0,"AUTHORITY_POLICY_CONFIG_MISSING");values[name]=fallback;continue; }
    const field=fields[0];
    if (typeof field.value==="string" && field.valueFrom===undefined) { values[name]=field.value;continue; }
    const ref=field.valueFrom?.configMapKeyRef;
    requireValue(ref && typeof ref.name==="string" && typeof ref.key==="string" && ref.optional!==true && field.value===undefined,"AUTHORITY_POLICY_CONFIG_REFERENCE_REJECTED");
    const matches=configMaps.filter(cm=>cm.metadata?.name===ref.name && cm.metadata.namespace==="kodex-system");
    requireValue(matches.length===1,"AUTHORITY_POLICY_CONFIG_REFERENCE_AMBIGUOUS");
    const cm=matches[0];
    requireValue(cm.kind==="ConfigMap" && cm.metadata.uid && cm.metadata.resourceVersion && typeof cm.data?.[ref.key]==="string","AUTHORITY_POLICY_CONFIG_REFERENCE_MISSING");
    values[name]=cm.data[ref.key];
    evidence.configMaps.push({name:ref.name,key:ref.key,uid:cm.metadata.uid,resourceVersion:cm.metadata.resourceVersion,valueSHA256:digest(values[name])});
  }
  return {oidcIssuer:values.CONTROL_PLANE_OIDC_ISSUER,oidcAudience:values.CONTROL_PLANE_OIDC_AUDIENCE,evidence};
}

export function verifyAuthorityPolicyEnvironmentReadback(expected,deployment,configMaps) {
  const actual=readAuthorityPolicyEnvironment(deployment,configMaps);
  requireValue(JSON.stringify(actual)===JSON.stringify(expected),"AUTHORITY_POLICY_CP_CONFIG_DRIFT");
  return actual;
}

// Environment должен происходить из readback выбранного CP deployment/config,
// закреплённого plan CAS. Эта функция не разрешает менять настройки контура.
export function renderAuthorityPolicyEnvironment(sourceRaw, environment) {
  let issuer;
  try { issuer = new URL(environment?.oidcIssuer); } catch { throw new Error("AUTHORITY_POLICY_OIDC_CONFIG_REQUIRED"); }
  requireValue(issuer.protocol === "https:" && issuer.hostname !== "" && issuer.username === "" && issuer.password === "" && issuer.search === "" && issuer.hash === "" && issuer.pathname === "/realms/kodex" && issuer.href === environment.oidcIssuer && environment.oidcAudience === "kodex-control-api", "AUTHORITY_POLICY_OIDC_CONFIG_REJECTED");
  requireValue(typeof sourceRaw === "string" && Buffer.byteLength(sourceRaw) <= 1 << 20, "AUTHORITY_POLICY_SOURCE_REJECTED");
  const source = JSON.parse(sourceRaw);
  const producers = source?.policy?.authority_proof_producers;
  requireValue(source.v === 1 && Array.isArray(producers), "AUTHORITY_POLICY_SOURCE_REJECTED");
  let count = 0;
  for (const producer of producers) {
    if (producer.caller_workload_id !== "control-api-gateway") continue;
    requireValue(producer.application_credential === "OIDC_BEARER" && producer.application_credential_metadata === "authorization" && producer.application_credential_audience === environment.oidcAudience && [issuerPlaceholder, environment.oidcIssuer].includes(producer.application_credential_issuer), "AUTHORITY_POLICY_OIDC_BINDING_REJECTED");
    producer.application_credential_issuer = environment.oidcIssuer;
    count++;
  }
  requireValue(count === 3, "AUTHORITY_POLICY_OIDC_COVERAGE_REJECTED");
  const renderedRaw = `${JSON.stringify(source,null,2)}\n`;
  requireValue(!/__KODEX_[A-Z_]+__/.test(renderedRaw), "AUTHORITY_POLICY_UNRESOLVED_TEMPLATE");
  return {raw:renderedRaw,sourceSHA256:digest(sourceRaw),renderedSHA256:digest(renderedRaw),oidcProducers:count};
}

// Проверка публикации отличается от проверки repository template: шаблон
// допустим в source checkout, но никогда не является runtime policy.
export function verifyAuthorityPolicyEnvironment(raw, environment) {
  const value = JSON.parse(raw);
  requireValue(!/__KODEX_[A-Z_]+__/.test(raw), "AUTHORITY_POLICY_UNRESOLVED_TEMPLATE");
  const rendered = renderAuthorityPolicyEnvironment(raw,environment);
  requireValue(JSON.stringify(JSON.parse(rendered.raw)) === JSON.stringify(value), "AUTHORITY_POLICY_ENVIRONMENT_DRIFT");
  return {sha256:digest(raw),oidcProducers:rendered.oidcProducers};
}
