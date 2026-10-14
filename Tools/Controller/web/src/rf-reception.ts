export type RfSignal = { code:number; bits:number; protocol:number };
export type RfReception = RfSignal & { pulse_us?:number; received_count:number; last_seen:string; connection_generation?:number };
export type RfLearnPreferences = { timed:boolean; timeout_seconds:number; continuous:boolean };
export const rfKey = (value:RfSignal) => `${value.code}:${value.bits}:${value.protocol}`;
export const rfPresent = (receipt:RfReception | undefined, connected:boolean, now=Date.now()) => connected && !!receipt && now-Date.parse(receipt.last_seen)>=0 && now-Date.parse(receipt.last_seen)<250;
export class RfReceptionSession {
  baseline=new Map<string,number>();
  open(receipts:readonly RfReception[]) {this.baseline=new Map(receipts.map(r=>[rfKey(r),r.received_count]));}
  count(receipt:RfReception | undefined) {return receipt ? Math.max(0,receipt.received_count-(this.baseline.get(rfKey(receipt)) ?? 0)):0;}
}
