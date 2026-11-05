import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {identityViolations} from './check-desktop-identity.mjs';
const migration=JSON.parse(readFileSync(new URL('../Audit/native-desktop-path-migration.json',import.meta.url),'utf8'));

test('previous product identity cannot return to source, UI, module imports or filenames',()=>{
  for(const token of migration.previousIdentityTokens){
    assert.ok(identityViolations('Tools/NativeDesktop/internal/example.go',`const name = "${token}"`).length);
    assert.ok(identityViolations(`Tools/NativeDesktop/${token}.go`,'').length);
    assert.ok(identityViolations('Tools/NativeDesktop/example.go',token.toUpperCase()).length);
  }
});
test('required provenance exceptions are narrow, not entire runtime directories',()=>{
  const token=migration.previousIdentityTokens[0];
  assert.deepEqual(identityViolations('Tools/NativeDesktop/docs/attribution.md',token),[]);
  assert.ok(identityViolations('Tools/NativeDesktop/internal/ui/example.go',token).length);
});
test('lighting icons, user labels and clamp helpers are not mistaken for product identity',()=>{
  assert.deepEqual(identityViolations('Tools/Controller/web/src/example.ts','lamp-ceiling; desk lamp; clamp(0,100)'),[]);
});
