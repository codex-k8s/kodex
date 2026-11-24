import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {isTelegramAuthorityImage} from './telegram-introduction-contract.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
const rejected='TELEGRAM_RUNTIME_OBSERVABILITY_REJECTED';
const fail=ok=>{if(!ok)throw Error(rejected);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const modes=['ENABLED','DISABLED'];
const names=['interaction-gateway','internal-rpc-authority-issuer','platform-worker-grant-agent'];
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&same(Object.keys(value).sort(),[...keys].sort());

// Проекция не включает mutable restart annotations publisher. Явный mode
// подтверждается фактически обслуживаемым env, без inference при отказе.
export function telegramPublisherRuntimeObservability(publisher,mode,pullRegistry){
 fail(modes.includes(mode)&&publisher?.apiVersion==='apps/v1'&&publisher.kind==='Deployment'&&
  publisher.metadata?.name==='internal-rpc-authority-publisher'&&publisher.metadata.namespace==='kodex-system');
 const containers=publisher.spec?.template?.spec?.containers;
 fail(Array.isArray(containers)&&containers.filter(c=>c.name==='publisher').length===1);
 const container=containers.find(c=>c.name==='publisher'),env=container.env??[];
 fail(isTelegramAuthorityImage(container.image,pullRegistry));
 fail(Array.isArray(env));
 const selected=env.filter(e=>e?.name==='OTEL_SDK_DISABLED');
 fail(selected.length<=1&&(!selected.length||exact(selected[0],['name','value'])&&['true','false'].includes(selected[0].value)));
 fail(mode==='DISABLED'?selected[0]?.value==='true':!selected.length||selected[0].value==='false');
 return {mode,publisherRuntimeSHA256:fingerprint({name:container.name,image:container.image,otelSDKDisabled:selected[0]??null})};
}

export function requireTelegramRuntimeObservability(profile,publisher,pullRegistry){
 fail(exact(profile,['mode','publisherRuntimeSHA256'])&&/^[a-f0-9]{64}$/.test(profile.publisherRuntimeSHA256??'')&&
  same(profile,telegramPublisherRuntimeObservability(publisher,profile.mode,pullRegistry)));
 return profile;
}

// Единственный renderer остаётся canonical Python transform. Проверка результата
// ограничивает его этой Deployment и тремя long-running env; one-shot init,
// images, Secret references, probes и остальные поля не меняются.
// -B исключает запись import cache в закреплённый source checkout.
export function projectTelegramRuntimeObservability(deployment,mode){
 fail(modes.includes(mode)&&deployment?.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&
  deployment.metadata?.name==='interaction-gateway'&&deployment.metadata.namespace==='kodex-system');
 const pod=deployment.spec?.template?.spec,containers=pod?.containers,initializers=pod?.initContainers;
 fail(Array.isArray(containers)&&Array.isArray(initializers)&&!pod.ephemeralContainers?.length);
 const running=[...containers,...initializers.filter(c=>c.restartPolicy==='Always')];
 fail(running.length===3&&same(running.map(c=>c.name).sort(),[...names].sort())&&
  initializers.filter(c=>c.restartPolicy!=='Always').length===1&&initializers.find(c=>c.restartPolicy!=='Always').name==='internal-rpc-authority-socket-init');
 for(const container of running){
  const env=container.env??[];fail(Array.isArray(env)&&env.filter(e=>e?.name==='OTEL_SDK_DISABLED').length<=1);
  const selected=env.find(e=>e?.name==='OTEL_SDK_DISABLED');
  if(mode==='ENABLED')fail(!selected||exact(selected,['name','value'])&&selected.value==='false');
 }
 const input=JSON.stringify([deployment]);fail(Buffer.byteLength(input)<=1<<20);
 const script="import sys,json;sys.path.insert(0,sys.argv[1]);import observability_profile;print(json.dumps(observability_profile.transform(json.load(sys.stdin),disabled=sys.argv[2]=='DISABLED')))";
 let result;
 try{result=JSON.parse(execFileSync('python3',['-I','-B','-c',script,root+'tools/install',mode],{
  cwd:root,input,encoding:'utf8',timeout:10000,maxBuffer:2<<20,
  env:Object.fromEntries(['PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]])),stdio:['pipe','pipe','pipe']}));
 }catch{throw Error(rejected);}
 const expected=structuredClone(deployment);
 if(mode==='DISABLED')for(const container of [...expected.spec.template.spec.containers,...expected.spec.template.spec.initContainers.filter(c=>c.restartPolicy==='Always')])
  container.env=[...(container.env??[]).filter(e=>e.name!=='OTEL_SDK_DISABLED'),{name:'OTEL_SDK_DISABLED',value:'true'}];
 fail(Array.isArray(result)&&result.length===1&&same(result[0],expected));
 return result[0];
}
