#!/usr/bin/env node
// SPDX-License-Identifier: MIT
// Exercise the actual linked AVR bit loop, including compiler register allocation.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { loadProjectEnv } from '../Build/env.mjs'

loadProjectEnv()

const listingPath = process.argv[2]
if (!listingPath) throw new Error('Usage: node Tools/Firmware/verify-strip-waveform.mjs AVR-LISTING.lst')
const listing = readFileSync(listingPath, 'utf8')
const start = listing.match(/^([0-9a-f]+) <head20\d+>:/m)
assert.ok(start, 'linked strip bit-loop label is missing')
const instructions = []
for (const line of listing.slice(start.index).split('\n')) {
  const match = line.trimEnd().match(/^\s*([0-9a-f]+):\s+(?:(?:[0-9a-f]{2})\s+)+\s*([a-z]+)\s*([^;]*)(?:;\s*(.*))?$/)
  if (!match) continue
  const [, address, op, operandText, comment = ''] = match
  const target = comment.match(/^0x([0-9a-f]+)/)
  instructions.push({ address: parseInt(address, 16), op, args: operandText.trim().split(/\s*,\s*/), target: target && parseInt(target[1], 16) })
  if (op === 'brne') break
}
assert.equal(instructions.at(-1)?.op, 'brne', 'complete strip byte loop is required')
const byAddress = new Map(instructions.map((instruction, index) => [instruction.address, index]))
const hi = instructions[0].args[1]
const byte = instructions.find(i => i.op === 'sbrc').args[0]
const bit = instructions.find(i => i.op === 'dec').args[0]
const moves = instructions.filter(i => i.op === 'mov')
const next = moves[0].args[0]
const lo = moves[1].args[1]
const count = instructions.find(i => i.op === 'sbiw').args[0]
assert.notEqual(next, lo, `compiler aliased changing bit operand and constant low operand (${lo})`)
assert.equal(new Set([hi, lo, byte, bit, next, count]).size, 6, 'loop operands must have independent registers')

function verify(bytes) {
  const registers = { [hi]: 64, [lo]: 0, [byte]: bytes[0], [bit]: 8, [next]: 0, [count]: bytes.length }
  let pointer = 1, pc = 0, cycles = 0, zero = false, carry = 0, level = 0
  const rises = [], falls = []
  for (let steps = 0; pc < instructions.length; steps++) {
    assert.ok(steps < bytes.length * 8 * 30, 'strip loop did not terminate')
    const instruction = instructions[pc]
    const [a, b] = instruction.args
    let advance = 1, elapsed = 1
    switch (instruction.op) {
      case 'st': {
        elapsed = 2
        const newLevel = registers[b] & 64
        if (newLevel !== level) (newLevel ? rises : falls).push(cycles + elapsed)
        level = newLevel
        break
      }
      case 'sbrc': if (!(registers[a] & (1 << Number(b)))) { advance = 2; elapsed = 2 }; break
      case 'mov': registers[a] = registers[b]; break
      case 'dec': registers[a] = (registers[a] - 1) & 255; zero = registers[a] === 0; break
      case 'breq':
      case 'brne':
        if (instruction.op === 'breq' ? zero : !zero) {
          advance = byAddress.get(instruction.target) - pc
          elapsed = 2
        }
        break
      case 'adc': {
        const sum = registers[a] + registers[b] + carry
        registers[a] = sum & 255
        carry = sum > 255 ? 1 : 0
        break
      }
      case 'rjmp': advance = byAddress.get(instruction.target) - pc; elapsed = 2; break
      case 'nop': break
      case 'ldi': registers[a] = Number(b); break
      case 'ld': registers[a] = bytes[pointer++] ?? 0; elapsed = 2; break
      case 'sbiw': registers[a]--; zero = registers[a] === 0; elapsed = 2; break
      default: throw new Error(`Unmodeled AVR instruction ${instruction.op}`)
    }
    assert.ok(Number.isFinite(advance), `invalid branch target at ${instruction.address.toString(16)}`)
    cycles += elapsed
    pc += advance
  }
  assert.equal(level, 0, 'data pin must finish low')
  assert.equal(rises.length, bytes.length * 8, 'one rising edge per data bit')
  assert.equal(falls.length, rises.length, 'one falling edge per data bit')
  for (let i = 0; i < rises.length; i++) {
    const value = (bytes[i >> 3] >> (7 - (i & 7))) & 1
    assert.equal(rises[i] - rises[0], i * 20, `bit ${i} period`)
    assert.equal(falls[i] - rises[i], value ? 13 : 5, `bit ${i} high width`)
  }
  assert.equal(pointer, bytes.length + 1, 'byte pointer must advance across the full frame')
}

// Every byte value, transitions both ways, and full 100-pixel frame length.
for (let value = 0; value < 256; value++) verify([value, value ^ 255, value])
verify(Array.from({ length: 300 }, (_, i) => i & 255))
console.log('PASS: linked AVR strip loop: 256 byte patterns, byte boundaries, 300-byte frame; 20-cycle cells and 5/13-cycle high pulses.')
