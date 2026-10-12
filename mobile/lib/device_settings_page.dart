import 'dart:async';

import 'package:flutter/material.dart';

import 'api.dart';
import 'cell_search.dart';
import 'device_settings.dart';
import 'firmware_page.dart';
import 'icons.dart';
import 'models.dart';
import 'peer_context.dart';

/// Actual Pealayer PreferencesTab contract-driven sections, grouped cards,
/// labeled typed controls, contextual options, visible save/load status adapted
/// to Flutter. Pakar device writes are explicit (not donor optimistic autosave).
class DeviceSettingsPage extends StatefulWidget {
  const DeviceSettingsPage({
    super.key,
    required this.api,
    required this.status,
    required this.authorized,
    required this.navigate,
    required this.adoptConnection,
  });
  final PakarApi api;
  final DeviceStatus status;
  final bool Function(String feature) authorized;
  final void Function(String page) navigate;
  final Future<void> Function(String endpoint, String user, String password)
  adoptConnection;
  @override
  State<DeviceSettingsPage> createState() => _DeviceSettingsPageState();
}

class _DeviceSettingsPageState extends State<DeviceSettingsPage> {
  late final session = DeviceSettingsSession(
    widget.api,
    authorized: () => widget.authorized('network'),
  );
  final fields = <String, TextEditingController>{};
  final host = TextEditingController(), name = TextEditingController();
  final login = TextEditingController(),
      current = TextEditingController(),
      replacement = TextEditingController(),
      confirmation = TextEditingController();
  final ownerReads = <String, int>{};
  Map<String, dynamic>? identity, access;
  List<String> ssids = [];
  String page = 'overview', search = '', info = '';
  bool working = false;
  bool maintenanceActive = false;
  Timer? refresh;
  int operation = 0;
  @override
  void initState() {
    super.initState();
    session.addListener(changed);
    refresh = Timer.periodic(const Duration(seconds: 15), (_) => load());
    unawaited(load());
  }

  void changed() {
    if (mounted) setState(() {});
  }

  @override
  void didUpdateWidget(covariant DeviceSettingsPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (maintenanceActive &&
        !widget.status.deviceSettingsPages.any(
          (entry) => entry['id'] == 'firmware',
        )) {
      // Revocation destroys the maintenance view/owner. Do not strand unrelated
      // settings behind its confirmation lock; no firmware action is resumed.
      working = maintenanceActive = false;
    }
    if (!widget.authorized('network') && oldWidget.status.peer.has('network')) {
      session.invalidate();
      access = null;
      current.clear();
      replacement.clear();
      confirmation.clear();
    }
    if (!widget.authorized('identity')) {
      identity = null;
      host.clear();
      name.clear();
    }
  }

