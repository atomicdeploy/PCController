// PCController network plugin. Original integration, using SignalRGB's public
// network socket API and DiscoveryService controller-registration contract.
// Configure this endpoint to match an explicitly started PCController DDP input.
import { udp } from "@SignalRGB/udp";

const target = { host: "127.0.0.1", port: 4048, pixels: 60 };
export function Name() { return "PCController"; }
export function Publisher() { return "PCController"; }
export function Version() { return "1.0.0"; }
export function Type() { return "network"; }
export function Size() { return [target.pixels, 1]; }
export function DefaultPosition() { return [0, 0]; }
export function DefaultScale() { return 1; }
export function LedNames() { return Array.from({ length: target.pixels }, (_, i) => `LED ${i + 1}`); }
export function LedPositions() { return Array.from({ length: target.pixels }, (_, i) => [i, 0]); }
export function ControllableParameters() {
  return [{ property: "streamEnabled", label: "Stream RGB", type: "boolean", default: "false" }];
}

let socket;
let sequence = 0;
let lastSend = 0;
/* global device, service, streamEnabled */
export function Initialize() {
  if (!Number.isInteger(target.pixels) || target.pixels < 1 || target.pixels > 100) throw new Error("PCController LED count must be 1..100");
  socket = udp.createSocket();
  socket.on("error", err => device.log(`PCController UDP: ${err}`));
  socket.connect(target.host, target.port);
  sequence = 0;
  lastSend = 0;
}
export function Render() {
  if (!socket || streamEnabled !== true) return;
  const now = Date.now();
  if (now - lastSend < 1000 / 60) return;
  lastSend = now;
  sequence = sequence % 15 + 1;
  const length = target.pixels * 3;
  const packet = [0x41, sequence, 0x0b, 1, 0, 0, 0, 0, length >> 8, length & 255];
  for (let i = 0; i < target.pixels; i++) {
    const rgb = device.color(i, 0);
    for (let c = 0; c < 3; c++) packet.push(Math.max(0, Math.min(255, Math.round(rgb[c]))));
  }
  socket.send(packet);
}
export function Shutdown() {
  // No automatic blackout, reconnect, ownership acquisition or host command.
  if (socket) socket.close();
  socket = undefined;
}

export function DiscoveryService() {
  // Explicitly configured endpoint, not an mDNS claim or a health assertion.
  // Registering the device does not start PCController's receiver.
  this.Initialize = function () {
    const id = `pccontroller-ddp-${target.host}-${target.port}`;
    const endpoint = { id, name: "PCController RGB input", ip: target.host, port: target.port, pixels: target.pixels };
    if (service.getController(id) === undefined) {
      service.addController(endpoint);
    }
    service.updateController(endpoint);
    service.announceController(endpoint);
  };
  this.Update = function () {};
}
