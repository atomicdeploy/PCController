// Human-reviewed meaning lives here; wire types and route coverage remain
// source-derived. Do not turn a Go field's zero value into an invented default.
export const groups = {
  service: ['Service identity', 'Identify a running host before selecting it. Liveness is not proof that a board is connected or that an update succeeded.'],
  'ui-config': ['Client bootstrap', 'Discover browser-safe transport paths and UI configuration so clients do not hard-code the selected host’s listener layout. No credentials are returned.'],
  auth: ['Server identity compatibility', 'Reserved authentication compatibility routes. Application authentication is disabled in the current alpha; these routes do not establish a login session.'],
  session: ['Transport-session compatibility', 'Reserved browser transport ticket flow. Disabled in the current alpha; use the configured WebSocket endpoint directly.'],
  rpc: ['Typed controller operations', 'One JSON-RPC dispatcher serves HTTP, WebSocket and NDJSON clients. Use the method catalog for RF, automation, peripherals, effects and other operations without a dedicated REST route.'],
  snapshot: ['Controller state', 'Initialize a client from one coherent cached observation, then follow changed-state notifications. A snapshot is not a new electrical measurement.'],
  sensors: ['PCB measurements', 'Export DS18B20 temperature and INA219 supply measurements to monitoring software. These describe the controller PCB, not the PC’s CPU or motherboard.'],
  docs: ['API contracts', 'Offline, embedded reference pages and machine-readable contracts for integrating clients without a CDN or live hardware requests.'],
  diagnostics: ['Host performance captures', 'Opt-in bounded Go runtime recordings for diagnosing CPU load and scheduling delays. Captures concern this host process and never exercise board outputs.'],
  estop: ['Effect and motion interlock', 'Inspect or change the host safety interlock used to inhibit effect and motion operations. Releasing it does not restart interrupted work or prove an external circuit is safe.'],
  peripherals: ['Peripheral presentation', 'Read advertised controls and customize host-owned labels. Presentation changes do not change wiring, firmware pin allocation or physical output values.'],
  pwm: ['PWM outputs', 'Read or set pulse-width-modulated output registers. Percent uses the configured channel curve; raw values address the 12-bit register directly. Register readback is not a measurement of load brightness or current.'],
  commands: ['Command vocabulary', 'Discover the shared CLI/TUI command grammar before using the command dispatcher. Prefer typed RPC methods when writing a new integration.'],
  command: ['Command execution', 'Execute the same semantic commands used by interactive clients. This is the controller command language, not a system shell or arbitrary process launcher.'],
  'program-state': ['Program ownership', 'Coordinate whether an application owns the controller in an idle, playback or recording workflow. This state is distinct from flashing firmware.'],
  menu: ['Board front-panel menus', 'Inspect and arrange firmware-advertised pages shown on the physical front panel. Page identifiers come from the live catalog, not UI row numbers.'],
  'host-menus': ['Host-presented front-panel content', 'Configure the additional menu directory supplied by the host to the board. This allows host features to appear on the physical front panel without adding firmware pages.'],
  os: ['Host operating-system integration', 'Read bounded PC facts or request explicitly allowed keyboard and power actions on the machine running PCController. These actions target the host, not the AVR board.'],
  messages: ['Application messages', 'Submit typed, source-tagged text to the controller message pipeline for integrations and event handling. This is separate from displaying an OSD or writing board display text.'],
  display: ['Physical display presentation', 'Present text on the board’s seven-segment display or LCD with explicit timing. Desktop overlays use the OSD API instead.'],
  opcode: ['Low-level board transport', 'Exchange a protocol opcode (one-byte operation identifier) and opaque payload with the connected board. Prefer typed methods; malformed or unsafe commands are still subject to ownership and interlock policy.'],
  app: ['Application coordination', 'Coordinate live TUI, WebUI and desktop instances through leased identities, page navigation and correlated actions. Delivery is distinct from a target acknowledging that an action was applied.'],
  bridges: ['Peer RPC relay', 'Inspect configured peer links and relay a bounded, non-recursive JSON-RPC request to a named peer. Discovery advertisements alone do not grant authority over a board.'],
  artifacts: ['Immutable update files', 'Import, inspect and retrieve content-addressed firmware, EEPROM, flash backups and host executables. Storing bytes does not apply them; programming is an explicit separate operation.'],
  board: ['Board firmware transfer', 'Read back the current board flash or program an already-staged firmware image through the guarded updater. Upload/download are named from the board’s perspective, not the browser’s.'],
  updates: ['Update transactions', 'Explicitly authorize a staged firmware, EEPROM or host update and follow its durable operation status. HTTP acceptance does not mean the target restarted or passed verification.'],
  restores: ['Flash recovery', 'Restore a verified captured flash backup through the same ownership, backup and programming safeguards as updates. A firmware candidate and a flash readback are different artifact kinds.'],
  discovery: ['Software candidate discovery', 'Find compatible software in GitHub Actions, GitHub Releases or a manifest; compare identities and stage verified bytes. This is software discovery, not LAN peer discovery, and never installs a candidate by itself.'],
  webhooks: ['Inbound messages and outbound delivery', 'Integrate HTTP event producers and consumers. Pending deliveries await retry; dead-letter deliveries exhausted automatic handling and require explicit replay or deletion.'],
  integrations: ['Host integration adapters', 'Inspect effective local integration routing or use the confined data-service adapter. The generic device HTTP path is deliberately disabled in favor of typed RPC actions.'],
  osd: ['Desktop on-screen display', 'Show non-activating Windows notifications with timed transitions, icons and optional human-activated actions. OSD is the desktop overlay, not the physical LCD or seven-segment display.'],
};

export const introduction = `PCController is the primary owner of a controller board and exposes the same operations to its desktop GUI, TUI (terminal UI), WebUI and remote clients. Start with **GET /api/ui-config**, read **GET /api/snapshot**, and use the [WebSocket guide](protocols.html#websocket) for live changes. Use [JSON-RPC methods](rpc.html) for RF reception/learning, board automation, melodies, lighting, channel routing and other typed features.

### Choose the right operation
Reading a snapshot or catalog does not actuate outputs. Editing presentation or an effect library does not play it. Importing/staging an artifact does not flash it. An asynchronous **202** means a job was queued: retain its operation ID and inspect its progress and terminal state. Never infer physical success solely from HTTP success or repeat a hardware mutation after a lost response.

### Terms and units
- **Host**: the PCController process on a computer. **Board**: the attached AVR hardware (or explicitly selected virtual board). **Peer**: another reachable application host. **Application instance**: one live GUI/TUI/WebUI surface with a leased identity.
- **PWM**: pulse-width modulation. Raw output is 0–4095; percent is 0–100 after channel calibration. **RF**: radio-frequency remote control; learning stores a received waveform, assigning binds it to an action. **LCD**: character display; **OSD**: desktop overlay.
- **DS18B20** measures temperature; **INA219** measures supply voltage/current. Sensor values use declared units; null/unavailable is not zero. A register readback confirms a stored command, not its electrical effect.
- **SHA-256**: content digest identifying exact artifact bytes. **Build hash**: source/build identity, not necessarily that file digest. **EEPROM**: persistent board settings. A **manifest** lists candidate files and provenance; a **candidate** is not yet verified or installed.
- **Revision / ETag**: opaque concurrency token. **Compare-and-swap** applies a change only if the supplied revision is current. Host-instance ID and connection generation fence commands against a replaced process or board session. Read these tokens from the selected host rather than manufacturing them.
- **Idempotency key**: caller identity for one logical job, allowing supported update APIs to find the same job after a transport interruption. It does not make arbitrary hardware commands safe to replay. **Outcome unknown** means the command may already have taken effect.
- **JSON-RPC**: named requests with correlated IDs and result/error envelopes. **NDJSON**: one JSON object per line over a stream. WebSocket push messages without an ID are notifications, not request responses. **ACK** means acknowledgement; its strength depends on the method (delivery, register readback or target application confirmation).

### Access and transport
Loopback is the safe default. Application authentication/authorization is disabled in the current alpha; dormant ticket/proof routes return 409. Operating-system, updater, ownership and safety gates still apply. The built-in listener does not terminate TLS: use a trusted deployment boundary for remote access. Examples use the selected origin; no request is executed from this reference. Contracts are unversioned and describe this build. See the [event/transport contract](asyncapi.json) and [RPC catalog](rpc.html) for subscriptions and response envelopes.`;

