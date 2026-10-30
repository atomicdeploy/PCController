import {spawnSync} from 'node:child_process';
import {mkdirSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
const root=resolve(fileURLToPath(new URL('../..',import.meta.url)));
const out=process.platform==='win32'?join(process.env.LOCALAPPDATA,'PCController','test-programs','native'):join(tmpdir(),'pccontroller-native-tests');
mkdirSync(out,{recursive:true});
const exe=join(out,'rf-deferred'+(process.platform==='win32'?'.exe':''));
for(const [command,args] of [[process.env.CXX||'g++',['-std=c++17','-DARDUINO=10819','-I'+join(root,'Tools/Firmware/tests/rf-deferred'),join(root,'Tools/Firmware/tests/rf-deferred/main.cpp'),join(root,'LocalLib/RCSwitch.cpp'),'-o',exe]],[exe,[]]]) {
 const result=spawnSync(command,args,{stdio:'inherit'});
 if(result.error)throw result.error;
 if(result.status!==0)process.exit(result.status||1);
}
