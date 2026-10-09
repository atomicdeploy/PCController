import { readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const policyURL = new URL('../../toolchain-profile.json', import.meta.url)
const lockURL = new URL('../../toolchain-lock.json', import.meta.url)
const outputURL = new URL('toolchain_policy_gen.go', import.meta.url)

const policy = JSON.parse(await readFile(policyURL, 'utf8'))
const lock = JSON.parse(await readFile(lockURL, 'utf8'))

const target = policy?.target
const targetIntegers = [
  'clock_hz', 'baud', 'application_limit_bytes', 'flash_bytes', 'eeprom_bytes'
]
if (policy?.format !== 'pccontroller-toolchain-policy/v1' ||
    typeof policy.name !== 'string' || !policy.name.trim() ||
    typeof policy.fqbn !== 'string' || !policy.fqbn.trim() ||
    !target || typeof target !== 'object' || Array.isArray(target) ||
    typeof target.mcu !== 'string' || !target.mcu.trim() ||
    typeof target.bootloader !== 'string' || !target.bootloader.trim() ||
    targetIntegers.some(field => !Number.isSafeInteger(target[field]) || target[field] <= 0) ||
    target.application_limit_bytes >= target.flash_bytes) {
  throw new Error(`${fileURLToPath(policyURL)} must contain a named policy, FQBN, and valid target capacities`)
}
if (lock?.format !== 'pccontroller-toolchain-lock/v1' ||
    lock?.policy_name !== policy.name || lock?.firmware?.fqbn !== policy.fqbn) {
  throw new Error(`${fileURLToPath(lockURL)} must be generated from the canonical named policy and FQBN`)
}

const canonicalPolicy = `${JSON.stringify(policy, null, 2)}\n`
const canonicalLock = `${JSON.stringify(lock, null, 2)}\n`
const source = `// Code generated from ../../toolchain-profile.json by generate-toolchain-policy.mjs; DO NOT EDIT.

package programmer

const generatedToolchainPolicyJSON = ${JSON.stringify(canonicalPolicy)}

const generatedToolchainLockJSON = ${JSON.stringify(canonicalLock)}

const (
	generatedBoardMCU                     = ${JSON.stringify(target.mcu)}
	generatedBoardClockHz          uint32 = ${target.clock_hz}
	generatedBoardBootloader              = ${JSON.stringify(target.bootloader)}
	generatedBoardBaud                    = ${target.baud}
	generatedBoardApplicationBytes uint32 = ${target.application_limit_bytes}
	generatedBoardFlashBytes       uint32 = ${target.flash_bytes}
	generatedBoardEEPROMBytes      uint32 = ${target.eeprom_bytes}
)
`

if (process.argv.includes('--check')) {
  const actual = await readFile(outputURL, 'utf8')
  if (actual.replaceAll('\r\n', '\n') !== source) {
    throw new Error(`generated toolchain policy is stale: ${fileURLToPath(outputURL)}; run node ${fileURLToPath(import.meta.url)}`)
  }
} else {
  await writeFile(outputURL, source, 'utf8')
}