// [title, behavior, result meaning, useful workflow, RPC counterpart]
const entries = {
  '/healthz': ['Identify the running service', 'Check that this HTTP process responds and identify its product/service. It does not probe the serial port.', 'ok/status describe service liveness; product/service identify the responding host, not board health.', 'Use before accepting a saved endpoint; then read the snapshot for board connection state.', 'controller.ping'],
  '/upnp/public.json': ['Read the public host directory', 'Return a bounded non-secret directory used by discovery clients to identify this host and its advertised endpoints.', 'Host, board and endpoint facts are observations; telemetry availability and health must be checked separately.', 'Verify a discovered service before offering it as a connection target.'],
  '/api/ui-config': ['Discover client transport settings', 'Read the non-secret browser bootstrap, including the configured WebSocket and Socket.IO paths. Do not assume /ipc is fixed.', 'The transport paths refer to this listener; combine them with the selected HTTP origin and choose ws or wss accordingly.', 'Bootstrap a WebUI or external client without storing transport paths in its source.'],
  '/api/auth/server-proof': ['Inspect server-proof availability', 'Reserved challenge-response route for server identity verification. It currently rejects with 409 because application authentication is disabled.', '409 means no proof was issued; it is not a failed board handshake.', 'Keep compatibility clients from mistaking a disabled authentication flow for a disconnected board.'],
  '/api/session/ticket': ['Inspect session-ticket availability', 'Reserved one-use transport ticket route. It currently returns 409; no ticket is issued or required in this alpha.', '409 describes disabled application authentication, not an expired browser session.', 'Current clients connect to the bootstrap WebSocket path without requesting a ticket.'],
  '/api/rpc': ['Call a typed controller method', 'Dispatch one JSON-RPC 2.0 method. The method catalog documents the exact params, capabilities and result semantics for each operation.', 'Inspect the JSON-RPC result or error inside the response; an HTTP transport success is not itself method success. IDs correlate requests; notifications omit the ID.', 'For example, call controller.rf.catalog to build an RF manager without parsing CLI text.'],
  '/api/snapshot': ['Read the controller snapshot', 'Return the host’s coherent cached controller state without issuing a new output command. It is the starting point for a live client.', 'Connection, board identity, owner/revision and observed values belong to this host/session. Missing or stale measurements must not be treated as zero.', 'Read once at connection, subscribe to changed state, and retain the last good observation during recovery.', 'controller.snapshot'],
  '/api/sensors/board': ['Read PCB sensor measurements', 'Export fresh DS18B20 temperature and INA219 voltage/current/power readings with explicit availability and original host receipt time.', 'Each reading has id, kind, unit and value. null means unavailable or expired; observed_at is frame receipt time, not the time of this HTTP request. fresh expires after five seconds.', 'Feed a USB sensor-bridge client without confusing board temperature with PC CPU temperature.'],
  '/api/sensors/metrics': ['Scrape PCB Prometheus metrics', 'Export the same PCB measurements as Prometheus text gauges. Unavailable readings are omitted rather than fabricated as zero.', 'Gauge labels identify sensors and units. Absence means no usable current reading; it is not a 0 V or 0 °C measurement.', 'Point a local monitoring collector at this route; use /api/os/facts for PC hardware sensors instead.'],
  '/api/docs/{asset}': ['Read an embedded API document', 'Serve this build’s offline reference, OpenAPI, JSON-RPC and AsyncAPI files. Only bundled regular assets are available; this is not a filesystem browser.', 'The content type identifies HTML, JSON, JavaScript or CSS. HEAD returns the same resource headers without a response body.', 'Download openapi.json for a client generator or asyncapi.json for event tooling.'],
  '/api/diagnostics/cpu-profile': ['Capture host CPU samples', 'Record five seconds of CPU samples in the Go pprof format. Loopback only; the shared capture lock excludes another profile or trace.', 'Binary gzip pprof bytes describe CPU time in this process, not a JSON status object. No board test is started.', 'Save the response to cpu.pprof and inspect it with go tool pprof while reproducing a performance issue.'],
  '/api/diagnostics/execution-trace': ['Capture host scheduling activity', 'Record a five-second Go execution trace to investigate goroutine scheduling, blocking and runtime work. Loopback only; shares the diagnostic capture lock.', 'Binary Go trace bytes, not a gzip CPU profile. No board operation is performed.', 'Save to trace.out and inspect with go tool trace to distinguish waiting from CPU work.'],
  '/api/estop': ['Read the effect and motion interlock', 'Inspect the authoritative emergency-stop latch, including its source/reason. This is a host interlock, not an independent safety-rated physical emergency stop.', 'active means effect/motion admission is inhibited; releasing the latch does not resume prior movement.', 'Check the interlock when a motion/effect request is rejected.', 'controller.estop.get'],
  '/api/peripherals': ['Read peripheral descriptors', 'Return the current peripheral/control presentation and host-owned names so clients can label physical channels consistently.', 'Descriptors identify controls and their presentation; labels may be renamed and must not be used as physical numeric addresses.', 'Build a channel picker using stable IDs while showing the configured names.', 'controller.peripherals.get'],
  '/api/pwm': ['Read PWM output registers', 'Read authoritative PWM values through the current host owner. PWM is a 12-bit output register; calibrated percentages may be nonlinear.', 'Returned values are observed register/setpoint state, not measured brightness or load current.', 'Initialize a mixer, then follow state updates instead of overwriting an active slider draft.', 'controller.pwm.values'],
  '/api/pwm/channel': ['Read PWM channel configuration', 'Read host-persisted per-channel output curves, icons and on/step behavior. These settings change presentation/conversion, not the current register value.', 'The configuration collection is distinct from /api/pwm output readings.', 'Show the same configured output type and gamma behavior in every client.'],
  '/api/commands': ['List controller commands', 'Read the shared command vocabulary used by CLI, TUI and the REST command dispatcher. This is not an OS executable list.', 'Catalog entries describe supported commands and syntax; discovery does not run them.', 'Use advertised commands for a command palette instead of guessing text grammar.', 'controller.command.catalog'],
  '/api/program-state': ['Read program ownership', 'Read the host-owned playback/recording state that coordinates application use of the board. This is unrelated to a firmware programming transaction.', 'The state reports active owners/modes; a paused UI is not proof that another owner has released the board.', 'Inspect ownership when an authoring application cannot acquire runtime control.', 'controller.program_state.get'],
  '/api/menu/catalog': ['List board front-panel pages', 'Query the connected board’s advertised menu pages and IDs. Page availability depends on this firmware and attached capabilities.', 'Use returned page IDs as identifiers, not their display order.', 'Populate a front-panel page chooser without hard-coding firmware menu indices.', 'controller.menu.list'],
  '/api/menu/layout': ['Read the board menu layout', 'Read the board’s current visibility mask and ordered menu page identifiers.', 'visible_mask is a bit field; order is the byte-sequence encoding used by the Go JSON wire contract, not a free-form list of captions.', 'Read the current layout before editing visibility or page order.', 'controller.menu.layout.get'],
  '/api/host-menus': ['Read host-presented menus', 'Read the configured host menu directory that supplements board-owned menu pages.', 'These entries are host configuration; they do not imply the board is connected or displaying a particular page.', 'Offer host features on the physical front panel while preserving firmware pages.', 'controller.host_menu.config.get'],
  '/api/os/status': ['Read host OS capabilities', 'Read operating-system status and configured policy for host integrations. The target is the computer running this host.', 'Reported support/policy determines whether keyboard and power actions are available; it is not board status.', 'Only offer OS actions that the selected host actually supports.', 'controller.os.status'],
  '/api/os/facts': ['Query a host-facts profile', 'Run one bounded, allowlisted read-only PC information profile. profile=list or catalog returns profile definitions; arbitrary queries are not accepted.', 'A profile result describes the host PC. Sensor data from lm-sensors or Libre Hardware Monitor is separate from PCB telemetry.', 'Request the sensors profile for PC hardware monitoring, or list to discover supported profiles.', 'controller.os.facts'],
  '/api/os/key': ['Send an allowed host key action', 'Request a validated keyboard action on the host desktop under its OS policy. This does not press one of the board’s four physical buttons.', '202 reports accepted host action processing, not evidence that an arbitrary foreground application consumed the key.', 'Map an explicitly configured automation to a supported media key.', 'controller.os.key'],
  '/api/os/power': ['Request a confirmed host power action', 'Request a supported system/display action under host policy and confirmation rules. The HTTP path forces a non-automation request.', '202 is host action acceptance; shutdown may end the connection before a final observation. Failure does not imply the machine powered off.', 'Use a deliberate confirmed power control, never a connection-health probe.', 'controller.os.power'],
  '/api/command': ['Execute a controller command', 'Run one command from the shared controller vocabulary after semantic capability and lock checks. It never invokes a system shell.', 'output is command text; an error response can also include partial output and does not establish that nothing happened.', 'Use /api/commands to obtain syntax, or prefer a typed RPC method in new clients.', 'controller.command.execute'],
  '/api/messages': ['Submit a typed application message', 'Submit text with structured type/source metadata to the message pipeline. Inbound access is tagged by the host.', '202 returns the accepted event, not proof that a desktop notification or board display showed it.', 'Integrate a trusted event producer without issuing hardware display commands.', 'controller.message.send'],
  '/api/display': ['Present text on a board display', 'Send a timed text presentation to the physical seven-segment or LCD display. Scroll/repeat settings belong to that display presentation.', 'The result describes the requested presentation; it is not a desktop OSD notification or camera proof of displayed pixels.', 'Present a short status on the front panel; use /api/osd for the computer desktop.', 'controller.display.send'],
  '/api/opcode': ['Exchange a board protocol frame', 'Send a one-byte opcode and at most one payload encoding, then wait for the selected response opcode. Opcode numbers come from Project/ProtocolContract.json.', 'The response is an opaque UART frame: opcode/name identify it, sequence correlates it, payload and payload_hex encode the same bytes. ACK defaults to 0x80.', 'Use for protocol diagnostics when no typed operation exists; never probe unknown opcodes on active hardware.', 'controller.opcode.exchange'],
  '/api/app/bridge': ['Read the coordinator instance', 'Identify the original process coordinating application surfaces and its bounded process self-information.', 'This is the coordinator’s instance report, not a list of all peers or a request to spawn a process.', 'Diagnose which host owns a group of TUI/WebUI/desktop instances.', 'controller.app.bridge'],
  '/api/app/instances': ['List live application instances', 'List leased application surface reports, or supply id for one exact instance. Reports expire; an old ID does not remain a valid target forever.', 'Without id the result is an array; with id it is one instance. Lease/state describes registry presence, not completion of a command.', 'Choose an exact active WebUI/TUI surface before routing an application action.', 'controller.app.instances'],
  '/api/app/action': ['Route a correlated application action', 'Resolve the selector once and freeze the exact live target set. Each target reports delivery and application outcome independently.', '202 means an operation was created, not applied. operation_id correlates progress; per-target applied/rejected/unknown outcomes are the authoritative result. 409 means all resolved targets were rejected.', 'Change a page on one exact surface and follow /api/app/action/outcome rather than assuming delivery equals application.', 'controller.app.action'],
  '/api/app/action/ack': ['Acknowledge one application target', 'Report applied or rejected for one exact target in an existing app-action operation. This is the target’s receipt, not a way to create an action.', 'The correlated operation is returned. Unknown, expired, foreign or conflicting acknowledgements produce 409.', 'A client acknowledges only after applying the received action to its own surface.', 'controller.app.action.ack'],
  '/api/app/action/outcome': ['Read application action outcomes', 'Inspect one retained app-action operation by operation_id without re-delivering it.', 'Per-target outcomes distinguish accepted delivery from applied/rejected/unknown. 404 means the operation is unknown or its bounded retention expired.', 'Recover the result after a client connection interruption without repeating the original action.', 'controller.app.action.outcome'],
  '/api/app/navigate': ['Navigate application surfaces', 'Request a canonical page for all, one surface kind or one exact live application instance. This changes UI navigation, not board menu wiring.', 'The coordinator returns routing/navigation state; independently hosted surfaces may still need to apply the change.', 'Synchronize a selected control page across the intended operator surfaces.', 'controller.app.navigate'],
  '/api/app/launch': ['Show a named application surface', 'Ensure, launch or focus an allowlisted TUI/WebUI surface. No arbitrary executable or shell command is accepted.', 'The result describes the requested surface lifecycle action; support depends on the receiving host desktop.', 'Restore the console/TUI from a desktop client without starting a second board owner.', 'controller.app.launch'],
  '/api/bridges': ['List configured peer bridges', 'Read the bridge manager’s configured peer states without connecting to or controlling an arbitrary advertised address.', 'Entries describe configured links; an available bridge does not guarantee the remote board is attached.', 'Choose a named configured peer before a typed relay call.', 'controller.bridge.list'],
  '/api/bridges/call': ['Relay a request to a configured peer', 'Forward a non-recursive JSON-RPC request to one named configured peer. The caller’s nested auth value is removed; bridge credentials are managed by the host.', 'peer identifies the selected link and response is the peer’s JSON-RPC envelope. Inspect its result/error separately from the outer HTTP response.', 'Read a peer snapshot through the existing bridge instead of opening a second connection from a restricted client.', 'controller.bridge.call'],
  '/api/artifacts/manifest': ['Read local update inventory', 'Read bundled defaults, current identities, verified readbacks and programming policy in one call. A stored firmware image is not automatically the running image.', 'defaults are recovery candidates; current.flash_readback is verified board data. board/comparison report identity; policy and update describe admission/progress.', 'Build an updater that distinguishes a bundled candidate from an actual device backup.', 'controller.artifact.manifest'],
  '/api/artifacts': ['List stored artifacts', 'List immutable content-addressed files, optionally filtering by kind. This does not download remote candidates or program a target.', 'artifacts contains descriptors with SHA-256, byte count and provenance; presence alone does not mean installed.', 'Select an exact staged digest for an explicit update request.', 'controller.artifact.list'],
  '/api/artifacts/upload': ['Import artifact bytes', 'Store raw bytes or a multipart file in the immutable artifact catalog. kind is a query parameter, not a JSON field; this route does not flash the board.', '201 returns an operation/artifact receipt after validation. SHA-256 identifies the imported bytes; it is not a source commit hash.', 'Upload a firmware file, retain artifact.sha256, then separately authorize /api/board/firmware/upload.', 'controller.artifact.upload'],
  '/api/artifacts/fetch': ['Fetch a remote artifact', 'Queue a bounded download into the immutable store with optional digest/size expectations and transient upstream credentials.', '202 returns an operation and progress stream. Read terminal success and the resulting artifact before any update.', 'Stage a known release file by URL without yet taking programming ownership.', 'controller.artifact.fetch'],
  '/api/artifacts/capture': ['Capture a board memory backup', 'Explicitly authorize readback of selected flash/EEPROM components through the selected programming transport. Readback can interrupt normal board ownership.', '202 queues capture; only a successfully verified artifact is a usable backup. A queued job is not a completed backup.', 'Capture before a deliberate recovery workflow; use the returned operation ID to follow progress.', 'controller.artifact.capture'],
  '/api/artifacts/{kind}/{sha256}': ['Download an immutable artifact', 'Read the exact stored bytes selected by kind and SHA-256. Supports HEAD and HTTP byte ranges; no new board readback is performed.', '200 is the whole file, 206 a byte range, 304 an unchanged cache validator, 416 an unsatisfiable range. ETag and X-Checksum-SHA256 identify the file.', 'Resume a verified backup download without recapturing the board.'],
  '/api/artifacts/current/{kind}': ['Download the latest verified readback', 'Serve the latest explicitly captured flash or EEPROM backup. This never manufactures a backup from a bundled firmware candidate.', '404 means no verified current readback exists. The file/digest headers describe captured bytes, not fresh measurements at download time.', 'Retrieve an existing backup without interrupting normal operation again.'],
  '/api/board/firmware/upload': ['Program staged board firmware', 'Queue guarded programming of an already-stored firmware artifact. artifact_sha256 selects bytes in the local store; authorized must explicitly permit the operation.', '202 returns operation/progress information. Follow terminal verification, including restart/handshake where reported; acceptance is not completed flashing.', 'First import/stage firmware, then explicitly program its digest and retain the operation ID.', 'controller.board.firmware.upload'],
  '/api/board/firmware/download': ['Download the verified flash backup', 'Serve the latest successful flash-only board readback without starting another capture.', '404 means no verified flash readback exists. The response contains backup bytes, not a queued job or the bundled firmware default.', 'Retrieve the backup produced by a completed POST to this same route.'],
  '/api/updates/{kind}': ['Apply a staged update', 'Queue an explicitly authorized firmware, EEPROM or host update using an immutable artifact digest. The route kind selects the target operation.', '202 identifies a job, not completion. Follow operation progress and terminal error/success; host replacement may restart this listener.', 'Stage first, compare identities, then update deliberately with a stable idempotency key.'],
  '/api/restores/flash': ['Restore a captured flash backup', 'Queue guarded recovery from a stored flash-backup artifact. This is not the path for a generic firmware candidate.', '202 returns a durable operation; only terminal verification can establish successful restoration.', 'Recover from an explicitly selected verified readback, preserving the operation receipt.', 'controller.restore.flash'],
  '/api/updates/status/{id}': ['Read update job progress', 'Read one retained artifact/update operation by its opaque ID without restarting it.', 'state reports lifecycle; progress_percent is meaningful with progress_known and describes the current stage. error/detail explain failures; bytes_done/total describe measured transfer where available.', 'Reconnect after a transport interruption and recover the same job outcome.', 'controller.update.status'],
  '/api/discovery/github/workflow': ['Find GitHub Actions build artifacts', 'Query successful runs in owner/repository, optionally filtered by branch and workflow display name; select the newest matching run and list its non-expired platform-matching artifacts. This does not run a workflow, download its files or install anything.', 'source and checked_at identify the inventory query; candidates carries archive metadata and build provenance. archive_sha256 hashes the outer archive, while sha256 (when known) identifies extracted bytes. An empty match is an error, not an installed/up-to-date result.', 'Find the latest successful Windows host build, compare candidates with /api/discovery/check, then stage one candidate explicitly.', 'controller.discovery.github.workflow'],
  '/api/discovery/github/release': ['Find GitHub release assets', 'Query a specific release tag or the latest eligible release, then select compatible downloadable assets. include_prerelease affects automatic release selection; it does not publish a release.', 'candidates contains remote file identities/provenance, not verified local artifacts or proof of a running version.', 'Offer published releases in an updater without downloading them until the operator selects one.', 'controller.discovery.github.release'],
  '/api/discovery/manifest': ['Read the local software manifest', 'Serve this host’s portable inventory for other update clients. POST to the same route validates a remote manifest instead.', 'artifacts describes advertised downloadable candidates; it is not the internal artifact/default/current manifest.', 'Let another controller discover software offered by this host.', 'controller.discovery.local_manifest'],
  '/api/discovery/check': ['Compare software identities', 'Compare the supplied current identity with compatible candidates by kind/platform and build metadata. It does not read the running board afresh or perform an update.', 'status and reason explain selection; candidate, when present, is the chosen comparison result, not a staged or installed image.', 'Pass an inventory plus the actual current identity before showing an update action.', 'controller.discovery.check'],
  '/api/discovery/stage': ['Stage a software candidate', 'Queue download, archive handling and verified import of one candidate. It changes the local artifact store but never flashes hardware or replaces the running host.', '202 returns operation.id; inspect /api/discovery/status/{id}. A successful operation includes the local artifact descriptor needed by the separate updater.', 'Select a candidate from discovery, stage it, then authorize an update using the verified artifact digest.', 'controller.discovery.stage'],
  '/api/discovery/status/{id}': ['Read candidate staging progress', 'Inspect one discovery/staging job without repeating the remote download.', 'operation state/progress and artifact describe staging only. They do not claim the board was programmed or a host executable started.', 'Recover staging progress after reconnecting, then present an explicit Apply action.', 'controller.discovery.status'],
  '/api/webhooks/inbound': ['Receive a typed integration webhook', 'Accept the configured inbound message format only when inbound webhooks are enabled. It enters the typed message pipeline rather than acting as an unrestricted command proxy.', 'Disabled inbound delivery returns 404. An accepted message/event is not proof of a downstream physical action.', 'Connect a trusted event producer to message handling without granting arbitrary board commands.'],
  '/api/webhooks/outbound/status': ['Read webhook queue health', 'Read bounded outbound delivery and dead-letter counters without retrying or deleting deliveries.', 'Pending counters are work awaiting delivery; dead-letter counters need operator review, not automatic repeated replay.', 'Diagnose a failing webhook destination before choosing specific deliveries to retry.', 'controller.webhooks.status'],
  '/api/webhooks/outbound/pending': ['List pending webhook deliveries', 'List a bounded non-secret view of queued outbound deliveries. limit is 1–100; omission uses the service default.', 'Entries describe pending delivery metadata, not target acknowledgement of a business action.', 'Inspect queue age/destination before investigating network or receiver failures.', 'controller.webhooks.pending'],
  '/api/webhooks/outbound/dead': ['List dead-letter webhook deliveries', 'List bounded non-secret deliveries that exhausted normal handling. Reading does not replay them.', 'Delivery IDs select individual retained dead letters; this is separate from the pending queue.', 'Choose exact failed deliveries after correcting a destination.', 'controller.webhooks.dead'],
  '/api/webhooks/outbound/replay': ['Replay selected dead letters', 'Explicitly requeue selected failed outbound deliveries. Replaying can duplicate a receiver-side action if a prior acknowledgement was lost.', 'The result reports the queue operation, not successful delivery to the destination.', 'Retry only reviewed delivery IDs after fixing the underlying receiver problem.', 'controller.webhooks.replay'],
  '/api/webhooks/outbound/clear': ['Delete selected dead letters', 'Remove explicitly selected dead-letter records without sending them again. This discards retry history for those records.', 'The result reports queue removal, not delivery or cancellation of any already-executed receiver action.', 'Discard reviewed obsolete failures without touching unrelated pending work.', 'controller.webhooks.clear'],
  '/api/integrations/status': ['Read effective integration routing', 'Inspect requested/effective buzzer integration routing and playback state so a client can distinguish configuration from the active route.', 'Requested routing is operator intent; effective routing/playback reflects what this host can currently use.', 'Diagnose why a buzzer mirror is silent without playing a test tone.', 'controller.integrations.status'],
  '/api/integrations/datahub/{path}': ['Forward a confined data-service request', 'Proxy only the configured loopback data-service namespace with sanitized routing/headers. This is not an arbitrary URL fetch endpoint.', 'Status/body belong to the upstream data-service operation; the schema depends on the selected confined path.', 'Access an installed local adapter through the host’s configured boundary.'],
  '/api/integrations/device/{path}': ['Inspect the disabled generic device route', 'This generic HTTP device route fails closed. Use controller.device.status, controller.device.inspect or controller.device.action with their typed contracts.', 'Rejection is intentional, not a missing device implementation. No generic device mutation is forwarded.', 'Migrate a legacy client to typed RPC so device actions receive semantic policy checks.'],
  '/api/osd': ['Read desktop overlay status', 'Inspect Windows OSD support, current or retained presentation and its terminal outcome without displaying anything. An optional id reads an exact notification.', 'supported describes platform availability; active is current visibility. text_renderer identifies DirectWrite or GDI fallback. Action/dismissal outcomes are separate from hardware state.', 'Use an exact returned id to observe one script notification without confusing it with a replacement.', 'controller.osd.status'],
};

