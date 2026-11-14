const fail=ok=>{if(!ok)throw Error('KUBERNETES_PORT_NAME_INVALID');};

// Совпадает с apimachinery IsValidPortName: IANA name, а не DNS label.
export function validKubernetesPortName(value){
 return typeof value==='string'&&value.length>0&&value.length<=15&&/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(value)&&/[a-z]/.test(value);
}

// Проверяется rendered source до нового migration plan, не только успешный YAML.
// Исторический readback не переписывает уже исполненный immutable plan.
export function validateKubernetesPortNames(resources){
 for(const resource of resources){
  if(resource.kind==='Service')for(const port of resource.spec?.ports??[]){
   if(port.name!==undefined)fail(typeof port.name==='string'&&port.name.length>0&&port.name.length<=63&&/^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/.test(port.name));
   if(typeof port.targetPort==='string')fail(validKubernetesPortName(port.targetPort));
  }
  const pod=resource.kind==='Pod'?resource.spec:resource.kind==='CronJob'?resource.spec?.jobTemplate?.spec?.template?.spec:resource.spec?.template?.spec;
  for(const container of [...(pod?.containers??[]),...(pod?.initContainers??[]),...(pod?.ephemeralContainers??[])]){
   const names=new Set();
   for(const port of container.ports??[])if(port.name!==undefined){fail(validKubernetesPortName(port.name)&&!names.has(port.name));names.add(port.name);}
  }
 }
}
