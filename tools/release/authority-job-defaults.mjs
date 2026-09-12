// Общие defaults миграции и наблюдения ротации фиксируются до intent/hash.
export function materializeAuthorityJobDefaults(spec) {
 // Материализуем defaults до intent/hash, а не удаляем их из readback.
 // Явные rendered values сохраняются; любое другое изменение spec — отказ.
 const absent=(value,key,fallback)=>{if(!Object.hasOwn(value,key))value[key]=fallback;};
 if(!Object.hasOwn(spec,'completions')&&!Object.hasOwn(spec,'parallelism'))spec.completions=1;
 for(const [key,value] of Object.entries({parallelism:1,completionMode:'NonIndexed',manualSelector:false,suspend:false,
  podReplacementPolicy:spec.podFailurePolicy?'Failed':'TerminatingOrFailed'}))absent(spec,key,value);
 const pod=spec.template.spec;
 for(const [key,value] of Object.entries({dnsPolicy:'ClusterFirst',schedulerName:'default-scheduler',
  serviceAccount:pod.serviceAccountName,terminationGracePeriodSeconds:30}))absent(pod,key,value);
 for(const container of [...pod.containers,...(pod.initContainers??[])]){
  if(!Object.hasOwn(container,'imagePullPolicy')){
   if(!/@sha256:[a-f0-9]{64}$/.test(container.image??''))throw new Error('SOURCE_MIGRATION_DEFAULT_IMAGE_UNPINNED');
   container.imagePullPolicy='IfNotPresent';
  }
  absent(container,'terminationMessagePath','/dev/termination-log');absent(container,'terminationMessagePolicy','File');
 }
 return spec;
}