const mutations = {
  'put /api/estop': ['Change the effect and motion interlock', 'Set active to engage/release the host interlock; source/reason record who requested it. Releasing does not replay stopped work.', 'Returns the authoritative interlock state; 409 includes the state when the transition failed.', 'Engage before a controlled intervention, then release only when the operator judges it safe.', 'controller.estop.set'],
  'put /api/peripherals': ['Replace host-owned channel labels', 'Replace peripheral_names with a map from stable peripheral IDs to captions. Omitted map entries are not a partial rename patch.', 'Returns the resulting peripheral presentation; no physical channel value changes.', 'Read the existing names, edit one caption locally, then submit the complete intended map.', 'controller.peripherals.set'],
  'put /api/pwm': ['Set one PWM output', 'Set channel 0–15 with exactly one of calibrated percent (0–100) or raw_value (0–4095). This REST compatibility route does not accept the revision pins of controller.pwm.set; prefer typed fenced writes for a new mixer.', 'Returns authoritative PWM readback; 409 can include outcome details. A lost response is not proof that the output stayed unchanged.', 'Convert a deliberate slider gesture into a final setpoint; do not replay a timed-out hardware write.'],
  'delete /api/pwm': ['Turn off all PWM outputs', 'Clear PWM outputs through the current board owner. There is no per-channel selector on this REST DELETE.', 'Returns authoritative readback; it does not prove that external loads are electrically isolated.', 'Use only for an explicit all-PWM-off action, not to remove channel configuration.'],
  'put /api/pwm/channel': ['Configure one PWM channel', 'Persist a partial channel configuration: output type, icon, curve/gamma, step size and full/restore on behavior. Omitted fields are preserved.', 'Returns the resulting channel configuration, not a changed physical setpoint.', 'Set on_behavior=restore so a later On action can use the remembered brightness.', 'controller.pwm.channel.configure'],
  'put /api/program-state': ['Change program ownership', 'Set the requesting owner’s program mode and reason. Empty owner defaults to rest; mode must be a supported program mode.', 'Returns updated ownership state; 409 means the requested ownership transition was rejected.', 'Coordinate playback/recording ownership before running a hardware timeline.', 'controller.program_state.set'],
  'put /api/menu/layout': ['Save the board menu layout', 'Persist a validated visible_mask and order using IDs from the live catalog. This changes front-panel navigation, not output values.', 'Returns board layout readback; 409 indicates rejected/failed persistence.', 'Reorder only advertised page IDs and retain pages required by board policy.', 'controller.menu.layout.set'],
  'put /api/host-menus': ['Replace host-presented menus', 'Persist a complete HostMenuConfig directory. This is a replacement of the directory, not a single-page patch.', 'Returns saved host configuration; unavailable persistence is 503 and conflicts are 409.', 'Add a host-presented front-panel page while retaining existing directory entries.', 'controller.host_menu.configure'],
  'post /api/app/instances': ['Report a live application instance', 'Create/refresh one leased report with a stable id, surface and bounded state. lease_seconds is capped at 300; clients must renew while alive.', 'Returns the normalized live report; reporting does not launch an application or execute a board command.', 'Register a new WebUI surface before it receives routed application actions.', 'controller.app.instance.report'],
  'delete /api/app/instances': ['Remove an application instance report', 'Remove the exact id query target from the coordinator registry. This unregisters a surface; it does not kill a process.', 'The result describes registry removal, not an application shutdown acknowledgement.', 'Unregister on orderly client close rather than waiting for lease expiry.', 'controller.app.instance.remove'],
  'post /api/board/firmware/download': ['Capture a fresh board flash backup', 'Explicitly authorize a new flash-only readback. Omit components or use only flash; EEPROM belongs to the general capture route.', '202 returns an operation/progress stream, not file bytes. After terminal success, GET this route retrieves the verified backup.', 'Make a deliberate fresh backup before recovery; do not poll POST to check completion.', 'controller.board.firmware.download'],
  'post /api/discovery/manifest': ['Discover a remote software manifest', 'Fetch and validate a portable manifest from url using optional transient upstream credentials. This does not stage or install its entries.', 'Returns a timestamped candidate inventory; candidates must still be selected and staged.', 'Use a private release manifest as an alternative to GitHub discovery.', 'controller.discovery.manifest'],
  'post /api/osd': ['Show a desktop overlay', 'Present a non-activating Windows OSD with title/body/icon, optional actions, lifetime, position and stacking/replacement policy. Action URIs run only after human activation.', 'Returns presentation status/identity. Accepted display is not action activation; retain id to wait for dismissal/action or to dismiss only that overlay.', 'Show a script’s completion summary without stealing focus; enable sound/taskbar flash only when wanted.', 'controller.osd.show'],
  'delete /api/osd': ['Dismiss a desktop overlay', 'Request the normal fade-out transition. Supply id to avoid an obsolete script closing a newer replacement notification.', 'Returns updated OSD status; closing the overlay does not execute its action buttons.', 'Cancel one script notification by the id returned when it was shown.', 'controller.osd.dismiss'],
};
for (const path of ['/api/estop', '/api/program-state', '/api/menu/layout', '/api/host-menus']) mutations['post '+path] = mutations['put '+path];

export function routeMeaning(path, method) {
  const value = mutations[method+' '+path] ?? entries[path];
  if (!value) throw new Error(`Missing semantic documentation for ${method.toUpperCase()} ${path}`);
  return { title:value[0], description:value[1], result:value[2], useCase:value[3], rpc:value[4] };
}

