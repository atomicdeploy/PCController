import assert from "node:assert/strict";
import test from "node:test";

import { findCaseFoldCollisions } from "./release-asset-collisions.mjs";

test("finds only differently-cased names that GitHub rejects as duplicates", () => {
  const desired = [
    "release-assets/PCController-Firmware-v0.1.0-alpha.1-AVR-ATmega328P.tar.gz",
    "release-assets/PCController-Host-v0.1.0-alpha.1-Linux-x64.tar.gz",
  ];
  const existing = [
    { id: 10, name: "pccontroller-firmware-v0.1.0-alpha.1-avr-atmega328p.tar.gz" },
    { id: 11, name: "PCController-Host-v0.1.0-alpha.1-Linux-x64.tar.gz" },
    { id: 12, name: "unrelated-user-asset.zip" },
  ];

  assert.deepEqual(findCaseFoldCollisions(desired, existing), [
    {
      id: 10,
      name: "pccontroller-firmware-v0.1.0-alpha.1-avr-atmega328p.tar.gz",
      replacement: "PCController-Firmware-v0.1.0-alpha.1-AVR-ATmega328P.tar.gz",
    },
  ]);
});

test("accepts paginated asset arrays and rejects ambiguous desired names", () => {
  assert.deepEqual(
    findCaseFoldCollisions(["PCController.hex"], [[{ id: 20, name: "pccontroller.hex" }]]),
    [{ id: 20, name: "pccontroller.hex", replacement: "PCController.hex" }],
  );

  assert.throws(
    () => findCaseFoldCollisions(["PCController.hex", "pccontroller.hex"], []),
    /collide case-insensitively/,
  );
});
