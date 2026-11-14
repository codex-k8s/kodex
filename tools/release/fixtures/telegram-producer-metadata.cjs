// Backend выводит только реально запрошенные встроенные kinds. Этот фильтр
// не даёт старому RS/Pod scan незаметно получить pending RC/Job из fake API.
module.exports=(items,requested)=>{
 const names={deployments:'Deployment',statefulsets:'StatefulSet',daemonsets:'DaemonSet',replicasets:'ReplicaSet',replicationcontrollers:'ReplicationController',jobs:'Job',cronjobs:'CronJob',pods:'Pod'};
 const kinds=new Set(requested.split(',').map(name=>names[name]));
 const refs=spec=>{
  const values=[[],[],[],[],[]];
  const visit=node=>{if(!node||typeof node!=='object')return;
   for(const [key,value] of Object.entries(node)){
    if(key==='secretName'&&typeof value==='string')values[0].push(value);
    if(key==='secretKeyRef'&&value?.name)values[1].push(value.name);
    if(key==='secretRef'&&value?.name)values[2].push(value.name);
    if(key==='secret'&&value?.name)values[3].push(value.name);
    if(key==='imagePullSecrets'&&Array.isArray(value))values[4].push(...value.map(item=>item.name));
    visit(value);
   }
  };visit(spec);return values.map(names=>names.join(' '));
 };

 return items.filter(item=>kinds.has(item.kind)).map(item=>[item.kind,item.metadata?.name,item.metadata?.uid,item.metadata?.namespace,
  item.metadata?.generation,item.metadata?.labels?.['app.kubernetes.io/name'],JSON.stringify(item.metadata?.ownerReferences??[]),
  item.spec?.serviceAccountName,item.spec?.template?.spec?.serviceAccountName,item.spec?.jobTemplate?.spec?.template?.spec?.serviceAccountName,
  item.spec?.template?.metadata?.labels?.['app.kubernetes.io/name'],item.spec?.jobTemplate?.spec?.template?.metadata?.labels?.['app.kubernetes.io/name'],
  item.spec?.replicas,item.status?.replicas,item.status?.observedGeneration,...refs(item.spec)].map(value=>value===undefined?'':value).join('\t')).join('\n')+(items.length?'\n':'');
};
