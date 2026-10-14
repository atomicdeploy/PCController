import { describe,it,expect } from 'vitest'
import { RfReceptionSession,rfPresent } from './rf-reception'
describe('RF reception session',()=>{
 it('resets only on opening and survives coalescing and duplicate snapshots',()=>{
  const session=new RfReceptionSession()
  const receipt={code:42,bits:24,protocol:1,received_count:100,last_seen:'2026-01-01T00:00:00Z'}
  session.open([receipt]);expect(session.count(receipt)).toBe(0)
  const received={...receipt,received_count:109};expect(session.count(received)).toBe(9);expect(session.count(received)).toBe(9)
  session.open([received]);expect(session.count(received)).toBe(0)
 })
 it('stays on through repeated packets, expires at silence and fails closed offline',()=>{
  const receipt={code:42,bits:24,protocol:1,received_count:2,last_seen:'2026-01-01T00:00:00Z'}
  const time=Date.parse(receipt.last_seen)
  expect(rfPresent(receipt,true,time+249)).toBe(true)
  expect(rfPresent(receipt,true,time+250)).toBe(false)
  expect(rfPresent(receipt,false,time+10)).toBe(false)
 })
})
