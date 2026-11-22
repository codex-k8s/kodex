import {authorityPostgresIdentity,authorityPostgresObservedIdentity,readCatalogAuthorityBaselinePaired} from './catalog-authority-baseline.mjs';
import {pairedTokenOwnerDatabase} from './paired-token-owner-database.mjs';
import {readPairedTokenReleaseOwner,validatePairedTokenReleaseManifest,verifyPairedTokenReleaseOwner} from './paired-token-release-plan.mjs';
import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';

const boundaries=new WeakMap();
const ensure=(value,code)=>{if(!value)throw Error(code);};

// Все SQL идут через существующий exact Unix-socket owner adapter. Caller
// передаёт original rotation receipts, но не SQL, DSN, backend или готовый proof.
// Reader не создаёт Jobs, не продвигает Goose и не переигрывает rotation.
export function readPairedTokenReleaseOwnerFromDatabase(manifest,{plan,events},kube,{now=Date.now,readPublisher}={}){
 validatePairedTokenReleaseManifest(manifest);
 const postgresIdentity=()=>authorityPostgresIdentity(kube),postgres=postgresIdentity();
 const database=pairedTokenOwnerDatabase(kube,postgres);
 let authority,controlPlane;

 const owner=readPairedTokenReleaseOwner(manifest,{
  now,
  readControlPlane:()=>{controlPlane=database.readControlPlane();return {postgres,backend:controlPlane.backend,proof:controlPlane.proof};},
  // Head и status читаются вместе именно в прежней SQL-точке baseline после
  // registry/publisher checks. Повторный port не читает и не remint старый proof.
  readAuthorityHead:()=>{authority=readCatalogAuthorityBaselinePaired({plan,events,policyRevision:110,now},kube,database.readAuthority,{readPublisher});
   return {...authority.head,postgres:authority.postgres,backend:authority.backend};},
  // Closure выполняется после late registry/publisher/Pod readback и ДО seal:
  // первоначальный 30s budget охватывает final backend epoch и оба PG snapshots.
  readAuthority:()=>{database.verifyEpoch(controlPlane.backend,authority.backend,postgresIdentity);return authority.baseline;},
 });
 boundaries.set(owner,{...authority.boundary,target:structuredClone(plan.policyTransition.targetProfile)});
 return owner;
}

// Объединённый нативный снимок получает publisher pins только у исходного
// sealed owner. Десериализованный proof не позволяет подменить target.
export function pairedTokenReleasePublisherPin(manifest,owner){
 verifyPairedTokenReleaseOwner(manifest,owner);
 const boundary=boundaries.get(owner);
 ensure(boundary?.showManagedFields,'PAIRED_TOKEN_RELEASE_OWNER_BOUNDARY_REQUIRED');
 return Object.freeze({target:structuredClone(boundary.target),deploymentSHA256:boundary.publisherSHA256});
}

// Финальное чтение после свежего API observation возвращает существующий
// sealed owner без обновления его срока. Полный SQL каталог уже подтверждён.
export async function recheckPairedTokenReleaseOwnerEpoch(manifest,owner,kube,client,budget){
 verifyPairedTokenReleaseOwner(manifest,owner);
 const boundary=boundaries.get(owner);
 ensure(boundary?.showManagedFields,'PAIRED_TOKEN_RELEASE_OWNER_BOUNDARY_REQUIRED');
 const {controlPlane,authority}=owner,postgres=controlPlane.postgres;
 const unchanged=async()=>{
  const {observed,deployment,registry,pod,statefulSet}=await client.readPairedOwnerBoundary(budget);
  const target=portableRunnerTarget(boundary.target,observed);
  ensure(target.allows(deployment)&&target.allows(registry)&&fingerprint(registry)===boundary.registrySHA256&&
   fingerprint(deployment)===boundary.publisherSHA256&&fingerprint(authorityPostgresObservedIdentity(pod,statefulSet))===fingerprint(postgres),
   'PAIRED_TOKEN_RELEASE_OWNER_BOUNDARY_CHANGED');
 };
 await unchanged();
 pairedTokenOwnerDatabase(kube,postgres).readEpoch(controlPlane.backend,authority.postgresBackend);
 await unchanged();
 return verifyPairedTokenReleaseOwner(manifest,owner);
}
