import assert from "node:assert/strict";
import test from "node:test";

import { verifyResetCaptureDisassembly } from "./assert-avr-reset-capture.mjs";

const symbols = `
0000024e g       .text 00000000 __ctors_end
0000025a l     F .text 00000024 (anonymous namespace)::captureResetCause()
0000027e g     F .text 00000016 __do_copy_data
`;

const valid = `
0000025a <(anonymous namespace)::captureResetCause()>:
 25a: 84 b7        in r24, 0x34
 25c: 81 11        cpse r24, r1
 25e: 01 c0        rjmp .+2
 260: 82 2d        mov r24, r2
 262: 80 93 97 06  sts 0x0697, r24
 266: 14 be        out 0x34, r1
 268: a8 95        wdr
0000027e <__do_copy_data>:
`;

test("proves early-init reset capture consumes Urboot r2 in the safe order", () => {
  const proof = verifyResetCaptureDisassembly(symbols, valid);
  assert.equal(proof.capture, 0x25a);
  assert.match(proof.report, /zero fallback from Urboot r2/u);
});

test("rejects a build that stops consuming Urboot r2", () => {
  assert.throws(
    () => verifyResetCaptureDisassembly(symbols, valid.replace("mov r24, r2", "mov r24, r3")),
    /consume Urboot reset flags from r2/u,
  );
});

test("rejects reset capture outside the early-init window", () => {
  assert.throws(
    () => verifyResetCaptureDisassembly(symbols.replace("0000025a l", "0000028a l"), valid),
    /not in the early-init window/u,
  );
});
