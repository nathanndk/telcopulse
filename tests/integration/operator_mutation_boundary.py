"""Verify sibling services' shared token cannot mutate operator resources."""

import json
import subprocess
import uuid

from kafka_recovery import compose


shared = compose("exec", "-T", "incident-service", "printenv", "SERVICE_TOKEN")
program = r'''
let input='';
process.stdin.on('data', chunk => input+=chunk);
process.stdin.on('end', async () => {
 try {
  const {token,key}=JSON.parse(input);
  const headers={'Authorization':'Bearer '+token,'Content-Type':'application/json','Idempotency-Key':key,
   'X-Operator-Actor':'operator:USR-0123456789abcdef01234567','X-Operator-Role':'Administrator'};
  const requests=[
   ['POST','http://incident-service:8080/internal/incidents',{title:'Must not be created',severity:'SEV-4',environment:'development',service:'payment-service'},401],
   ['PUT','http://incident-service:8080/internal/incidents/INC-0123456789abcdef01234567',{state:'Acknowledged'},401],
   ['POST','http://simulation-service:8080/internal/simulations',{scenario:'payment-decline',environment:'development',percentage:1,duration_seconds:30,reason:'Must not start'},401],
   ['POST','http://simulation-service:8080/internal/simulations/SIM-0123456789abcdef01234567/stop',{reason:'Must not stop'},401],
   ['GET','http://incident-service:8080/internal/incidents?environment=development',undefined,200],
   ['GET','http://simulation-service:8080/internal/simulations?environment=development',undefined,200],
  ];
  for(const [method,url,body,want] of requests){
   const response=await fetch(url,{method,headers,body:body?JSON.stringify(body):undefined});
   if(response.status!==want)throw new Error(method+' '+url+' returned '+response.status+'; expected '+want);
  }
  console.log(JSON.stringify({shared_token_mutations:'denied',shared_token_reads:'allowed'}));
 }catch(error){console.error(error.message);process.exitCode=1;}
});
'''
subprocess.run(
    ["docker", "compose", "exec", "-T", "web", "node", "-e", program],
    input=json.dumps({"token": shared, "key": uuid.uuid4().hex}), text=True, check=True,
)
