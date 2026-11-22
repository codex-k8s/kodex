import {fingerprint} from './scoped-release.mjs';
import {isPersonalIntegrationsMigrationProfile} from './personal-integrations-migration-contract.mjs';

const names=['internal-rpc-authority-issuer','platform-worker-grant-agent'];
const initName='internal-rpc-authority-socket-init';
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const absentDefault=(value,key,expected)=>{if(!Object.hasOwn(value,key))value[key]=expected;};

// Planned bytes остаются неизменными. Только три добавленных immutable
// контейнера получают точную прогнозируемую API default-проекцию; поля actual
// никогда не удаляются, а явно заданные поля expected не переписываются.
export function personalIntegrationsArtifactAPISpec({profile,name,beforeSpec,expectedSpec,authorityImage}){
 if(!isPersonalIntegrationsMigrationProfile(profile)||name!=='artifact-retention')return expectedSpec;
 const before=beforeSpec?.template?.spec,pod=expectedSpec?.template?.spec;
 if(before?.containers?.length!==1||before.containers[0].name!=='artifact-retention'||
  (before.initContainers?.length??0)!==0||pod?.containers?.length!==3||pod.initContainers?.length!==1||
  !/^.+@sha256:[a-f0-9]{64}$/.test(authorityImage??'')||
  !names.every(n=>pod.containers.filter(c=>c.name===n&&c.image===authorityImage).length===1)||
  pod.initContainers[0].name!==initName||pod.initContainers[0].image!==authorityImage)return expectedSpec;
 const result=structuredClone(expectedSpec),out=result.template.spec;
 for(const c of [...out.containers.filter(c=>names.includes(c.name)),...out.initContainers]){
  absentDefault(c,'imagePullPolicy','IfNotPresent');
  absentDefault(c,'terminationMessagePath','/dev/termination-log');
  absentDefault(c,'terminationMessagePolicy','File');
  for(const e of c.env??[])if(e.valueFrom?.fieldRef)absentDefault(e.valueFrom.fieldRef,'apiVersion','v1');
  for(const key of ['livenessProbe','readinessProbe','startupProbe'])if(c[key]){
   if(c[key].httpGet)absentDefault(c[key].httpGet,'scheme','HTTP');
   absentDefault(c[key],'successThreshold',1);absentDefault(c[key],'failureThreshold',3);
  }
  if(c.name===initName)absentDefault(c,'resources',{});
 }
 return result;
}

// Допускаются только исходный exact spec либо полная точная API-проекция.
// Ни неполный default-набор, ни лишнее поле, ни изменение старого main не проходят.
export function personalIntegrationsArtifactSpecMatches(input,actual){
 return same(input.expectedSpec,actual)||same(personalIntegrationsArtifactAPISpec(input),actual);
}