// Precise shared wire vocabulary, used only when no more-specific description
// exists. Context-dependent names (value, mode, kind, state) are not guessed.
const fieldMeaning = {
  repository:'GitHub owner/repository, for example atomicdeploy/PCController; not a web page URL.',
  branch:'Optional Git branch filter for successful workflow runs; omission considers all branches.',
  workflow:'Optional workflow display name, matched case-insensitively against run.Name; not the YAML filename.',
  platform:'GOOS/GOARCH selector such as windows/amd64. Host artifacts are platform-specific; firmware selection is not a host executable selection.',
  bearer_token:'Optional transient credential for the upstream download/GitHub service, not a login token for this PCController listener. Never include it in URLs or logs.',
  api_base_url:'Optional GitHub API base, normally https://api.github.com; subject to remote URL validation.',
  include_prerelease:'Allow a prerelease when automatically choosing a release. An explicit tag selects that release instead.',
  tag:'Optional exact GitHub release tag. Omission uses the latest eligible release selection.',
  sha256:'SHA-256 digest of the artifact bytes, 64 hexadecimal characters; distinct from a source build hash.',
  artifact_sha256:'Digest of an already-verified artifact in this host’s immutable store. This is not an upload payload or a URL.',
  archive_sha256:'Digest of the outer archive download, not the extracted artifact.',
  archive_bytes:'Expected outer archive length in bytes; distinct from extracted artifact bytes.',
  archive_path:'Optional selected member path inside the archive, subject to confined extraction rules.',
  archive:'Whether the remote download is an archive requiring bounded extraction before import.',
  build_hash:'Build/source identity used in provenance and comparison; it is not necessarily the SHA-256 of the executable or firmware file.',
  build_timestamp:'Build time metadata, normally an RFC 3339 timestamp; not a semantic version number.',
  packed_timestamp:'Unsigned 32-bit packed board build timestamp from producer metadata. Preserve it as supplied; do not substitute Unix milliseconds.',
  workflow_run_id:'GitHub Actions run ID providing provenance for this candidate.',
  checked_at:'UTC time this remote inventory was queried, not when its software was installed.',
  created_at:'Creation timestamp supplied by this record’s producer; not proof of installation.',
  generated_at:'Time the inventory document was generated.',
  candidates:'Remote software candidates returned by discovery; selection does not verify, stage or install them.',
  candidate:'One selected remote candidate with its provenance and integrity expectations; pass the unmodified discovered record for staging.',
  candidate_id:'Identity of the candidate associated with this staging job.',
  authorized:'Explicit consent to the requested programming/readback transaction. Must be true for guarded operations; it does not bypass host policy.',
  idempotency_key:'Caller-generated identity for this one logical job. Reuse the same key only to recover that job after an uncertain response; do not assign a new key to retry it blindly.',
  components:'Device memory components to capture (flash and/or eeprom); board firmware download only permits flash.',
  reinitialize_eeprom:'Explicit development-only data-loss exception: retain the raw pre-flash backup but do not restore incompatible semantic settings after the new firmware authenticates.',
  deployment:'Host-updater deployment selector; only supported deployment modes are accepted by the updater.',
  progress:'How to follow update events: transport/url locate the stream, event_prefix selects update notifications, operation_id correlates this job.',
  progress_percent:'Progress percentage for the reported stage. Do not show it as measured whole-transaction progress when progress_known is false.',
  progress_known:'Whether measured progress is available for the current stage; false is indeterminate, not zero percent completed.',
  bytes_done:'Measured bytes transferred for the reported operation/stage.',
  bytes_total:'Known total bytes for the reported operation/stage; absence does not imply an empty transfer.',
  started_at:'When this operation began, as a host timestamp.', updated_at:'When this operation status last changed.',
  stage_started_at:'When the currently reported programming stage began.',
  operation_id:'Opaque job/action correlation ID returned by its initiating request; use it to observe the same operation, not start another.',
  event_prefix:'Event-name prefix for this operation’s progress notifications.',
  reused:'True when this response refers to an existing idempotently identified job instead of creating another.',
  artifact:'Verified local artifact descriptor produced/selected by the operation; absent until an artifact is available.',
  artifacts:'Artifact descriptors; inspect kind, exact digest, byte count and provenance before selecting one.',
  verified_readback:'True only for verified bytes captured from device memory; a bundled candidate is not a device backup.',
  embedded:'Whether the artifact originated from this host’s bundled defaults.',
  download_url:'Host-relative or advertised URL for retrieving the exact descriptor’s bytes; retain its digest for verification.',
  media_type:'MIME type of the stored artifact bytes.',
  expected_host_instance_id:'Exact current host process identity from the selected snapshot; rejects writes addressed to a replaced host.',
  expected_connection_generation:'Current board connection generation from the selected snapshot; fences writes against reconnects.',
  expected_revision:'Opaque current resource revision from the selected host; stale values reject the change rather than overwrite newer work.',
  expected_pwm_configuration_revision:'Opaque current PWM curve/configuration revision; prevents applying a percentage using an obsolete conversion.',
  if_match:'Canonical ETag/revision returned by the corresponding read. Required by compare-and-swap setters; never manufacture or numerically increment it.',
  board_identity:'Exact identity of the attached board used to fence this operation; a renamed channel caption is not a board identity.',
  host_instance_id:'Identity of the host process that produced this observation; changes when that process is replaced.',
  connection_generation:'Monotonic board-session generation within the host; a new connection invalidates old session pins.',
  pwm_configuration_revision:'Opaque revision of PWM conversion/configuration; retain with the matching snapshot.',
  percent_milli:'Logical percent in thousandths: 0–100000 represents 0–100%. Curve-aware writes also require the current PWM configuration revision.',
  raw_value:'Raw 12-bit PWM register value, 0–4095. Do not combine with percent.',
  percent:'Calibrated PWM percentage, 0–100; converted using the channel’s configured curve. Do not combine with raw_value.',
  peripheral_names:'Complete map of stable peripheral identifiers to host-owned captions; replacing this map does not change physical output values.',
  lease_seconds:'Application registry lease lifetime in seconds, capped at 300; renew while the surface remains alive.',
  surface:'Application surface identity, for example a TUI or WebUI; used to select UI targets, not a serial port.',
  page:'Canonical application page identifier, not an arbitrary URL or executable.',
  visible_mask:'Bit mask selecting visible board menu IDs; obtain IDs from the current menu catalog.',
  payload:'Base64-encoded UART payload bytes. Supply payload or payload_hex, never both.',
  payload_hex:'Hexadecimal UART payload bytes. Supply payload_hex or payload, never both.',
  expect_opcode:'Response opcode to await; default 128 (0x80, ACK). An ACK does not necessarily prove an external load changed.',
  opcode:'One-byte protocol operation identifier from the canonical opcode contract; unknown operations are not safe probes.',
  sequence:'UART frame correlation sequence, 0–255; not an event-history ID.',
  observed_value:'Register readback observed after the admitted write. This is not electrical measurement.',
  outcome:'Operation result strength; unknown means the request may already have taken effect and must not be blindly replayed.',
  error:'Failure detail when present. Inspect the enclosing operation state/outcome; transport failure alone does not prove non-actuation.',
  reason:'Explanation of the selection, refusal or state transition; not a machine-stable status code.',
  detail:'Additional human-readable progress/error context; use typed state fields for control flow.',
  text_renderer:'Effective Windows text rasterizer: directwrite is the upgraded renderer; gdi is the compatibility fallback.',
};

export function explainFields(schema, overrides={}) {
  if (!schema || typeof schema !== 'object') return schema;
  const context={...typeFields[schema['x-go-type']],...overrides};
  for (const [name, value] of Object.entries(schema.properties ?? {})) {
    if(value===false) continue;
    const child=value===true?(schema.properties[name]={}):value;
    if (!child.description && (context[name]??fieldMeaning[name])) child.description = context[name]??fieldMeaning[name];
    explainFields(child);
  }
  for (const key of ['items','additionalProperties']) if (schema[key] && typeof schema[key]==='object') explainFields(schema[key]);
  for (const key of ['allOf','anyOf','oneOf']) for (const child of schema[key] ?? []) explainFields(child);
  return schema;
}

export function explainContract(name,schema) {
  return explainFields(schema, contractFields[name]);
}

const contractFields={
  OSDRequest:{id:'Optional caller notification identity, used for exact status/dismissal.',title:'Optional heading; omission uses the configured application title.',icon:'Semantic built-in icon identity, not a file path.',severity:'Semantic severity used for presentation.',duration_ms:'Lifetime in milliseconds; 0 selects the default 5000 ms, not an infinite overlay.',position:'Placement in the monitor work area; default is bottom-center.',theme:'System, dark or light presentation palette.',reduce_motion:'Disable motion-heavy transitions/marquee behavior for this request.',behavior:'replace dismisses the current overlay; stack retains concurrent notifications within the bounded stack.',background:'current uses the normal card surface; none/transparent removes it; acrylic/blur requests a blurred desktop backdrop.',actions:'Up to five buttons activated only by an explicit human click.'},
  OSDAction:{label:'Visible action-button caption.'},
  OSDStatus:{supported:'Whether this platform supports the native overlay.',active:'Whether this notification is currently presented.',accepted:'Cumulative count of accepted show requests, not an output acknowledgement.',id:'Identity of this current/retained notification.',shown_at:'Time this notification was presented.',expires_at:'Scheduled presentation expiry.',last_action:'Last explicitly activated action URI, when available.',last_action_id:'Script choice ID activated by the user.',dismissal:'Terminal reason: expired, dismissed, action, replaced, closed or failed.',last_error:'Last presentation/activation failure.',active_count:'Number of currently presented stacked notifications.',items:'Bounded stack entries, not a second independent show request.',dpi:'Effective monitor DPI used for rasterization/layout.',pixel_width:'Rendered physical pixel width.',pixel_height:'Rendered physical pixel height.',background:'Effective backdrop after platform support/fallback.',attention:'Sound/taskbar attention outcome, independent of visual display success.'},
  JSONRPCRequest:{jsonrpc:'Required JSON-RPC protocol marker 2.0.',id:'Caller correlation ID echoed in the response; notifications omit it.',method:'Exact callable method name from the JSON-RPC catalog.',params:'Method-specific parameters; inspect the linked method schema rather than sending all fields shown in other methods.'},
  SessionTicketRequest:{transport:'Reserved transport selection; tickets are disabled in this alpha.'},
  AppInstanceReport:{id:'Stable identity of this live UI surface, not a PID.',state:'Current surface lifecycle state: active, hidden, background or leaving.',values:'Bounded non-secret surface state metadata.',self:'Optional bounded process self-information for diagnostics.'},
  AppInstance:{registered_at:'First registry registration time.',expires_at:'Lease expiry; an expired instance is not a current action target.'},
  InstanceSelf:{kind:'Application/process kind.',pid:'Reporting process ID on the receiving host.',parent_pid:'Parent process ID, when known.',image_path:'Reported executable path; not permission to run an arbitrary file.',working_directory:'Reported process working directory.',vars:'Bounded non-secret process/environment metadata.'},
  AppNavigation:{target:'Application selector: all, a supported surface, or an exact instance as admitted by the coordinator.'},
  AppSurfaceLaunch:{mode:'Lifecycle intent: ensure, launch or focus for an allowlisted surface.',target:'Application selector admitted by the coordinator, not an arbitrary executable.'},
  AppAction:{kind:'Typed action vocabulary; app.* is reserved to coordinator actions and other allowed dotted kinds are extension intents.',value:'Action-specific value, for example a canonical page for app.page.',source:'Originating application identity.',target:'Selector resolved once to exact live instances; * selects the live set.',at:'Action creation timestamp.'},
  ActionAck:{delivery_id:'Exact coordinator-owned delivery ID from the received action.',instance_id:'This receiving instance identity, not another target’s ID.',state:'applied after the target actually applies the action, or rejected with a reason.'},
  ActionTargetOutcome:{instance_id:'Exact frozen target instance.',state:'queued awaits acknowledgement; applied/rejected are target receipts; timeout means no timely receipt, not proof of non-application.'},
  ActionOperation:{selector:'Original target selector before freezing the target set.',kind:'Requested action kind.',value:'Requested action value.',source:'Originating caller/application.',targets:'Per-target delivery/application outcomes.',expires_at:'End of this retained operation’s acknowledgement/retention window as reported by the coordinator.'},
  ActionOperationEnvelope:{operation:'Correlated operation with frozen targets and their current outcomes.'},
  SemanticChannel:{key:'Stable semantic binding key, independent of user caption.',kind:'relay or pwm; constrains eligible physical targets.',target:'Physical output bound to this semantic key.',name:'Renamable user caption.',icon:'Semantic icon ID for clients.'},
  ChannelRouting:{enabled:'Whether semantic bindings are active.',channels:'Complete semantic binding list.'},
  ChannelRoutingUpdate:{routing:'Complete replacement routing catalog; this operation does not actuate outputs.'},
  ChannelRoutingCatalog:{board_profile:'Current physical-board wiring/presentation profile.',routing:'Configured semantic binding table.',targets:'Eligible physical outputs and their descriptors.'},
  PinnedOutputResult:{accepted:'True only for this confirmed pinned-write receipt.',target:'Exact physical output written.',value:'Raw register setpoint; relay values are 0/1, PWM values 0–4095.',revision:'Observation/revision associated with the verified write.'},
  BoardSensorReading:{id:'Stable PCB sensor identity.',label:'Human-readable sensor label.',kind:'Measurement dimension.',unit:'Declared engineering unit: V, A, W or C.'},
  BoardSensorSnapshot:{source:'Fixed PCB status-frame source, not a host PC sensor provider.',connected:'Whether the board session is connected.',fresh:'Whether the original status frame remains inside its five-second freshness window.',readings:'PCB measurements; null values are unavailable, not zero.'},
  JSONRPCError:{code:'JSON-RPC failure code; see the RPC error-code catalog.',message:'Human-readable failure; use typed code/data for control flow.',data:'Optional method-specific error/outcome detail. Absence never establishes that a hardware command did not run.'},
  Error:{correlation_id:'Optional host correlation ID for diagnostic matching.'},
};
contractFields.AppActionRequest=contractFields.AppAction;

