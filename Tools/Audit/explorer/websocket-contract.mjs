// Documentation for the connection-scoped subscription contract in ipcjson/ipc.go.
// These examples read/subscribe only; they never actuate hardware.
export const subscriptionSchema = {
  type: "object",
  properties: {
    topics: { type: "array", items: { enum: ["events", "state", "debug", "status", "opcodes", "telemetry", "opcode"] }, description: "Defaults to events, or opcodes when an opcode filter is supplied. telemetry aliases status; opcode aliases opcodes." },
    opcodes: { type: "array", items: { type: "integer", minimum: 1, maximum: 255 }, description: "Optional raw-opcode filter; requires the opcodes topic. Zero is reserved." },
    interval_ms: { type: "integer", anyOf: [{ const: 0 }, { minimum: 50, maximum: 60000 }], default: 200, description: "Status cadence only. Omitted/zero selects 200 ms; ignored without status." },
    state_interval_ms: { type: "integer", anyOf: [{ const: 0 }, { minimum: 16, maximum: 1000 }], default: 0, description: "Coalescing budget for high-rate state only; zero delivers immediately. Segment edges remain immediate." },
    after_id: { type: "integer", minimum: 0, description: "Resume retained event streams after this event ID. Omitted/zero starts at the current cursor. IDs are host-instance scoped, not durable across restarts." },
    preserve: { type: "boolean", default: false, description: "False replaces all subscriptions on this connection. True replaces only the named topics, preserving other workers/cursors." },
  },
  examples: [{ topics: ["events", "state", "status"], interval_ms: 200 }],
};

export const connectionGuides = [
  {
    id: "websocket", name: "WebSocket quick start", address: "/ipc",
    summary: "Full-duplex JSON-RPC requests, replies and live notifications on one WebSocket connection.",
    sections: [
      { title: "Connect", text: "The default endpoint is ws://<host>:<port>/ipc. Read websocket_path from GET /api/ui-config when the host uses a custom path. Use wss:// behind a TLS reverse proxy; the built-in host does not terminate TLS. Immediate-alpha application authentication is disabled. Remote listeners and allowed Origins must still be configured; do not put credentials in URLs." },
      { title: "Browser JavaScript · read-only example", text: "Run in the controller application page or your own allowed-origin client, not this non-executing reference page. No hardware commands are sent.", code: `const config = await fetch('/api/ui-config').then(r => r.json());
const url = new URL(config.websocket_path, location.origin);
url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
const socket = new WebSocket(url);
socket.addEventListener('message', ({ data }) => {
  const message = JSON.parse(data);
  if ('id' in message) {
    console.log('reply', message.id, message.result ?? message.error);
  } else {
    console.log('notification', message.method, message.params);
  }
});
socket.addEventListener('open', () => {
  socket.send(JSON.stringify({ jsonrpc: '2.0', id: 1,
    method: 'controller.snapshot', params: {} }));
  socket.send(JSON.stringify({ jsonrpc: '2.0', id: 2,
    method: 'controller.subscribe', params: {
      topics: ['events', 'state', 'status'], interval_ms: 200
    } }));
});
// When finished: send an unsubscribe request, or close the connection.
function stopWatching() {
  if (socket.readyState === WebSocket.OPEN)
    socket.send(JSON.stringify({ jsonrpc: '2.0', id: 3,
      method: 'controller.unsubscribe', params: {} }));
}
// socket.close(1000, 'Finished');` },
      { title: "Framing and response correlation", text: "Send one JSON-RPC object per text message, not newline-delimited batches or Socket.IO packets. Binary messages are ignored. The inbound message limit is 1 MiB. Give requests unique IDs and match replies by id, not arrival order. Calls without id do not receive replies. Notifications have method/params and no id; they can interleave with replies, including before the subscription acknowledgement. Register your handler first and buffer notifications if your client requires an ACK boundary. All documented RPC methods share this transport and their existing admission rules." },
      { title: "Subscription lifecycle", text: "A new connection has no subscriptions. controller.subscribe defaults to events; state also sends an initial controller.snapshot and subsequent snapshot/PWM patches. A new subscription replaces all topics unless preserve is true. controller.unsubscribe removes all topics from this socket. Closing the connection stops its workers. Reconnect explicitly and resubscribe; use after_id only for retained events from the same host instance, and refresh the snapshot on host replacement. Do not automatically replay mutations after disconnect." },
      { title: "Subscription parameters", schema: subscriptionSchema },
      { title: "Topics and notification methods", table: [
        ["events", "controller.event; relevant changes can also emit controller.snapshot"],
        ["state", "controller.snapshot initially; controller.state, controller.pwm.values and changed snapshots afterward"],
        ["debug", "controller.debug (explicit opt-in)"],
        ["status / telemetry", "controller.status at interval_ms; controller.error on status read failure"],
        ["opcodes / opcode", "controller.opcode, optionally filtered by opcodes"],
      ] },
    ],
    methods: ["controller.subscribe", "controller.unsubscribe", "controller.snapshot", "controller.status"],
  },
  {
    id: "socketio", name: "Socket.IO framing", address: "/socket.io/?EIO=4&transport=websocket",
    summary: "Bounded Engine.IO v4 / Socket.IO WebSocket adapter. It is not the raw JSON-RPC /ipc endpoint.",
    sections: [
      { title: "Handshake and heartbeat", text: "Connect using transport=websocket and EIO=4. The server sends 0{...} with sid, upgrades, pingInterval, pingTimeout and maxPayload. Send 40; the server replies 40{\"sid\":...}. Answer server text ping 2 with text pong 3 (the advertised ping interval is 25 seconds). A compatible Socket.IO client handles framing and heartbeat; disable polling fallback. Send 41 or close the WebSocket to disconnect." },
      { title: "Subscribe · client → server", code: '42["subscribe",{"topics":["events","state","status"],"interval_ms":200}]' },
      { title: "Read-only RPC · client → server", code: '42["rpc",{"jsonrpc":"2.0","id":1,"method":"controller.snapshot","params":{}}]' },
      { title: "Replies and events", text: "Events are 42[name,payload]. subscribe replies with subscribed; unsubscribe with unsubscribed. rpc replies with rpc.response carrying the JSON-RPC envelope. Controller notifications carry the notification method as event name and params as payload. message and command have message.accepted and command.response replies; their normal authorization and hardware admission still apply. Unsupported events produce error." },
      { title: "Unsubscribe · client → server", code: '42["unsubscribe",{}]' },
      { title: "Supported subset", text: "WebSocket only: no long-polling, custom namespaces, rooms, binary attachments or acknowledgement callbacks. Use the raw /ipc endpoint if you do not need Socket.IO framing." },
    ],
  },
];
