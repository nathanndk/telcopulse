"""Exercise internal or PUBLIC_INCIDENT_API=1 gateway routes without exposing credentials."""
import os
import json
import subprocess
import uuid
from kafka_recovery import compose

# Capture only the needed credential and pass it over stdin, never command arguments.
public = os.environ.get("PUBLIC_INCIDENT_API") == "1"
token = "" if public else compose("exec", "-T", "incident-service", "printenv", "SERVICE_TOKEN")
mutation_token = "" if public else compose("exec", "-T", "incident-service", "printenv", "INCIDENT_OPERATOR_TOKEN")
program = r'''
let input='';
process.stdin.on('data', chunk => input+=chunk);
process.stdin.on('end', async () => {
 try {
  const {token,mutation_token,key,public}=JSON.parse(input);
  const headers={'Content-Type':'application/json','Idempotency-Key':key};
  if(!public){headers.Authorization='Bearer '+token;headers['X-Telcopulse-Mutation-Token']=mutation_token;}
  const call=async(method,path,body,status)=>{
   const response=await fetch((public?'http://web:3000/api/v1/incidents':'http://incident-service:8080/internal/incidents')+path,{method,headers,body:body?JSON.stringify(body):undefined});
   if(response.status!==status)throw new Error(method+' returned '+response.status+'; expected '+status);
   return response.json();
  };
  const fields={title:'Integration check: synthetic payment incident',severity:'SEV-4',owner:'local-test',impact:'Synthetic verification; no customer impact',root_cause:'Controlled integration scenario',mitigation:'Verification only',resolution:'Service checks completed',postmortem_notes:'Exercise completed without customer impact',evidence:[],action_items:[]};
  const creation={...fields,environment:'development',service:'payment-service'};
  let current=await call('POST','',creation,201);
  const replay=await call('POST','',creation,200);
  if(replay.id!==current.id)throw new Error('Creation replay changed identity');
  await call('POST','',{...creation,title:'Different input'},409);
  await call('PUT','/'+current.id,{...fields,state:'Resolved',expected_version:current.version,note:'Invalid state jump'},422);
  await call('GET','?limit=101',undefined,422);
  const listing=await call('GET','?environment=development&state=Detected&limit=100',undefined,200);
  if(!listing.items.some(item=>item.id===current.id))throw new Error('Created incident absent from filtered list');
  for(const state of ['Acknowledged','Investigating','Identified','Mitigating','Monitoring','Resolved','Postmortem']){
   current=await call('PUT','/'+current.id,{...fields,state,expected_version:current.version,note:'Synthetic lifecycle verification'},200);
  }
  await call('PUT','/'+current.id,{...fields,state:'Postmortem',expected_version:1,note:'Stale edit'},409);
  const detail=await call('GET','/'+current.id,undefined,200);
  if(detail.history.length!==8 || detail.incident.version!==8 || detail.incident.state!=='Postmortem')throw new Error('Audit history or state mismatch');
  if(!detail.incident.acknowledged_at || !detail.incident.resolved_at)throw new Error('Lifecycle timestamps missing');
  for(let i=0;i<3;i++)current=await call('PUT','/'+current.id,{...fields,state:'Postmortem',expected_version:current.version,note:'Pagination verification'},200);
  const first=await call('GET','/'+current.id,undefined,200);
  if(first.history.length!==10 || !first.history_more || first.history_next!==10)throw new Error('History was not bounded');
  const next=await call('GET','/'+current.id+'?history_after='+first.history_next,undefined,200);
  if(next.history.length!==1 || next.history[0].version!==11 || next.history_more)throw new Error('History cursor not forwarded');
  await call('GET','/'+current.id+'?history_after=-1',undefined,422);
  const page=await call('GET','?environment=development&limit=1',undefined,200);
  if(!page.more || !page.next)throw new Error('Missing list cursor');
  const older=await call('GET','?environment=development&limit=1&cursor='+page.next,undefined,200);
  if(older.items.length!==1 || older.items[0].id===page.items[0].id)throw new Error('List cursor not forwarded');
  if('evidence' in page.items[0])throw new Error('Unbounded details leaked into list');
  await call('GET','?environment=development&cursor=bad',undefined,422);
  console.log(JSON.stringify({id:current.id,state:current.state,audit_entries:11,history_pages:[first.history.length,next.history.length],list_cursor_verified:true}));
 }catch(error){console.error(error.message);process.exitCode=1;}
});
'''
subprocess.run(
    ["docker", "compose", "exec", "-T", "web", "node", "-e", program],
    input=json.dumps({"token": token, "mutation_token": mutation_token, "key": uuid.uuid4().hex, "public": public}), text=True, check=True,
)