const requestFieldMeaning={
  '/api/estop':{active:'True engages the interlock; false releases admission without resuming work.',source:'Caller/source label recorded with this change.'},
  '/api/pwm':{channel:'Physical PWM channel index 0–15. This is not the display caption.'},
  '/api/pwm/channel':{channel:'Physical PWM channel index 0–15.',...typeFieldsForPWM()},
  '/api/program-state':{owner:'Application claim owner; omission/empty uses rest.',mode:'Application program state, Idle or Running (accepted aliases are normalized by the host).'},
  '/api/menu/layout':{order:'Base64-encoded Go []byte containing the complete page-ID permutation; obtain IDs from the live catalog.'},
  '/api/command':{command:'One command in the advertised controller grammar; not a system shell command.'},
  '/api/bridges/call':{peer:'Exact configured bridge peer name.',request:'Nested non-recursive JSON-RPC request; caller auth is stripped before relay.'},
  '/api/app/launch':{mode:'Lifecycle intent: ensure, launch or focus, as supported by the selected named surface.',target:'Application surface/instance selector admitted by the coordinator, not an arbitrary executable.'},
  '/api/discovery/manifest':{url:'Remote HTTP(S) manifest location subject to metadata-size and URL validation. This is not a local file path.'},
};
function typeFieldsForPWM(){return {icon:'Semantic icon ID used in channel presentation.',output_type:'Load/output presentation type; it does not rewire the board.',curve:'Configured conversion curve between logical percent and raw PWM.',gamma:'Exponent used by the selected gamma curve.',step_percent:'Logical percentage step for increase/decrease actions.',on_behavior:'full turns on at full output; restore uses the remembered nonzero setpoint.'};}

const artifactFields={kind:'Artifact validation/storage class: firmware, eeprom, flash-backup or host-executable.',name:'Display/download filename for this immutable file; not a local filesystem path.',bytes:'Expected or observed artifact byte length; discovery metadata can omit an unknown length.',url:'Remote HTTP(S) artifact location subject to bounded download and URL validation.',source:'Artifact/discovery provenance, not a board connection endpoint.',metadata:'Bounded non-secret producer provenance; never supply credentials in metadata.'};
const candidateFields={...artifactFields,id:'Opaque discovery candidate identity; preserve it when staging the returned record.',artifact_name:'Original producer artifact name before download filename/archive handling.',release_tag:'GitHub release tag providing provenance for this candidate.'};
const updateFields={id:'Opaque durable update/capture job identity; retain it across transport interruption.',kind:'Job target/operation kind, not a claim about an installed version.',state:'Transaction lifecycle state; inspect terminal error/success, not only HTTP acceptance.',stage:'Current transaction phase; progress_percent belongs to this phase.',programming_method:'Actual programming transport: none, urclock or usbasp; never infer it from error text.',bootloader_outcome:'Outcome of the UART bootloader attempt. not_attempted is normal for ISP/host updates.',isp_fallback_suggested:'Whether failure evidence suggests an explicitly selected ISP recovery; no automatic ISP flash is authorized.',error_code:'Typed failure classification, when available; detail contains human-readable context.',toolchain:'Selected programming provider readiness evidence captured before measured transfer progress.'};
const menuFields={id:'Stable host menu identifier, used for directory references rather than a caption.',label:'Compact front-panel menu caption.',title:'Display heading for this menu.',content:'Host-rendered display content for this page.',brightness:'Front-panel display brightness for this presentation.',edit_visual:'Configured editing presentation style for this menu.',flags:'Menu visibility, selection and editing affordances.',items:'Ordered items in this host-presented menu.',node_id:'Wire node identity for the host menu tree.',parent_id:'Parent menu node identity; preserve the directory hierarchy.',order_id:'Order within the parent menu; not a physical channel number.',stable_id:'Immutable firmware page ID whose online presentation is overridden.'};
const typeFields={
  'controller.TextMessage':{source:'Producer identity; the receiving host also tags inbound access provenance.',targets:'Message recipients; host is the normal local recipient.',type:'Application message type for event handlers; not a transport method.',text:'Primary message text.',line1:'Optional first LCD/text presentation line.',line2:'Optional second LCD/text presentation line.',action:'Descriptive action metadata only; never implicitly executed as a command.',severity:'Message severity for presentation/routing.',correlation:'Caller correlation identity preserved across message/event handling.',delivery:'Delivery metadata, separate from physical action confirmation.',metadata:'Bounded message metadata; credentials are filtered from inbound webhook provenance.'},
  'releaseplane.Candidate':candidateFields,'releaseplane.ManifestArtifact':candidateFields,
  'releaseplane.DiscoveryResult':{source:'Discovery provider used for this inventory (GitHub workflow/release or manifest).'},
  'releaseplane.GitHubWorkflowRequest':artifactFields,'releaseplane.GitHubReleaseRequest':artifactFields,
  'releaseplane.CheckRequest':{...artifactFields,current:'Caller-supplied identity of the actually installed/running target; not a selected candidate.'},
  'releaseplane.CheckResult':{status:'same: matching digest/build; newer/older: comparable build time; different: unequal or incomparable identity; unavailable: no compatible candidate.'},
  'releaseplane.StageStatus':{id:'Opaque staging job ID for /api/discovery/status/{id}.',kind:artifactFields.kind,state:'Staging lifecycle: queued, downloading, completed or failed. completed means imported, not installed.',progress_percent:'Staging progress: bounded download progress occupies up to 85%; completed reports 100 only after validation/import.'},
  'releaseplane.StageResult':{operation:'Current staging job snapshot; operation.id is the status lookup key.'},
  'artifacts.Descriptor':{...artifactFields,current:'Marked current by the artifact service; verified_readback separately establishes captured device bytes.'},
  'artifacts.FetchRequest':artifactFields,'artifacts.UploadRequest':{...artifactFields,data:'Base64-encoded artifact bytes for the JSON-RPC upload, not a multipart file or a local path.'},'artifacts.UpdateStatus':updateFields,
  'artifacts.OperationResult':{operation:'Current durable artifact/update job snapshot; operation.id is its lookup key.'},
  'artifacts.ToolchainReadiness':{ready:'Whether the selected provider passed readiness checks.',policy:'Configured provider selection policy; system is preferred unless explicitly selected otherwise.',provider:'Selected verified programming provider.',version:'Reported selected provider version.',providers:'Optional providers reported separately from the selected one.',compatible_sources:'Count of available compatible toolchain sources.'},
  'artifacts.ProgressStream':{transport:'Progress transport, normally the configured WebSocket.',url:'Configured progress stream endpoint; resolve host-relative paths against this listener.'},
  'artifacts.CaptureRequest':{method:'Explicit programming transport selection, such as urclock or usbasp; omission uses updater policy.',port:'Serial/programmer port selection for this operation; not an HTTP listener port.'},
  'artifacts.UpdateRequest':{method:'Explicit programming transport selection, such as urclock or usbasp; omission uses updater policy.',port:'Serial/programmer port selection for this operation.'},
  'artifacts.Manifest':{enabled:'Whether the artifact/update service is enabled.',defaults_enabled:'Whether bundled recovery defaults are available.',defaults:'Bundled firmware/EEPROM candidates; never proof of current device contents.',current:'Tracked current artifacts, including a separately verified flash readback.',board:'Current board connection/build identity for comparison.',comparison:'Comparison of default candidate and observed board build identity.',policy:'Explicit consent and remote-programming admission requirements.',update:'Latest/current update transaction snapshot, when retained.'},
  'artifacts.CurrentArtifacts':{firmware:'Tracked programmed firmware candidate, not a fresh flash backup.',eeprom:'Tracked EEPROM artifact.',flash_readback:'Verified bytes explicitly captured from board flash.',host:'Tracked host executable artifact.'},
  'artifacts.DefaultArtifacts':{firmware:'Bundled firmware candidate used for explicit recovery/first install.',eeprom:'Bundled EEPROM candidate; applying it can replace persistent board settings.'},
  'artifacts.BoardIdentity':{connected:'Whether the host currently has a connected board identity.'},
  'artifacts.Comparison':{default_firmware:'Comparison classification for bundled firmware versus the observed board.',board_build_hash:'Observed board build/source hash.',candidate_hash:'Selected/default candidate build identity.',board_timestamp:'Observed board build time.',candidate_timestamp:'Candidate build time.'},
  'artifacts.Policy':{explicit_authorization_required:'Whether update/readback requests must explicitly authorize programming ownership.',remote_programming_enabled:'Whether remote callers may initiate programming under this host policy.'},
  'ipcjson.pwmValues':{available:'Whether authoritative PWM values are available in this observation.',selected_channel:'Current firmware PWM-selection index, not a filter on the returned values.',values:'Sixteen raw 12-bit register values in physical channel-index order.',channels:'Per-channel calibrated values, names and remembered-on state.',revision:'Observation revision within this host session; retain with host and connection generation.',time:'Original authoritative observation time; not a new measurement invented by the HTTP read.'},
  'ipcjson.pwmChannelValue':{channel:'Physical PWM index 0–15.',name:'Configured channel caption, not its physical address.',config:'Persisted conversion and On/step behavior for this channel.',logical_percent:'Observed raw value converted through the configured channel curve.',remembered_known:'Whether a remembered nonzero On setpoint is known.',remembered_raw:'Remembered nonzero raw register setpoint.',remembered_percent:'Remembered setpoint converted to calibrated percentage.',remembered_persisted:'Whether the remembered On value has been durably persisted.'},
  'appconfig.PWMChannelConfig':{output_type:'Configured load/output presentation type; does not rewire the board.',curve:'Raw-to-logical conversion curve.',gamma:'Gamma exponent used by the selected nonlinear curve.',icon:'Semantic icon identifier for the channel.',step_percent:'Logical percentage step for increase/decrease actions.',on_behavior:'full requests full output; restore uses a remembered nonzero brightness where available.'},
  'appconfig.HostMenuConfig':{default_menu:'Host-menu ID selected when a host session opens.',request_gesture:'Configured front-panel gesture that requests the host menu.',display_duration_ms:'Host display presentation lifetime in milliseconds.',session_timeout_ms:'Idle host-menu session timeout in milliseconds.',builtin_overrides:'Online presentation overrides for immutable firmware page IDs.',menus:'Host-owned page directory supplied during an online session.'},
  'appconfig.HostMenu':menuFields,'appconfig.BuiltinMenuOverride':menuFields,
  'appconfig.HostMenuFlags':{visible:'Whether the page appears in this menu.',selectable:'Whether the operator may select this page.',editable:'Whether the presentation advertises editing.',read_only:'Whether edits are prohibited for this entry.'},
  'hostos.VirtualKeyRequest':{key:'Validated host key identifier from the supported OS key vocabulary; not a board button index.',hold_ms:'Optional key-hold duration in milliseconds, subject to host bounds.'},
  'hostos.PowerRequest':{action:'Supported system/display action name under the current OS policy.',confirmation:'Required confirmation value under the host power policy; not a transport credential.',automation:'Ignored as authority on the REST route, which forces false; automation policy is enforced separately.'},
  'hostos.ActionResult':{action:'Accepted host OS action identifier.',at:'Host timestamp of the action result.'},
  'control.DisplayRequest':{target:'Physical display target: segments, lcd or both (seg/segment are aliases).',text:'Text to present on the selected board display.',speed_ms:'Scroll-step interval, 80–5000 ms; zero/omission selects 220 ms.',duration_ms:'Presentation duration, 80–65535 ms; zero/omission selects 5000 ms.',repeat:'Repeat mode: once, loop or interval.',interval_ms:'Pause interval for interval repeat mode, in milliseconds.',scroll:'Force seven-segment scrolling even when the text fits four cells.'},
  'control.DisplayResult':{target:'Normalized physical display target.',text:'Normalized text accepted for presentation.',scrolling:'Whether this presentation scrolls.',speed_ms:'Effective scroll-step interval in milliseconds.',duration_ms:'Effective presentation lifetime in milliseconds.',repeat:'Effective repeat mode.',interval_ms:'Effective repeat pause in milliseconds.',segment_cells:'Number of segment display cells in the normalized presentation.',lcd_cells:'Number of LCD cells in the normalized presentation.'},
  'ipcjson.webhookSelectorParams':{delivery_id:'One exact retained dead-letter ID; mutually exclusive with all.',all:'Select every retained dead letter; requires confirm_all=true and no delivery_id.',confirm_all:'Explicit confirmation for the bulk all selection. Does not confirm successful delivery.'},
  'ipcjson.WebhookReplayResult':{replayed:'Number of selected dead letters requeued, not successfully delivered.',status:'Queue health after requeueing.'},
  'ipcjson.WebhookClearResult':{cleared:'Number of selected dead-letter records removed.',status:'Queue health after deletion.'},
  'ipcjson.WebhookDeliveryList':{deliveries:'Bounded non-secret retained delivery records.',status:'Queue counters captured with this list.'},
  'ipcjson.WebhookDeliveryView':{id:'Retained delivery ID used for explicit replay/deletion.',target:'Configured destination name; its URL and credentials are not exposed.',event_id:'Host event-history identity that produced this delivery.',event_kind:'Source event kind.',attempts:'Attempts already made.',max_attempts:'Configured retry budget.',next_attempt_at:'Scheduled retry time, when pending.',last_attempt_at:'Most recent attempt time.',last_attempt_id:'Identity of the most recent attempt.',last_status:'Most recent receiver HTTP status, when one was received.',last_error:'Most recent delivery failure, not proof that the receiver performed no action.',correlation_id:'Correlation identity of the originating event/message.'},
  'ipcjson.WebhookQueueStatus':{pending:'Retained deliveries awaiting work/retry.',dead:'Current dead-letter count.',completed_dedupe_records:'Retained completion identities used to suppress duplicates.',enqueued:'Cumulative accepted queue entries.',delivered:'Cumulative successful HTTP deliveries, not receiver-side physical acknowledgements.',retried:'Cumulative retry attempts.',dead_lettered:'Cumulative deliveries moved to dead letters.',dropped:'Cumulative deliveries dropped by queue policy.',duplicates:'Cumulative duplicate delivery identities suppressed.',dead_discarded:'Cumulative discarded dead-letter records.',next_attempt_at:'Earliest pending scheduled attempt.',in_flight:'Deliveries currently being attempted.',closing:'Whether the queue is shutting down.'},
  'control.EmergencyStopState':{active:'True inhibits effect/motion/output admission and requests cancellation/off; false only releases admission.',revision:'Interlock state revision.',source:'Caller that requested the latest transition.',changed_at:'Host timestamp of the latest transition.'},
  'control.ProgramStateSnapshot':{mode:'Application-level Idle or Running, composed from current ownership claims.',owners:'Applications holding durable/transient runtime ownership claims.',revision:'Program-ownership state revision.',changed_at:'Host timestamp of the latest ownership-state change.'},
  'control.ProgramStateOwner':{name:'Application owner identity, not the host process ID.',references:'Number of active claims held by this owner.',persistent:'Whether this owner has a durable claim beyond transient activity.'},
  'control.MenuLayout':{schema:'Firmware layout data format identifier.',supported:'Whether persistent layout is advertised by the connected firmware.',persistent:'Whether this layout was read from persistent board storage.',source:'Origin of this layout; a catalog fallback is not persisted board state.',order:'Base64-encoded byte sequence containing a complete permutation of stable page IDs (Go []byte JSON encoding).'},
  'control.MenuCatalog':{source:'Catalog source, distinguishing live firmware data from fallback.',live_list:'Whether pages were obtained from the board live-list capability.',firmware_hash:'Firmware build hash associated with the catalog.',current_page:'Currently selected physical front-panel page ID.',program_mode:'Firmware program-mode value.',pages:'Advertised pages with stable IDs and descriptions.',layout:'Current visibility/order layout and its persistence support.'},
  'control.MenuPageInfo':{id:'Stable firmware page ID; not its current visible row number.',key:'Semantic page key.',label:'Compact board display caption.',name:'Human-readable page name.',description:'Purpose of this firmware page.'},
};

