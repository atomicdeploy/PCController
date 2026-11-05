#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {readFileSync,existsSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';

const root=resolve(dirname(fileURLToPath(import.meta.url)),'../..');
const migration=JSON.parse(readFileSync(resolve(root,'Tools/Audit/native-desktop-path-migration.json'),'utf8'));
export function identityViolations(path,content){
  if(path.includes('/vendor/')||migration.historicalPaths.some(p=>p.endsWith('/')?path.startsWith(p):path===p))return [];
  const findings=[];
  for(const token of migration.previousIdentityTokens){
    if(path.toLowerCase().includes(token.toLowerCase())||content.toLowerCase().includes(token.toLowerCase()))findings.push(`previous product identity: ${token}`);
  }
  for(const pattern of migration.previousModelPatterns){
    if(new RegExp(pattern,'m').test(content))findings.push('previous desktop model naming');
  }
  return findings;
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
  const files=execFileSync('git',['ls-files','-z','--cached','--others','--exclude-standard'],{cwd:root,encoding:'utf8',maxBuffer:32*1024*1024}).split('\0').filter(Boolean);
  const failures=[];
  for(const path of new Set(files)){
    if(!existsSync(resolve(root,path)))continue;
    const bytes=readFileSync(resolve(root,path));
    if(bytes.includes(0))continue;
    const content=bytes.toString('utf8');
    if(!Buffer.from(content).equals(bytes))continue;
    for(const finding of identityViolations(path,content))failures.push(`${path}: ${finding}`);
  }
  if(failures.length){console.error(failures.join('\n'));process.exitCode=1;}
  else console.log('Desktop identity audit passed; only explicit historical/compatibility and lighting-icon vocabulary is retained.');
}
