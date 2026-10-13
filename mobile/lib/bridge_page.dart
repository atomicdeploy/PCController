import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';
import 'bridge.dart';
import 'icons.dart';
import 'models.dart';

class BridgePage extends StatefulWidget {
  const BridgePage({super.key, required this.api, required this.observed});
  final PakarApi api;
  final DeviceStatus? Function() observed;
  @override
  State<BridgePage> createState() => _BridgePageState();
}

class _BridgePageState extends State<BridgePage> with WidgetsBindingObserver {
  late final bridge = AndroidBridge(widget.api, widget.observed);
  final broker = TextEditingController(),
      topic = TextEditingController(text: 'pakar/bridge'),
      user = TextEditingController(),
      password = TextEditingController(),
      ws = TextEditingController(),
      token = TextEditingController(),
      interval = TextEditingController(text: '5');
  bool local = true,
      commands = false,
      firmware = false,
      working = false,
      foreground = true;
  int qos = 0;
  String message = '';
  static const vault = FlutterSecureStorage();
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    bridge.addListener(changed);
    restore();
  }

  void changed() {
    if (mounted) setState(() {});
  }

  Future<void> restore() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('pakar.bridge.options');
      final secret = await vault.read(key: 'pakar.bridge.password');
      final remoteSecret = await vault.read(key: 'pakar.bridge.token');
      if (!mounted || working || bridge.running) return;
      if (raw != null) {
        final v = jsonDecode(raw) as Map<String, dynamic>;
        broker.text = v['broker'] as String;
        topic.text = v['topic'] as String;
        user.text = v['user'] as String;
        ws.text = v['websocket'] as String;
        interval.text = '${v['seconds']}';
        qos = v['qos'] as int;
      }
      password.text = secret ?? '';
      token.text = remoteSecret ?? '';
      changed();
    } on Object {
      if (mounted) {
        setState(
          () => message = 'Bridge preferences unavailable; not overwritten.',
        );
      }
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    foreground = state == AppLifecycleState.resumed;
    if (state != AppLifecycleState.resumed) unawaited(bridge.stop());
  }

  Future<void> start() async {
    if (working) return;
    setState(() {
      working = true;
      message = '';
    });
    try {
      final options = BridgeOptions(
        broker: broker.text.trim(),
        topic: topic.text.trim(),
        user: user.text,
        password: password.text,
        remoteWebSocket: ws.text.trim(),
        remoteToken: token.text,
        local: local,
        commands: commands,
        firmwareStage: firmware,
        telemetrySeconds: int.tryParse(interval.text) ?? 0,
        qos: qos,
      );
      options.validate();
      await vault.write(key: 'pakar.bridge.password', value: password.text);
      await vault.write(key: 'pakar.bridge.token', value: token.text);
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(
        'pakar.bridge.options',
        jsonEncode({
          'broker': options.broker,
          'topic': options.topic,
          'user': options.user,
          'websocket': options.remoteWebSocket,
          'seconds': options.telemetrySeconds,
          'qos': options.qos,
        }),
      );
      if (!mounted || !foreground) return;
      await bridge.start(options);
    } on Object {
      if (mounted) {
        setState(
          () => message = 'Bridge could not start; check configuration, connectivity and peer identity. No operation replayed.',
        );
      }
    } finally {
      if (mounted) setState(() => working = false);
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    bridge.removeListener(changed);
    bridge.dispose();
    for (final c in [broker, topic, user, password, ws, token, interval]) {
      c.dispose();
    }
    super.dispose();
  }

  Widget field(TextEditingController c, String label, {bool secret = false}) =>
      Padding(
        padding: const EdgeInsets.only(bottom: 12),
        child: TextField(
          controller: c,
          obscureText: secret,
          enabled: !working && !bridge.running,
          decoration: InputDecoration(labelText: label),
        ),
      );
  Widget section(String title, List<Widget> children) => Card(
    child: Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(title, style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 16),
          ...children,
        ],
      ),
    ),
  );
  @override
  Widget build(BuildContext context) => ListView(
    padding: const EdgeInsets.all(16),
    children: [
      const Text(
        'Advanced · Android bridge',
        style: TextStyle(fontSize: 24, fontWeight: FontWeight.w700),
      ),
      const Padding(
        padding: EdgeInsets.symmetric(vertical: 12),
        child: Text(
          'Forward observed telemetry and explicitly allowed commands between this ESP, Android clients, MQTT and a remote WebSocket. Runs only while this page/app is foreground. Changing peers or losing peer freshness stops the bridge.',
        ),
      ),
      section('MQTT · Android client', [
        field(broker, 'Broker · mqtts://host:8883 or wss://host/mqtt'),
        field(topic, 'Exact base topic'),
        field(user, 'Broker username'),
        field(password, 'Broker password · secure storage', secret: true),
        DropdownButtonFormField<int>(
          initialValue: qos,
          decoration: const InputDecoration(labelText: 'Telemetry / reply QoS'),
          items: const [
            DropdownMenuItem(value: 0, child: Text('0 · at most once')),
            DropdownMenuItem(value: 1, child: Text('1 · at least once')),
          ],
          onChanged: working || bridge.running
              ? null
              : (v) => setState(() => qos = v!),
        ),
        const SizedBox(height: 12),
        const Text(
          'Clean session; no retained commands, no automatic reconnect/offline replay. Broker subscription acknowledgment and transport status are separate from board acknowledgment.',
        ),
      ]),
      section('Remote WebSocket', [
        field(ws, 'wss:// endpoint · optional'),
        field(token, 'Remote bearer token · secure storage', secret: true),
      ]),
      section('Routing policy', [
        field(interval, 'Telemetry interval · seconds (1–3600)'),
        SwitchListTile(
          title: const Text('Local Android HTTP / WebSocket bridge'),
          subtitle: const Text(
            'Loopback only · bearer token required · no browser CORS',
          ),
          value: local,
          onChanged: working || bridge.running
              ? null
              : (v) => setState(() => local = v),
        ),
        SwitchListTile(
          title: const Text('Allow remote/local control commands'),
          value: commands,
          onChanged: working || bridge.running
              ? null
              : (v) => setState(() => commands = v),
        ),
        SwitchListTile(
          title: const Text('Allow firmware export / pinned staging'),
          subtitle: const Text(
            'No remote apply, reboot, arbitrary URL proxy or binary auto-flash',
          ),
          value: firmware,
          onChanged: working || bridge.running
              ? null
              : (v) => setState(() => firmware = v),
        ),
      ]),
      section('Bridge session', [
        Text(bridge.state),
        Text(
          'MQTT: ${bridge.mqttConnected ? 'connected' : 'offline'} · remote WebSocket: ${bridge.webSocketConnected ? 'connected' : 'offline'}',
        ),
        Text(
          'MQTT request subscription: ${bridge.subscriptionAcknowledged ? 'acknowledged by broker' : 'not acknowledged'}',
        ),
        Text(
          'Received ${bridge.received} · rejected ${bridge.rejected} · telemetry snapshots ${bridge.sent}',
        ),
        if (bridge.server != null)
          SelectableText(
            'Local endpoint: http://127.0.0.1:${bridge.server!.port}\nBearer token: ${bridge.token}',
          ),
        if (bridge.admission != null && bridge.running)
          SelectableText('Request session: ${bridge.admission!.session}'),
        const Text(
          'Tokens are intentionally visible here for explicit trusted-app setup; never included in telemetry/discovery. Requests require id, bridge_session, board_id, boot_id and expires_at_ms (max 60 seconds).',
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 12,
          children: [
            FilledButton.icon(
              onPressed: working || bridge.running ? null : start,
              icon: const Icon(PakarIcons.play),
              label: const Text('Start bridge'),
            ),
            OutlinedButton(
              onPressed: working || !bridge.running
                  ? null
                  : () => bridge.stop(),
              child: const Text('Stop bridge'),
            ),
          ],
        ),
        if (working) const LinearProgressIndicator(),
        for (final incident in bridge.incidents.entries)
          Text(
            '${incident.key}${incident.value > 1 ? ' · ${incident.value} occurrences' : ''}',
          ),
        if (message.isNotEmpty) Text(message),
      ]),
    ],
  );
}
