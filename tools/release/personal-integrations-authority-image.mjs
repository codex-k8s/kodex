import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {validatedStablePublication} from './stable-image-publication.mjs';
import {qualifyComponentNodeImages} from './component-node-qualification.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};

// Sidecars получают отдельную exact публикацию authority из того же source049.
// Fresh CRI readback использует существующий режим readback без pull. Ни digest
// сам по себе, ни прежний publisher image не заменяют publication/node custody.
export async function readPersonalIntegrationsAuthorityImage({sourceSHA,target,kubeconfig,publicationPlanPath,publicationEvidencePath,nodeProofPath},io={}){
 const publicationPlan=readAuthorityPrivateJSON(publicationPlanPath);
 const publicationEvidence=readAuthorityPrivateText(publicationEvidencePath).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
 const evidence={publicationPlan,publicationEvidence},imageRef=validatedStablePublication(sourceSHA,evidence,'internal-rpc-authority');
 fail(publicationPlan.context===target.context&&publicationPlan.clusterUID===target.clusterUID&&publicationPlan.namespaceUID===target.namespaceUID,
  'PERSONAL_INTEGRATIONS_AUTHORITY_IMAGE_TARGET_REJECTED');
 const nodes=readAuthorityPrivateJSON(nodeProofPath);
 fail(Array.isArray(nodes.binding)&&nodes.binding.length>0&&Array.isArray(nodes.images)&&nodes.images.length===1&&nodes.images[0].imageRef===imageRef,
  'PERSONAL_INTEGRATIONS_AUTHORITY_NODE_PROOF_REQUIRED');
 const pinned=nodes.images[0].nodes;
 fail(Array.isArray(pinned)&&pinned.length===nodes.binding.length,'PERSONAL_INTEGRATIONS_AUTHORITY_NODE_PROOF_REQUIRED');
 const nodeTarget={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,
  nodes:pinned.map(({name,uid})=>({name,uid}))};
 const fresh=await qualifyComponentNodeImages({target:nodeTarget,kubeconfig,images:[imageRef],mode:'readback',expectedBinding:nodes.binding},io.operation);
 fail(fingerprint(fresh)===fingerprint(nodes),'PERSONAL_INTEGRATIONS_AUTHORITY_NODE_PROOF_CHANGED');
 return {schema:'kodex.personal-integrations-authority-image/v1',sourceSHA,imageRef,
  publicationSHA256:fingerprint(evidence),nodeProofSHA256:fingerprint(nodes)};
}
