import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';

const fail=ok=>{if(!ok)throw Error('BACKUP_MAINTENANCE_POSTGRES_EXECUTION_CHANGED');};
let canonical;
// Источник — существующий Git-owned StatefulSet, а не снимаемый с live Pod
// baseline. Safe YAML parser разбирает только публичный manifest с Kubernetes
// YAML1.1 octal modes (0400); Python работает без PYTHONPATH/user site.
export function canonicalPostgresBackupSpec(){
 if(!canonical){
  const raw=readFileSync(new URL('../../deploy/k8s/base/platform-state/postgresql.yaml',import.meta.url));
  let documents;
  try{documents=JSON.parse(execFileSync('python3',['-I','-c','import json,sys,yaml; print(json.dumps(list(yaml.safe_load_all(sys.stdin))))'],{input:raw,encoding:'utf8',timeout:10000,maxBuffer:1<<20,stdio:['pipe','pipe','pipe']}));}
  catch{throw Error('BACKUP_MAINTENANCE_POSTGRES_SOURCE_INVALID');}
  const found=documents.filter(d=>d.kind==='StatefulSet'&&d.metadata?.name==='kodex-postgresql');
  fail(found.length===1);canonical=found[0].spec.template.spec;
 }
 return structuredClone(canonical);
}

// Допускаются только значения штатных API defaults; дополнительные поля
// container, hooks, env/commands или mounts не удаляются при сравнении.
function normalizeContainer(value){
 const result={terminationMessagePath:'/dev/termination-log',terminationMessagePolicy:'File',...structuredClone(value)};
 for(const key of ['startupProbe','readinessProbe','livenessProbe'])if(result[key])
  result[key]={timeoutSeconds:1,periodSeconds:10,successThreshold:1,failureThreshold:3,...result[key]};
 return result;
}
export function postgresBackupBoundary(spec,kind){
 const expected=canonicalPostgresBackupSpec();
 fail(['Pod','StatefulSet'].includes(kind)&&spec&&Array.isArray(spec.containers)&&spec.containers.length===1&&
  spec.containers[0].name==='postgresql'&&!(spec.initContainers?.length)&&!(spec.ephemeralContainers?.length)&&
  !spec.hostPID&&!spec.hostIPC&&!spec.hostNetwork&&!spec.shareProcessNamespace&&
  spec.automountServiceAccountToken===false&&spec.enableServiceLinks===false&&
  (!spec.serviceAccountName||spec.serviceAccountName==='default')&&(!spec.serviceAccount||spec.serviceAccount==='default'));
 // Существующий observability_profile.py допускает единственную добавку
 // OTEL_SDK_DISABLED=true. Это закрытый второй render, не произвольный env.
 const containers=[normalizeContainer(expected.containers[0]),normalizeContainer({...expected.containers[0],env:[...expected.containers[0].env,{name:'OTEL_SDK_DISABLED',value:'true'}]})];
 fail(containers.some(container=>fingerprint(normalizeContainer(spec.containers[0]))===fingerprint(container))&&
  fingerprint(spec.securityContext)===fingerprint(expected.securityContext));
 const volumes=structuredClone(expected.volumes);
 if(kind==='Pod')volumes.push({name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}});
 const order=items=>[...items].sort((a,b)=>a.name.localeCompare(b.name));
 fail(Array.isArray(spec.volumes)&&fingerprint(order(spec.volumes))===fingerprint(order(volumes)));
 // Persisted inventory содержит только hash проверенного публичного профиля,
 // не container env, аргументы или значения credentials.
 return fingerprint({containers,securityContext:expected.securityContext,volumes:order(expected.volumes)});
}

export function canonicalPostgresBackupBoundarySHA256(){return postgresBackupBoundary(canonicalPostgresBackupSpec(),'StatefulSet');}