export function describeRoute(path, method) {
  const meaning = routeMeaning(path, method);
  return { ...meaning, text:`${meaning.description}\n\n**Use case:** ${meaning.useCase}${meaning.rpc ? `\n\n**Typed alternative:** [${meaning.rpc}](rpc.html#${meaning.rpc}).` : ''}` };
}

// Request/response type names are package-qualified to avoid confusing the
// release manifest with the artifact-store manifest (both are named Manifest).
export const httpTypes = {
  '/api/discovery/github/workflow':['releaseplane.GitHubWorkflowRequest','releaseplane.DiscoveryResult'],
  '/api/discovery/github/release':['releaseplane.GitHubReleaseRequest','releaseplane.DiscoveryResult'],
  '/api/discovery/manifest':['releaseplane.ManifestRequest','releaseplane.DiscoveryResult'],
  '/api/discovery/check':['releaseplane.CheckRequest','releaseplane.CheckResult'],
  '/api/discovery/stage':['releaseplane.StageRequest','releaseplane.StageResult'],
  '/api/discovery/status/{id}':[null,'releaseplane.StageStatus'],
  '/api/artifacts/manifest':[null,'artifacts.Manifest'],
  '/api/artifacts':[null,'artifacts.List'],
  '/api/artifacts/fetch':['artifacts.FetchRequest','artifacts.OperationResult'],
  '/api/artifacts/capture':['artifacts.CaptureRequest','artifacts.OperationResult'],
  '/api/artifacts/upload':[null,'artifacts.OperationResult'],
  '/api/board/firmware/upload':['artifacts.UpdateRequest','artifacts.OperationResult'],
  '/api/board/firmware/download':['artifacts.CaptureRequest','artifacts.OperationResult'],
  '/api/updates/{kind}':['artifacts.UpdateRequest','artifacts.OperationResult'],
  '/api/restores/flash':['artifacts.UpdateRequest','artifacts.OperationResult'],
  '/api/updates/status/{id}':[null,'artifacts.UpdateStatus'],
  '/api/pwm':[null,'ipcjson.pwmValues'],
  '/api/peripherals':[null,'ipcjson.PeripheralSettings'],
  '/api/estop':[null,'control.EmergencyStopState'],
  '/api/program-state':[null,'control.ProgramStateSnapshot'],
  '/api/menu/catalog':[null,'control.MenuCatalog'],
  '/api/menu/layout':[null,'control.MenuLayout'],
  '/api/snapshot':[null,'ipcjson.controllerSnapshotEnvelope'],
  '/api/host-menus':['appconfig.HostMenuConfig','appconfig.HostMenuConfig'],
  '/api/os/key':['hostos.VirtualKeyRequest','hostos.ActionResult'],
  '/api/os/power':['hostos.PowerRequest','hostos.ActionResult'],
  '/api/display':['control.DisplayRequest','control.DisplayResult'],
  '/api/messages':['controller.TextMessage','controller.Event'],
  '/api/webhooks/outbound/status':[null,'ipcjson.WebhookQueueStatus'],
  '/api/webhooks/outbound/pending':[null,'ipcjson.WebhookDeliveryList'],
  '/api/webhooks/outbound/dead':[null,'ipcjson.WebhookDeliveryList'],
  '/api/webhooks/outbound/replay':['ipcjson.webhookSelectorParams','ipcjson.WebhookReplayResult'],
  '/api/webhooks/outbound/clear':['ipcjson.webhookSelectorParams','ipcjson.WebhookClearResult'],
};

// Explicit RPC bindings: do not reuse the raw/multipart HTTP upload contract
// for controller.artifact.upload, whose Go RPC request carries base64 data.
const rpcTypes={
  'controller.discovery.github.workflow':httpTypes['/api/discovery/github/workflow'],
  'controller.discovery.github.release':httpTypes['/api/discovery/github/release'],
  'controller.discovery.manifest':httpTypes['/api/discovery/manifest'],
  'controller.discovery.local_manifest':[null,'releaseplane.Manifest'],
  'controller.discovery.check':httpTypes['/api/discovery/check'],
  'controller.discovery.stage':httpTypes['/api/discovery/stage'],
  'controller.discovery.status':[null,'releaseplane.StageStatus'],
  'controller.artifact.manifest':httpTypes['/api/artifacts/manifest'],
  'controller.artifact.list':httpTypes['/api/artifacts'],
  'controller.artifact.upload':['artifacts.UploadRequest','artifacts.OperationResult'],
  'controller.artifact.fetch':httpTypes['/api/artifacts/fetch'],
  'controller.artifact.capture':httpTypes['/api/artifacts/capture'],
  'controller.board.firmware.upload':httpTypes['/api/board/firmware/upload'],
  'controller.board.firmware.download':httpTypes['/api/board/firmware/download'],
  'controller.update.firmware':httpTypes['/api/board/firmware/upload'],
  'controller.update.eeprom':httpTypes['/api/board/firmware/upload'],
  'controller.update.host':httpTypes['/api/board/firmware/upload'],
  'controller.restore.flash':httpTypes['/api/restores/flash'],
  'controller.update.status':httpTypes['/api/updates/status/{id}'],
  'controller.display.send':httpTypes['/api/display'],
  'controller.message.send':httpTypes['/api/messages'],
  'controller.os.key':httpTypes['/api/os/key'],
  'controller.os.power':httpTypes['/api/os/power'],
};
export function rpcWireDocumentation(name,shapes) {
  if (name === 'controller.front_panel.key') return {
    request_fields: {
      type:'object',required:['key','hold_ms'],additionalProperties:false,
      properties:{
        key:{type:'integer',minimum:1,maximum:4,description:'Previous, Next, Decrease or Increase, in order.'},
        hold_ms:{oneOf:[{const:0},{type:'integer',minimum:100,maximum:2000}],description:'Lease duration from firmware receipt. Zero explicitly releases the key.'},
        expected_connection_generation:{type:'integer',minimum:1,description:'Optional generation from the selected board snapshot.'},
      },
    },
    request_template:{key:1,hold_ms:700},
    result_schema:{
      type:'object',required:['accepted','key','hold_ms','connection_generation'],
      properties:{
        accepted:{const:true,description:'Firmware acknowledged setting or releasing the leased input.'},
        key:{type:'integer',minimum:1,maximum:4},
        hold_ms:{type:'integer'},
        connection_generation:{type:'integer',minimum:1},
      },
    },
  };
  const names=rpcTypes[name];if(!names)return {};
  const read=key=>{if(!key)return undefined;const schema=shapes.$types[key];if(!schema||!Object.keys(schema).length)throw Error('Missing RPC Go type: '+key);return explainFields(structuredClone(schema));};
  const result={};
  if(names[0])result.request_fields=read(names[0]);
  if(names[1])result.result_schema=read(names[1]);
  const example=Object.entries(routeExamples).find(([key])=>{const [method,path]=key.split(' ');return routeMeaning(path,method).rpc===name;})?.[1];
  if(example)result.request_template=example;
  return result;
}

export const routeExamples = {
  'post /api/discovery/github/workflow':{repository:'atomicdeploy/PCController',branch:'main',kind:'host-executable',platform:'windows/amd64'},
  'post /api/discovery/github/release':{repository:'atomicdeploy/PCController',kind:'firmware'},
  'post /api/discovery/manifest':{url:'https://updates.example.org/pccontroller/manifest.json'},
  'post /api/board/firmware/upload':{artifact_sha256:'<sha256 from a verified staged firmware descriptor>',authorized:true,idempotency_key:'<one stable caller-generated job key>'},
  'post /api/board/firmware/download':{authorized:true,components:['flash'],idempotency_key:'<one stable caller-generated capture key>'},
  'post /api/rpc':{jsonrpc:'2.0',id:1,method:'controller.rf.catalog',params:{}},
  'put /api/pwm':{channel:0,percent:50},
  'post /api/osd':{title:'Export complete',text:'The verified backup is ready.',icon:'success',duration_ms:5000},
  'post /api/command':{command:'help'},
};

const query = (name, description, schema={type:'string'}, required=false) => ({in:'query',name,description,schema,required});
const binary = {type:'string',format:'binary'};
const jsonResponse = (description, schema) => ({description,content:{'application/json':{schema}}});
const fileRoutes = new Set(['/api/artifacts/{kind}/{sha256}','/api/artifacts/current/{kind}','/api/board/firmware/download']);
const asyncRoutes = new Set(['/api/artifacts/fetch','/api/artifacts/capture','/api/board/firmware/upload','/api/board/firmware/download','/api/updates/{kind}','/api/restores/flash','/api/discovery/stage','/api/os/key','/api/os/power','/api/messages']);
export function enrichOperation(operation, path, method, shapes) {
  const meaning = describeRoute(path, method);
  operation.summary = meaning.title;
  operation.description = meaning.text;
  // Keep established operation IDs for existing generated clients, but hide
  // these implementation labels in the human reference (Scalar config).
  operation['x-use-case'] = meaning.useCase;
  const body = !['get','head','delete'].includes(method);
  const names = httpTypes[path];
  const named = name => {
    const schema = shapes.$types[name];
    if (!schema || !Object.keys(schema).length) throw new Error(`Unresolved documented Go type: ${name}`);
    return explainFields(structuredClone(schema));
  };
  if (names?.[0] && body) operation.requestBody = {required:true,content:{'application/json':{schema:named(names[0])}}};
  else if (body && shapes.$http[path] && !operation.requestBody?.content?.['application/json']?.schema?.$ref) operation.requestBody.content['application/json'].schema = explainFields(structuredClone(shapes.$http[path]));
  if (names?.[1] && !(fileRoutes.has(path) && !body)) operation.responses['200'] = jsonResponse(meaning.result,named(names[1]));
  if (operation.responses['200']) operation.responses['200'].description = meaning.result;
  const success = asyncRoutes.has(path) && body ? '202' : path==='/api/artifacts/upload' ? '201' : null;
  if (success && operation.responses['200']) {
    operation.responses[success] = operation.responses['200'];
    delete operation.responses['200'];
  }
  if (path==='/api/discovery/manifest' && method==='get') operation.responses['200']=jsonResponse(meaning.result,named('releaseplane.Manifest'));
  if (path.startsWith('/api/diagnostics/')) {
    delete operation.requestBody;
    operation.responses['200'].content={'application/octet-stream':{schema:binary}};
  }
  if (path==='/api/rpc') {
    const envelope={type:'object',required:['jsonrpc','id'],properties:{jsonrpc:{const:'2.0',description:'JSON-RPC protocol marker.'},id:{$ref:'#/components/schemas/JSONRPCID',description:'Echo of the request ID; correlate independently of notification arrival order.'},result:{description:'Method-specific successful return value; see that method’s result contract.'},error:{$ref:'#/components/schemas/JSONRPCError',description:'Method/dispatch failure; may include typed outcome data.'}},oneOf:[{required:['result'],not:{required:['error']}},{required:['error'],not:{required:['result']}}]};
    operation.responses['200']=jsonResponse('Successful JSON-RPC result envelope. The method defines whether this means cached data, accepted work or confirmed readback.',envelope);
    operation.responses['400']=jsonResponse('JSON-RPC parse/dispatch/method error envelope; inspect error.code, message and optional data. A mutation’s outcome may still be unknown.',envelope);
    operation.responses['415']={description:'Content-Type is not application/json.'};
  }
  if (path==='/api/webhooks/inbound') {
    operation.summary=method==='options'?'Read inbound webhook methods':'Ingest an integration webhook';
    operation.description+='\n\n**Compatibility detail:** GET, POST, PUT, PATCH and DELETE all ingest a message; GET here is not a safe health probe. Query text/source/type/action/line1/line2 override body values; repeated targets selects recipients. Plain text is also accepted. OPTIONS only reports allowed methods.';
    operation['x-idempotency']=method==='options'?'safe':'non-idempotent';
    operation.parameters=['source','type','text','action','line1','line2','targets'].map(name=>query(name,name==='targets'?'Repeat this parameter to select message recipients; defaults to host.':`Override the message ${name} from the body.`));
    delete operation.requestBody;
    if (['post','put','patch','delete'].includes(method)) operation.requestBody={required:false,description:'Optional typed message or plain text, overridden by named query fields.',content:{'application/json':{schema:named('controller.TextMessage')},'text/plain':{schema:{type:'string'}}}};
    delete operation.responses['200'];
    operation.responses[method==='options'?'204':'202']=method==='options'?{description:'Allowed methods only, with no message ingested or response body.'}:jsonResponse('Accepted message event; downstream physical/UI action is not implied.',named('controller.Event'));
    operation.responses['404']={description:'Inbound webhooks are disabled.'};
    operation.responses['413']={description:'The request body exceeds the bounded message limit.'};
    if(method==='options') operation.description='Read the HTTP methods supported by the optional inbound webhook handler without ingesting a message.\n\n**Response meaning:** 204 has no body; Allow lists accepted methods. Disabled webhooks return 404.\n\n**Use case:** Check integration compatibility without producing an application event.';
  }
  if (path==='/healthz') operation.responses['200']=jsonResponse(meaning.result,{type:'object',properties:{ok:{const:true,description:'This listener handled the liveness request.'},status:{const:'ok',description:'HTTP service liveness, not board connection health.'},product:{const:'pccontroller',description:'Product identity of the responding listener.'},service:{type:'string',description:'Configured human-readable service identity.'}}});
  if (fileRoutes.has(path) && !body) {
    delete operation.requestBody;
    const headers={
      ETag:{description:'Quoted sha256:<digest> validator for the exact bytes.',schema:{type:'string'}},
      'X-Checksum-SHA256':{description:fieldMeaning.sha256,schema:{type:'string'}},
      'X-Artifact-Kind':{description:'Stored artifact kind (firmware, eeprom, flash-backup or host-executable).',schema:{type:'string'}},
      'Content-Disposition':{description:'Attachment filename from the validated descriptor.',schema:{type:'string'}},
    };
    operation.responses['200']={description:meaning.result,headers, ...(method!=='head'?{content:{'application/octet-stream':{schema:binary}}}:{})};
    operation.responses['206']={...operation.responses['200'],description:'Requested byte range; Content-Range identifies the returned interval.'};
    operation.responses['304']={description:'The supplied cache validator still matches; no response body.'};
    operation.responses['404']={description:'No verified stored artifact/readback matches this selection.'};
    operation.responses['416']={description:'The requested byte range cannot be satisfied.'};
    operation.parameters??=[];
    operation.parameters.push({in:'header',name:'Range',description:'Optional HTTP byte range, for example bytes=0-4095.',schema:{type:'string'}},{in:'header',name:'If-None-Match',description:'Optional ETag from an earlier download; a match yields 304 without bytes.',schema:{type:'string'}});
  }
  if (method==='delete' && path==='/api/osd') operation.requestBody={required:false,description:'Omit id to dismiss the current presentation; supply id to fence a specific overlay.',content:{'application/json':{schema:{$ref:'#/components/schemas/OSDDismiss'}}}};
  if (method==='get' && path==='/api/pwm/channel') operation['x-required-capability']='read';
  if (path==='/api/pwm' && method==='put') {
    const schema=operation.requestBody.content['application/json'].schema;
    schema.required=['channel'];schema.additionalProperties=false;
    Object.assign(schema.properties.channel,{minimum:0,maximum:15});
    Object.assign(schema.properties.percent,{minimum:0,maximum:100});
    Object.assign(schema.properties.raw_value,{minimum:0,maximum:4095});
    schema.oneOf=[{required:['percent'],not:{required:['raw_value']}},{required:['raw_value'],not:{required:['percent']}}];
  }
  if (path==='/api/artifacts/upload') {
    operation.requestBody={required:true,description:'Raw artifact bytes, or multipart/form-data with a file part. Metadata goes in the query, not JSON.',content:{'application/octet-stream':{schema:binary},'multipart/form-data':{schema:{type:'object',required:['file'],properties:{file:binary}}}}};
    operation.parameters=[query('kind','Validation/storage kind of the uploaded bytes.',{enum:['firmware','eeprom','flash-backup','host-executable']},true),...['name','sha256','bytes','build_hash','build_timestamp','packed_timestamp','platform'].map(name=>query(name,fieldMeaning[name]??(name==='name'?'Display/download filename; multipart filename is used when omitted.':'Expected artifact byte length; zero/omission leaves it unspecified.'))),{in:'header',name:'X-Checksum-SHA256',description:'Optional expected digest; the sha256 query parameter takes precedence.',schema:{type:'string'}}];
  }
  if (path==='/api/artifacts' && method==='get') operation.parameters=[query('kind','Optional artifact-kind filter.',{enum:['firmware','eeprom','flash-backup','host-executable']})];
  if (path==='/api/os/facts') operation.parameters=[query('profile','Allowlisted host-facts profile; list or catalog returns available profiles. The omitted profile uses the host service default.')];
  if (path==='/api/docs/{asset}') operation.parameters=[{in:'path',name:'asset',required:true,description:'Embedded relative asset, for example reference.html, rpc.html, openapi.json or asyncapi.json; not a local filesystem path.',schema:{type:'string',minLength:1}}];
  if (/\/outbound\/(pending|dead)$/u.test(path)) operation.parameters=[query('limit','Maximum retained delivery records to return; omission uses the service default.',{type:'integer',minimum:1,maximum:100})];
  if (path.startsWith('/api/integrations/device/')) {
    delete operation.requestBody;
    delete operation.responses['200'];
    operation.responses['404']={description:'Generic device routing is disabled or the integration target is absent; use the typed controller.device.* RPC methods.'};
  }
  for (const parameter of operation.parameters??[]) {
    if (parameter.description) continue;
    const descriptions={asset:'Embedded relative asset, for example reference.html, rpc.html, openapi.json or asyncapi.json.',kind:path==='/api/updates/{kind}'?'Update target: firmware, eeprom or host.':'Readback kind: flash or eeprom. For immutable downloads use firmware, eeprom, flash-backup or host-executable.',sha256:fieldMeaning.sha256,id:path.includes('/status/')?'Opaque job ID returned by the initiating operation.':'Exact live application-instance ID; this is not a PID.',path:'Path within the configured confined integration namespace, not a URL or filesystem path.'};
    parameter.description=descriptions[parameter.name]??fieldMeaning[parameter.name];
    if (!parameter.description) throw new Error(`Undocumented parameter ${path} ${parameter.name}`);
  }
  const kindParameter=operation.parameters?.find(p=>p.in==='path'&&p.name==='kind');
  if(kindParameter) kindParameter.schema.enum=path==='/api/updates/{kind}'?['firmware','eeprom','host']:path==='/api/artifacts/current/{kind}'?['flash','eeprom']:['firmware','eeprom','flash-backup','host-executable'];
  const example=routeExamples[method+' '+path];
  if (example && operation.requestBody?.content['application/json']) operation.requestBody.content['application/json'].examples={workflow:{summary:'Example request (replace angle-bracket placeholders)',value:example}};
  // Avoid presenting empty JSON as a valid hardware request. Read operations
  // still get a copyable URL; complex actions link to their typed contract.
  if (['get','head'].includes(method) && !path.includes('{')) operation['x-codeSamples']=[{lang:'Shell',label:'Read from this host',source:`curl --request ${method.toUpperCase()} '<HOST_ORIGIN>${path}${path==='/api/os/facts'?'?profile=list':''}'`}];
  else if (example) operation['x-codeSamples']=[{lang:'Shell',label:'Request template',source:`curl --request ${method.toUpperCase()} '<HOST_ORIGIN>${path}' --header 'Content-Type: application/json' --data '${JSON.stringify(example)}'`}];
  explainFields(operation.requestBody?.content?.['application/json']?.schema,requestFieldMeaning[path]);
  return operation;
}

export function rpcMeaning(name, description) {
  if(name==='controller.artifact.upload') description='Import base64-encoded artifact data through the JSON-RPC request into the immutable store. This differs from the raw/multipart HTTP upload route; neither operation flashes the board. Retain the verified descriptor for a separately authorized update.';
  const matches=[];
  for (const [path,value] of Object.entries(entries)) if(value[4]===name) matches.push(describeRoute(path,'get'));
  for (const [key,value] of Object.entries(mutations)) if(value[4]===name) {
    const [method,path]=key.split(' '); matches.push(describeRoute(path,method));
  }
  const match=matches[0];
  // RPC names remain visible as the callable identifier. Their purpose is
  // separate from that identifier, and REST notes are reused only as workflow
  // context, not as a claim that the wire request/response shapes are equal.
  if(match) return {title:match.title,summary:match.title,description:/^(Read|Invoke) the .* (surface|operation)\.$/u.test(description)?match.description:description, use_case:match.useCase, result_description:match.result};
  const canonical=({'controller.front-panel':'controller.front_panel','controller.host_menu.config':'controller.host_menu.config.get','controller.virtual_key':'controller.os.key','controller.exit':'controller.quit','controller.peripherals':'controller.peripherals.get','controller.opcode.request':'controller.opcode.exchange'})[name]??name;
  if(canonical!==name) {
    const result=rpcMeaning(canonical,description);
    return {...result,description:result.description+` Alias: ${canonical}.`};
  }
  const extra=rpcDescriptions[name];
  if(extra) description=extra;
  if (name === 'controller.front_panel.key') return {
    title:'Hold or release a Virtual Board key', summary:'Hold or release a Virtual Board key', description,
    use_case:'Send a bounded hold on pointer-down and release it on pointer-up or loss of focus.',
    result_description:'The receipt reports the lease acknowledged by firmware. Observe controller.event or controller.front_panel for the resulting key state.',
  };
  if (name.startsWith('controller.mqtt.')) return {
    title:methodTitle(name), summary:methodTitle(name), description,
    use_case:name.endsWith('.set')?'Enable a unique broker topic in read-only mode before granting board command access.':'Confirm the configured broker session before relying on retained telemetry.',
    result_description:'config is the persisted MQTT configuration without a password. status reports build availability, enabled configuration, acknowledged broker connection and any connection error. Access is local only.',
  };
  if(/^(Read|Invoke) the .* (surface|operation)\.$/u.test(description)) throw new Error(`RPC method needs a purpose description: ${name}`);
  const title=methodTitle(name);
  return {title,summary:title,description};
}

function methodTitle(name) {
  const parts=name.replace(/^controller\./u,'').split(/[._-]+/u);
  const verbs={get:'Read',set:'Update',put:'Save',list:'List',configure:'Configure',update:'Update',remove:'Remove',clear:'Clear',start:'Start',stop:'Stop',cancel:'Cancel',open:'Open',close:'Close',reset:'Reset',build:'Build',play:'Play',begin:'Begin',chunk:'Transfer chunk for',finish:'Finish',abort:'Abort',capture:'Capture',fetch:'Fetch',download:'Download',upload:'Upload',inspect:'Inspect',status:'Inspect',apply:'Apply',resync:'Resynchronize',invoke:'Invoke',execute:'Execute',release:'Release',renew:'Renew',override:'Override',add:'Add',map:'Map',transmit:'Transmit',send:'Send',scan:'Discover',subscribe:'Subscribe to',unsubscribe:'Unsubscribe from'};
  const labels={ui:'UI',os:'OS',rf:'RF',pwm:'PWM',lcd:'LCD',udp:'UDP',mqtt:'MQTT',config:'configuration',estop:'emergency interlock',eeprom:'EEPROM',ack:'acknowledgement',opcodes:'opcodes'};
  const tail=parts.at(-1),verb=verbs[tail];
  if(verb)parts.pop();
  const object=parts.map(p=>labels[p]??p).join(' ');
  const standalone={connect:'Connect the controller',quit:'Shut down the host',reset:'Reset the controller',open:'Open the controller connection',close:'Close the controller connection'};
  return object?`${verb??'Read'} ${object}`:standalone[tail]??`${verb??'Read'} controller state`;
}

const rpcDescriptions = {
  'controller.app.instance.get':'Read one currently leased UI surface by exact instance ID. Use controller.app.instances for discovery; an expired registry entry is not a live action target.',
  'controller.artifact.download.close':'Release a pinned peer artifact download handle. Closing this handle does not delete the immutable artifact or cancel a board operation.',
  'controller.artifact.upload.abort':'Abort one incomplete peer upload and remove its partial transfer state. Already-published immutable artifacts are not deleted by this operation.',
  'controller.artifact.upload.begin':'Reserve one bounded peer upload using its expected kind, digest and byte count. Retain transfer_id and the advertised chunk bound; no firmware is applied.',
  'controller.artifact.upload.finish':'Validate the completed ordered peer transfer and publish its immutable artifact descriptor. Transfer completion is not permission to install it.',
  'controller.board_automation.list':'Read one generation-consistent snapshot of firmware-owned automation rules. These execute on the board, unlike host event/RF rules returned by controller.automations.get.',
  'controller.board_automation.remove':'Remove one board-owned rule and verify its absence by readback. Removing a rule does not assert an electrical state for the output it previously controlled.',
  'controller.close':'Close the current owned serial connection without terminating the host or deleting its configuration. Existing board-session pins become obsolete.',
  'controller.open':'Open the selected serial controller connection and follow handshake/session state. Port availability alone does not establish board identity.',
  'controller.port.open':'Open the selected serial port through the controller connection lifecycle. Observe the handshake before enabling board-dependent controls.',
  'controller.port.close':'Close the current controller serial session without shutting down the host HTTP/RPC service. Do not reuse old connection-generation pins afterward.',
  'controller.port.reset':'Reset the owned serial board through the configured reset path. This can interrupt execution and must not be used as a read-only connection probe.',
  'controller.reset':'Request the controller reset lifecycle through the owned connection. A reset acknowledgement is not proof that firmware has completed its next handshake.',
  'controller.discovery.config.set':'Persist and hot-apply network advertisement configuration. This changes how the host is advertised, not software candidate discovery or board output authority.',
  'controller.host_menu.config.set':'Replace persistent host-presented menu configuration. These pages are rendered by the online host; they do not overwrite immutable firmware page identities.',
  'controller.hotkeys.get':'Read configured host keyboard shortcuts and their current policy. Reading does not register a new shortcut or send a key to another application.',
  'controller.hotkeys.set':'Update host-owned shortcut configuration under its revision/policy contract. A shortcut command remains subject to the same semantic action and safety checks as direct invocation.',
  'controller.integrations.local.get':'Read configured local integration settings without probing arbitrary user-supplied URLs or actuating a device.',
  'controller.integrations.local.set':'Persist configured local integration settings and apply them through the host adapter. This does not grant an arbitrary generic device proxy or bypass device policy.',
  'controller.lcd.presentation.configure':'Change host LCD presentation/arbitration configuration. Presentation policy determines which content wins; it does not change the physical display wiring.',
  'controller.mirror.configure':'Persist mirror behavior through the host’s typed mirror configuration. Configuration and an admitted live mirrored update are separate operations.',
  'controller.os.configure':'Change host OS integration policy. Enabling a policy does not itself send keys, request shutdown or run a board command.',
  'controller.program-state.get':'Read application-level Idle/Running ownership state. This compatibility alias reports runtime ownership, not firmware flashing progress.',
  'controller.program-state.set':'Change the requesting application’s Idle/Running ownership claim. Conflicting ownership can reject the transition; this does not start media playback.',
  'controller.rf.binding.remove':'Remove one named host-owned RF application binding. Learned waveform records and unrelated assignments are preserved.',
  'controller.rf.clear':'Clear the board’s learned RF table through the explicit board operation. This removes remote waveform records; inspect existing assignments before this destructive change.',
  'controller.rf.learn.cancel':'Cancel the active RF learning session without manufacturing a confirmed learned record. Existing learned records and assignments must be retained.',
  'controller.rf.learn.start':'Enter RF capture/learning according to the persisted learning preferences. Timed learning is optional; receiving an unlearned waveform does not automatically grant an output action.',
  'controller.rf.list':'Read learned RF waveform records from the controller. Use controller.rf.catalog for the combined reception, bindings and advertised assignment choices.',
  'controller.rf.remove':'Remove one learned RF waveform record through the board interface. This is distinct from removing a named host-only binding.',
  'controller.rf.transmit':'Transmit one validated RF waveform using its protocol/pulse/bit identity. This can affect external receivers and is not a read-only remote reception test.',
  'controller.status_led.set':'Apply an explicit status LED override under the current board policy. Use controller.status_led.release to return presentation to the reactive policy.',
  'controller.udp.config.get':'Read persisted binary UDP transport settings without starting discovery or broadcasting commands. Effective runtime health is available from controller.udp.status.',
  'controller.udp.config.set':'Persist binary UDP transport settings through the host configuration path. Advertisement/subscription settings do not grant remote output authority.',
  'controller.mqtt.config.get':'Read the local host’s saved outbound MQTT broker, topic and credential reference together with effective connection status. The password is never returned. This local-only call does not start or stop the transport.',
  'controller.mqtt.config.set':'Replace the local host’s MQTT configuration and request a hot reload. A successful reply confirms persistence, not broker login; check controller.mqtt.status. Command authority is off unless allow_commands is true.',
  'controller.mqtt.status':'Check whether this build supports MQTT, whether the integration is enabled, and whether the broker acknowledged its session. The result includes the saved configuration and the latest connection error, without secrets.',
  'controller.buzzer.tone':'Send one bounded tone to the board buzzer, replacing any streamed melody. Specify frequency_hz (20–20000) and duration_ms (1–5000); the receipt acknowledges dispatch, not audible output.',
  'controller.udp.events.get':'Read bounded retained UDP transport event receipts for diagnostics. Historical receipts must not be replayed as current board commands.',
  'controller.unsubscribe':'Remove this WebSocket connection’s active subscriptions. This affects future notifications only; it does not disconnect the board, stop outputs or cancel already-running jobs.',
  'controller.app.page':'Navigate application surfaces using the compatibility page operation. Use controller.app.navigate for explicit surface/instance targeting.',
  'controller.connect':'Connect the host to a selected serial controller using its connection policy. A successful port open is distinct from a completed board handshake; observe the resulting snapshot.',
  'controller.device.action':'Run one typed action on a configured integration device under its advertised capability and policy. This is not a generic HTTP proxy or arbitrary OS command.',
  'controller.device.inspect':'Inspect a configured device’s advertised typed facts and actions without actuating it. Use the returned capability vocabulary when composing an action.',
  'controller.device.status':'Read configured integration device state without sending an action. A missing/unavailable fact must not be substituted with zero.',
  'controller.discovery.scan':'Scan for LAN application peers using configured discovery mechanisms. Advertisements identify candidates only; verify the endpoint before selecting it and never acquire board authority just to discover it.',
  'controller.event.latest':'Read the latest retained controller activity event without waiting for a new one. An event ID belongs to this host history; it is not a board protocol sequence.',
  'controller.front_panel':'Read the physical front-panel presentation and button/display state. This is board-facing state, not the desktop GUI layout.',
  'controller.front_panel.key':'Set, renew, or release a bounded Virtual Board key hold. Use key 1–4 and hold_ms 100–2000, or 0 to release. Optionally fence the request with expected_connection_generation from the snapshot. Firmware clears the input when the lease expires or its transport disconnects.',
  'controller.history.status':'Read recording/history availability and retention state. This does not start recording or replay any historical command.',
  'controller.history.timeline':'Read a bounded retained event timeline for inspection. Historical events are evidence, not commands to repeat on current hardware.',
  'controller.host_menu.content.push':'Publish content for a host-presented front-panel page. It updates display content rather than adding a firmware menu page or changing wiring.',
  'controller.host_menu.directory.replace':'Replace the host-presented menu directory used during an online board session. Retain stable page identities; this is separate from the firmware menu layout.',
  'controller.host_menu.state':'Read current host-presented menu/session state so clients can distinguish the configured directory from the page actually being presented.',
  'controller.lcd.presentation.status':'Read the effective LCD presentation source and priority state without writing display bytes. Use this to diagnose which presentation currently owns the LCD.',
  'controller.lcd.priority':'Request a priority LCD presentation under the display arbitration rules. Priority applies to presentation ownership, not electrical output authority.',
  'controller.lcd.prompt':'Present a structured prompt on the character LCD. Prompt acceptance does not mean the operator has answered it; observe the related input/state events.',
  'controller.menu.current':'Read the board’s current menu page identity. It may differ from a host application’s selected UI page.',
  'controller.menu.jump':'Select a firmware-advertised front-panel menu page. Resolve stable IDs from controller.menu.list rather than assuming a visible row number is the wire ID.',
  'controller.menu.page':'Request a board menu page through the page-selection compatibility operation. Use advertised page IDs and inspect current menu state afterward.',
  'controller.mirror.apply':'Apply a typed mirrored state update to this receiver using its mirror session/sequence admission. Mirroring is distinct from a local operator command and must retain its ownership fences.',
  'controller.mirror.inspect':'Inspect configured mirror capabilities and admission details without sending a mirrored update. Use this before choosing what a peer may mirror.',
  'controller.mirror.resync':'Request mirror resynchronization after missing or stale state. This does not authorize replay of arbitrary past hardware actions.',
  'controller.mirror.status':'Read mirror session state and synchronization health. Requested mirroring and effective current state are separate facts.',
  'controller.os.policy':'Read the receiving host’s OS integration permission policy. Reading the policy does not enable virtual keys, power actions or automation.',
  'controller.port.owner':'Read which process currently owns the selected serial port. Ownership information does not grant permission to terminate that process.',
  'controller.port.process':'Inspect the selected port’s owning process information for connection diagnostics. This is not a process-control API.',
  'controller.ports':'List serial-port candidates visible to the receiving host. Enumeration is not proof of controller identity; verify the board handshake before using a port.',
  'controller.quit':'Request orderly shutdown of this host process, including its transport/UI lifecycle. The connection may close before the caller receives a final response; do not repeat output actions to test shutdown.',
  'controller.reset.lines':'Apply the requested serial control-line reset sequence to the owned port. Reset can interrupt board execution; it is not a connection-health read.',
  'controller.rf.learn.status':'Read RF learning/capture state without entering learning. RF means radio-frequency remote reception; observing an unlearned code does not assign or authorize an action.',
  'controller.rf.presentation':'Read RF records and their client presentation, including reception metadata where advertised. Received/unassigned and learned/assigned are distinct states.',
  'controller.session.snapshot':'Read the current controller session snapshot used to fence ownership and generation-sensitive operations. Do not reuse pins after reconnect or host replacement.',
  'controller.session.snapshot.last':'Read the retained last session snapshot for recovery/diagnostics. Retained state may describe a disconnected session and must not authorize a current write.',
  'controller.status':'Read controller status through the typed status operation. Inspect connection/availability fields before interpreting board data; unavailable is not zero.',
  'controller.status_led.release':'Release an explicit status-light override so the configured reactive policy can resume. This does not disable or delete the policy.',
  'controller.system.status':'Read receiving-host system status for diagnostics. This describes the PC/application environment, not PCB voltage or load current.',
  'controller.temperatures':'Read controller temperature observations with their availability. These are board peripheral readings, not host CPU/motherboard sensors.',
  'controller.udp.call':'Send a correlated typed request through the configured binary UDP peer transport. UDP is datagram-based; missing acknowledgement leaves mutation outcome uncertain and is not permission for a blind retry.',
  'controller.udp.status':'Read binary UDP transport configuration, peer/subscription health and observed state without broadcasting a hardware command.',
  'controller.udp.subscribe':'Create or update a UDP event subscription for the selected peer/topics. Subscription delivery does not grant output authority.',
  'controller.udp.unsubscribe':'Remove the selected UDP event subscription without disconnecting the board or changing outputs.',
  'controller.update.eeprom':'Queue an explicitly authorized update from a staged EEPROM artifact. EEPROM contains persistent board settings; inspect backup and compatibility policy before proceeding.',
  'controller.update.firmware':'Queue guarded programming from an already-staged firmware digest. Follow the returned operation to terminal verification; request acceptance is not proof of a running new image.',
  'controller.update.host':'Queue replacement of this host from a verified staged host executable under deployment policy. Retain the job/idempotency receipt across listener restart and verify the active identity.',
};