  @override
  void dispose() {
    ++operation;
    refresh?.cancel();
    session.removeListener(changed);
    session.dispose();
    for (final controller in [
      ...fields.values,
      host,
      name,
      login,
      current,
      replacement,
      confirmation,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  void check(String feature) {
    if (!mounted ||
        !widget.authorized(feature) ||
        widget.api.connectionRevision != session.owner) {
      throw const StaleConnection();
    }
  }

  Future<void> load() async {
    if (!mounted || working || session.pending) return;
    if (widget.authorized('network')) {
      await session.load(SettingsGroup.network);
      if (!mounted || !widget.authorized('network')) return;
      await session.load(SettingsGroup.cloud);
    }
    if (mounted && identity == null && widget.authorized('identity')) {
      await loadIdentity();
    }
    if (mounted && access == null && widget.authorized('network')) {
      await loadAccess();
    }
  }

  Future<void> readOwned(
    String feature,
    String path,
    void Function(Map<String, dynamic>) accept,
  ) async {
    final id = (ownerReads[path] ?? 0) + 1, captured = operation;
    ownerReads[path] = id;
    try {
      check(feature);
      final value = await widget.api.getJson(path);
      check(feature);
      if (captured != operation || ownerReads[path] != id || working) return;
      setState(() => accept(value));
    } catch (error) {
      if (mounted && captured == operation) {
        setState(() => info = error.toString());
      }
    }
  }

  Future<void> loadIdentity() =>
      readOwned('identity', '/api/device/name', (value) {
        if (value['hostname'] is! String ||
            !validHostname(value['hostname']) ||
            !validText(value['display_name'], 63)) {
          throw const FormatException('Device identity unavailable');
        }
        // Reloading observed identity must not replace a name draft.
        if (identity == null ||
            (host.text == identity!['hostname'] &&
                name.text == identity!['display_name'])) {
          host.text = value['hostname'];
          name.text = value['display_name'];
        }
        identity = value;
      });
  Future<void> loadAccess() => readOwned('network', '/api/auth/info', (value) {
    if (value['ready'] is! bool ||
        value['default_credentials'] is! bool ||
        value['username'] is! String) {
      throw const FormatException('Device access information unavailable');
    }
    if (access == null) login.text = value['username'];
    access = value;
  });
  Future<bool> confirm(String title, String detail) async =>
      await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(title),
          content: Text(detail),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Continue'),
            ),
          ],
        ),
      ) ??
      false;
  Future<void> reload() async {
    if (working || session.pending) return;
    final identityDirty =
        identity != null &&
        (host.text != identity!['hostname'] ||
            name.text != identity!['display_name']);
    if ((session.dirty ||
            identityDirty ||
            current.text.isNotEmpty ||
            replacement.text.isNotEmpty) &&
        !await confirm(
          'Discard and reload?',
          'Discard unsaved device and secret edits, then read the selected board again?',
        )) {
      return;
    }
    if (!mounted) return;
    session.invalidate();
    identity = access = null;
    current.clear();
    replacement.clear();
    confirmation.clear();
    await load();
  }

  Future<void> saveGroup(SettingsGroup group) async {
    if (working || session.pending) return;
    try {
      session.write(group);
    } catch (error) {
      setState(() => info = error.toString());
      return;
    }
    if (group == SettingsGroup.network &&
        !await confirm(
          'Save Wi-Fi policy?',
          'Connectivity may change. The station password is replaced on every save. Keep a recovery path enabled.',
        )) {
      return;
    }
    if (mounted) {
      setState(() => info = '');
      await session.save(group);
    }
  }

  Future<void> runWrite(String feature, Future<void> Function() action) async {
    if (!mounted || working || session.pending) return;
    ++operation;
    setState(() {
      working = true;
      info = '';
    });
    try {
      check(feature);
      await action();
    } catch (error) {
      if (mounted) setState(() => info = '$error No write was replayed.');
    } finally {
      if (mounted) setState(() => working = false);
    }
  }

  Future<void> saveIdentity() async {
    final next = host.text.toLowerCase().replaceFirst(RegExp(r'\.local$'), '');
    final label = name.text;
    if (!validHostname(next) ||
        !validText(label, 63) ||
        label.trim() != label) {
      setState(
        () => info = 'Use a valid hostname and a 1–63 UTF-8 byte board name without edge spaces or controls',
      );
      return;
    }
    if (identity == null ||
        !await confirm(
          'Save board identity?',
          'Save $label as $next.local. If the hostname changes, this client will follow it using its existing credentials.',
        )) {
      return;
    }
    await runWrite('identity', () async {
      final prior = await widget.api.getJson('/api/device/name');
      check('identity');
      if (prior['hostname'] != identity!['hostname'] ||
          prior['display_name'] != identity!['display_name']) {
        throw const FormatException(
          'Another client changed identity; reload before saving',
        );
      }
      final ack = await widget.api.postJson(
        '/api/device/name',
        {'hostname': next, 'display_name': label},
        authorize: () =>
            mounted &&
            widget.authorized('identity') &&
            widget.api.connectionRevision == session.owner,
      );
      if (widget.api.connectionRevision != session.owner) {
        throw const StaleConnection();
      }
      if (ack['accepted'] != true ||
          ack['persisted'] != true ||
          ack['hostname'] != next ||
          ack['display_name'] != label) {
        throw const FormatException(
          'Device did not confirm persisted identity',
        );
      }
      if (ack['apply_pending'] == true) {
        await widget.adoptConnection(
          'http://$next.local',
          widget.api.user,
          widget.api.password,
        );
      } else {
        final actual = await widget.api.getJson('/api/device/name');
        check('identity');
        if (actual['hostname'] != next || actual['display_name'] != label) {
          throw const FormatException('Saved identity readback differs');
        }
        identity = actual;
        host.text = next;
        info = 'Saved identity verified without Wi-Fi restart';
      }
    });
  }

  Future<void> saveAccess() async {
    final username = login.text, password = replacement.text;
    if (!RegExp(r'^[A-Za-z0-9_.-]{1,32}$').hasMatch(username) ||
        !RegExp(r'^[\x20-\x7e]{8,64}$').hasMatch(password) ||
        password != confirmation.text ||
        current.text.isEmpty) {
      setState(
        () => info = 'Enter current password, valid username, matching new password (8–64 printable ASCII characters)',
      );
      return;
    }
    if (!await confirm(
      'Change device credentials?',
      'Existing sessions will be revoked. This client will reconnect using the new login; secrets stay in platform encrypted storage.',
    )) {
      return;
    }
    await runWrite('network', () async {
      final ack = await widget.api.postJson(
        '/api/auth',
        {
          'current_password': current.text,
          'username': username,
          'password': password,
        },
        authorize: () =>
            mounted &&
            widget.authorized('network') &&
            widget.api.connectionRevision == session.owner,
      );
      if (widget.api.connectionRevision != session.owner) {
        throw const StaleConnection();
      }
      if (ack['updated'] != true || ack['reauthenticate'] != true) {
        throw const FormatException('Device did not confirm credential update');
      }
      if (mounted) {
        current.clear();
        replacement.clear();
        confirmation.clear();
      }
      await widget.adoptConnection(widget.api.baseUrl, username, password);
    });
  }

  Future<void> scan() => runWrite('network', () async {
    final value = await widget.api.getJson('/api/wifi/scan');
    check('network');
    if (value['networks'] is! List) {
      throw const FormatException('Wi-Fi scan unavailable');
    }
    ssids =
        (value['networks'] as List)
            .whereType<Map>()
            .map((v) => v['ssid'])
            .whereType<String>()
            .where((v) => validText(v, 32))
            .toSet()
            .toList()
          ..sort();
    info = '${ssids.length} SSIDs reported by this board';
  });
  TextEditingController controller(String id, String value) {
    final field = fields.putIfAbsent(
      id,
      () => TextEditingController(text: value),
    );
    if (field.text != value) {
      field.value = TextEditingValue(
        text: value,
        selection: TextSelection.collapsed(offset: value.length),
      );
    }
    return field;
  }

  Widget textField(
    String id,
    String label,
    String value,
    void Function(String) edit, {
    bool secret = false,
    bool enabled = true,
    bool number = false,
  }) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: TextField(
      key: ValueKey(id),
      controller: controller(id, value),
      enabled: enabled && !working && !session.pending,
      obscureText: secret,
      enableSuggestions: !secret,
      autocorrect: !secret,
      keyboardType: number ? TextInputType.number : TextInputType.text,
      decoration: InputDecoration(
        labelText: label,
        border: const OutlineInputBorder(),
      ),
      onChanged: edit,
    ),
  );
  Widget card(String title, List<Widget> children) => Card(
    child: Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(title, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 16),
          ...children,
        ],
      ),
    ),
  );
  Widget publicField(SettingsGroup group, String key, String label) {
    final draft = session.drafts[group]!;
    if (group.fields[key] == 'boolean') {
      return SwitchListTile.adaptive(
        contentPadding: EdgeInsets.zero,
        title: Text(label),
        value: draft.value[key] == true,
        onChanged: working || session.pending
            ? null
            : (value) => session.set(group, key, value),
      );
    }
    final numeric = ['port', 'interval'].contains(group.fields[key]);
    return textField('$group:$key', label, '${draft.value[key]}', (value) {
      session.set(
        group,
        key,
        numeric ? int.tryParse(latinDigits(value)) ?? value : value,
      );
    }, number: numeric);
  }

  Widget secretField(
    String key,
    String label,
    SecretEdit edit,
    bool presence,
  ) => Column(
    children: [
      DropdownButtonFormField<SecretMode>(
        isExpanded: true,
        icon: const Icon(PakarIcons.caretDown),
        key: ValueKey('$key:${edit.mode.name}'),
        initialValue: edit.mode,
        decoration: InputDecoration(labelText: '$label handling'),
        items: [
          DropdownMenuItem(
            value: SecretMode.keep,
            child: Text(
              presence ? 'Keep stored $label' : 'Leave $label unset',
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const DropdownMenuItem(
            value: SecretMode.replace,
            child: Text('Replace'),
          ),
          const DropdownMenuItem(
            value: SecretMode.clear,
            child: Text('Clear stored value'),
          ),
        ],
        onChanged: working || session.pending
            ? null
            : (mode) => setState(() {
                edit.mode = mode!;
                if (mode != SecretMode.replace) edit.value = '';
              }),
      ),
      const SizedBox(height: 12),
      if (edit.mode == SecretMode.replace)
        textField(key, 'Replacement $label', edit.value, (value) {
          edit.value = value;
          session.changed();
        }, secret: true),
    ],
  );
  Widget groupEditor(SettingsGroup group) {
    final draft = session.drafts[group];
    if (draft == null) {
      return card('Saved settings unavailable', [
        const Text('No editable defaults. Reload to read the real device.'),
      ]);
    }
    final cloud = group == SettingsGroup.cloud, mqtt = page == 'mqtt';
    return card(
      cloud
          ? mqtt
                ? 'MQTT broker'
                : 'Outbound WebSocket'
          : 'Wi-Fi policy',
      [
        if (!cloud) ...[
          publicField(group, 'station_enabled', 'Station enabled'),
          publicField(group, 'ap_enabled', 'Recovery AP enabled'),
          const SizedBox(height: 8),
          publicField(group, 'station_ssid', 'Station SSID'),
          OutlinedButton.icon(
            onPressed: working || session.pending ? null : scan,
            icon: const Icon(PakarIcons.wifiHigh),
            label: const Text('Scan Wi-Fi'),
          ),
          if (ssids.isNotEmpty)
            DropdownButtonFormField<String>(
              isExpanded: true,
              icon: const Icon(PakarIcons.caretDown),
              decoration: const InputDecoration(labelText: 'Scanned SSIDs'),
              items: ssids
                  .map(
                    (ssid) => DropdownMenuItem(
                      value: ssid,
                      child: Text(ssid, overflow: TextOverflow.ellipsis),
                    ),
                  )
                  .toList(),
              onChanged: working || session.pending
                  ? null
                  : (value) => session.set(group, 'station_ssid', value!),
            ),
          const SizedBox(height: 12),
          textField(
            'wifi-password',
            'Station password · re-enter on save',
            session.wifiPassword,
            (value) {
              session.wifiPassword = value;
              session.changed();
            },
            secret: true,
            enabled: !session.openNetwork,
          ),
          SwitchListTile.adaptive(
            contentPadding: EdgeInsets.zero,
            title: const Text('Open station network · no password'),
            value: session.openNetwork,
            onChanged: working || session.pending
                ? null
                : (value) => setState(() => session.openNetwork = value),
          ),
          const Text(
            'The station password is write-only and replaced on every save. Keep at least one network path enabled.',
          ),
        ] else ...[
          publicField(
            group,
            mqtt ? 'mqtt_enabled' : 'websocket_enabled',
            mqtt ? 'Enable MQTT' : 'Enable outbound WebSocket',
          ),
          publicField(group, mqtt ? 'mqtt_tls' : 'websocket_tls', 'TLS'),
          const SizedBox(height: 8),
          publicField(
            group,
            mqtt ? 'mqtt_host' : 'websocket_host',
            mqtt ? 'Broker hostname' : 'Server hostname',
          ),
          publicField(group, mqtt ? 'mqtt_port' : 'websocket_port', 'Port'),
          if (mqtt) ...[
            publicField(group, 'mqtt_user', 'Username'),
            publicField(group, 'mqtt_base_topic', 'Base topic'),
          ] else
            publicField(group, 'websocket_path', 'Path'),
          secretField(
            mqtt ? 'mqtt-password' : 'websocket-token',
            mqtt ? 'password' : 'token',
            mqtt ? session.mqttPassword : session.websocketToken,
            draft.value[mqtt ? 'mqtt_password_set' : 'websocket_token_set'] ==
                true,
          ),
          publicField(
            group,
            'telemetry_seconds',
            'Telemetry interval · seconds',
          ),
          const Text(
            'MQTT and WebSocket share one saved cloud configuration. Both public domains and secret-presence flags are verified on save.',
          ),
        ],
        if (draft.conflict)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text(
              'Conflict: another client changed this configuration. Your draft is retained; discard and reload before saving.',
            ),
          ),
        const SizedBox(height: 16),
        Align(
          alignment: Alignment.centerRight,
          child: FilledButton.icon(
            onPressed: working || session.pending || draft.conflict
                ? null
                : () => saveGroup(group),
            icon: const Icon(PakarIcons.floppyDisk),
            label: const Text('Save and verify'),
          ),
        ),
      ],
    );
  }

  Widget detail() => switch (page) {
    'wifi' => groupEditor(SettingsGroup.network),
    'mqtt' || 'websocket' => groupEditor(SettingsGroup.cloud),
    'identity' => card('Board identity', [
      TextField(
        controller: host,
        enabled: identity != null && !working,
        decoration: const InputDecoration(
          labelText: 'Hostname · optional .local',
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: name,
        enabled: identity != null && !working,
        decoration: const InputDecoration(labelText: 'Board display name'),
      ),
      const SizedBox(height: 12),
      Text(
        'Current application: ${widget.status.peer.application?.title ?? 'Not advertised'}',
      ),
      const Text(
        'Application title and artwork come from the running firmware; they are not fabricated client settings.',
      ),
      const SizedBox(height: 16),
      FilledButton.icon(
        onPressed: identity == null || working || session.pending
            ? null
            : saveIdentity,
        icon: const Icon(PakarIcons.floppyDisk),
        label: const Text('Save identity'),
      ),
    ]),
    'access' => card('Device access', [
      if (access == null)
        const Text('Access information unavailable')
      else
        Text(
          access!['ready'] != true
              ? 'Storage is not ready; physical USB recovery is required.'
              : access!['default_credentials'] == true
              ? 'Default credentials active. Change them here.'
              : 'Configured device credentials active.',
        ),
      const SizedBox(height: 12),
      TextField(
        controller: login,
        enabled: !working,
        decoration: const InputDecoration(labelText: 'Username'),
      ),
      const SizedBox(height: 12),
      for (final entry in [
        (current, 'Current password'),
        (replacement, 'New password'),
        (confirmation, 'Confirm new password'),
      ]) ...[
        TextField(
          controller: entry.$1,
          enabled: !working,
          obscureText: true,
          enableSuggestions: false,
          autocorrect: false,
          decoration: InputDecoration(labelText: entry.$2),
        ),
        const SizedBox(height: 12),
      ],
      const Text(
        'Plain HTTP LAN access is not encrypted in transit. Use a trusted network. No secret value is exported or stored in ordinary preferences.',
      ),
      const SizedBox(height: 16),
      FilledButton.icon(
        onPressed: access?['ready'] != true || working || session.pending
            ? null
            : saveAccess,
        icon: const Icon(PakarIcons.lock),
        label: const Text('Change and reconnect'),
      ),
    ]),
    'leds' || 'front-panel' || 'avr' => card('Actual board controls', [
      const Text(
        'Open the existing live controls; no cloned settings or build-only switches.',
      ),
      const SizedBox(height: 16),
      FilledButton.icon(
        onPressed: () => widget.navigate(
          page == 'leds'
              ? 'table'
              : page == 'avr'
              ? 'hardware'
              : 'front-panel',
        ),
        icon: const Icon(PakarIcons.link),
        label: const Text('Open live controls'),
      ),
    ]),
    'firmware' => FirmwarePage(
      api: widget.api,
      authorized: widget.authorized,
      busyChanged: (value) {
        if (mounted) {
          setState(() {
            working = value;
            maintenanceActive = value;
          });
        }
      },
    ),
    _ => card('Observed device', [
      Text(widget.status.peer.name),
      Text('${widget.status.hostname}.local'),
      for (final entry in widget.status.observedBuildInformation.entries)
        Text('${entry.key}: ${entry.value}'),
      const SizedBox(height: 12),
      const Text(
        'UART mode, AVR clock/fuses, pins, chipset/channel order and pixel count are build parameters, not ineffective runtime switches.',
      ),
    ]),
  };
  IconData pageIcon(String icon) => switch (icon) {
    'GLOBE' => PakarIcons.globe,
    'WIFI_HIGH' => PakarIcons.wifiHigh,
    'BROADCAST' => PakarIcons.broadcast,
    'LINK' => PakarIcons.link,
    'LOCK' => PakarIcons.lock,
    'GRID_FOUR' => PakarIcons.gridFour,
    'GAUGE' => PakarIcons.gauge,
    'CPU' => PakarIcons.cpu,
    'FLOPPY_DISK' => PakarIcons.floppyDisk,
    _ => PakarIcons.circuitry,
  };
  @override
  Widget build(BuildContext context) {
    final pages = widget.status.deviceSettingsPages;
    if (!pages.any((p) => p['id'] == page)) page = 'overview';
    final visible = pages
        .where(
          (p) => '${p['label']} ${p['keywords']}'.toLowerCase().contains(
            search.toLowerCase(),
          ),
        )
        .toList();
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'Device settings',
                      style: Theme.of(context).textTheme.headlineSmall,
                    ),
                    Text(
                      '${widget.status.peer.name} · ${widget.status.hostname}.local',
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: 'Discard and reload device settings',
                onPressed: working || session.pending ? null : reload,
                icon: const Icon(PakarIcons.arrowClockwise),
              ),
            ],
          ),
        ),
        if ((working && !maintenanceActive) || session.pending)
          const LinearProgressIndicator(),
        Expanded(
          child: LayoutBuilder(
            builder: (context, bounds) {
              final wide = bounds.maxWidth >= 760;
              final navigation = Column(
                children: [
                  TextField(
                    decoration: const InputDecoration(
                      labelText: 'Find setting',
                      prefixIcon: Icon(PakarIcons.gear),
                    ),
                    onChanged: (value) => setState(() => search = value),
                  ),
                  const SizedBox(height: 12),
                  if (!wide)
                    Wrap(
                      spacing: 8,
                      runSpacing: 4,
                      children: [
                        for (final p in visible)
                          ChoiceChip(
                            avatar: Icon(
                              pageIcon(p['icon'] as String),
                              size: 18,
                            ),
                            label: Text(p['label'] as String),
                            selected: page == p['id'],
                            showCheckmark: false,
                            onSelected: working || session.pending
                                ? null
                                : (_) =>
                                      setState(() => page = p['id'] as String),
                          ),
                      ],
                    )
                  else
                    for (final p in visible)
                      ListTile(
                        dense: true,
                        leading: Icon(pageIcon(p['icon'] as String)),
                        title: Text(p['label'] as String),
                        selected: page == p['id'],
                        shape: RoundedRectangleBorder(
                          borderRadius: BorderRadius.circular(8),
                        ),
                        onTap: working || session.pending
                            ? null
                            : () => setState(() => page = p['id'] as String),
                      ),
                ],
              );
              final body = ListView(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                children: [
                  if (!wide) ...[navigation, const SizedBox(height: 12)],
                  detail(),
                  const SizedBox(height: 12),
                  Semantics(
                    liveRegion: true,
                    child: Text(info.isNotEmpty ? info : session.message),
                  ),
                ],
              );
              return wide
                  ? Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        SizedBox(
                          width: 210,
                          child: SingleChildScrollView(
                            padding: const EdgeInsets.only(left: 16),
                            child: navigation,
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(child: body),
                      ],
                    )
                  : body;
            },
          ),
        ),
      ],
    );
  }
}
