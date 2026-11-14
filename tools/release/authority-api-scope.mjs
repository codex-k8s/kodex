import {fingerprint} from './scoped-release.mjs';

const same=(left,right)=>fingerprint(left)===fingerprint(right);
const identity=row=>`${row.apiVersion}|${row.kind}|${row.name}`;
const api='apps/v1|Deployment|control-api-gateway';
const service='v1|Service|control-api-gateway';
const names=new Set([api,service]);
const authorityNames=new Set([...names,'apps/v1|Deployment|control-plane','apps/v1|Deployment|internal-rpc-authority-publisher',
 'v1|ConfigMap|internal-rpc-authority-publisher-target-registry']);

// Закрытие API доказывает только свой exact API/Service boundary. Полномочия
// publisher/registry/CP отдельно доказывает полный authority target той же установки.
export function requireAuthorityAPIScope(apiTarget,authorityTarget){
 const fail=()=>{throw Error('CATALOG_API_SCOPE_MISMATCH');};
 if(!apiTarget||!authorityTarget||!Array.isArray(apiTarget.resources)||!Array.isArray(authorityTarget.resources))fail();
 const seal=target=>Object.fromEntries(Object.entries(target).filter(([key])=>key!=='resources'));
 if(!same(seal(apiTarget),seal(authorityTarget)))fail();
 if(authorityTarget.resources.length!==5||new Set(authorityTarget.resources.map(identity)).size!==5||
  !authorityTarget.resources.every(row=>authorityNames.has(identity(row))))fail();
 const expected=authorityTarget.resources.filter(row=>names.has(identity(row)));
 if(expected.length!==2||new Set(expected.map(identity)).size!==2)fail();
 if(same(apiTarget,authorityTarget))return;
 if(apiTarget.resources.length!==2||new Set(apiTarget.resources.map(identity)).size!==2||
  !apiTarget.resources.every(row=>names.has(identity(row))&&expected.some(want=>same(row,want))))fail();
}
