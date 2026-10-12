import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'documents.dart';
import 'pakar_reference.dart' show SevenSegmentPreview;
import 'unified/transport.dart';
import 'unified/effect_editor.dart';

void main() => runApp(const UnifiedControllerApp());

class UnifiedControllerApp extends StatelessWidget {
  const UnifiedControllerApp({super.key, this.restore = true});
  final bool restore;
  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'PCController',
    debugShowCheckedModeBanner: false,
    themeMode: ThemeMode.system,
    theme: _theme(Brightness.light),
    darkTheme: _theme(Brightness.dark),
    home: ControllerHome(restore: restore),
  );
  ThemeData _theme(Brightness brightness) => ThemeData(
    useMaterial3: true,
    brightness: brightness,
    fontFamily: 'Vazirmatn',
    colorScheme: ColorScheme.fromSeed(
      seedColor: const Color(0xff8b5cf6),
      brightness: brightness,
    ),
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
      isDense: true,
    ),
    cardTheme: const CardThemeData(margin: EdgeInsets.zero),
    visualDensity: VisualDensity.standard,
  );
}

class ControllerHome extends StatefulWidget {
  const ControllerHome({super.key, this.restore = true});
  final bool restore;
  @override
  State<ControllerHome> createState() => _ControllerHomeState();
}

class _ControllerHomeState extends State<ControllerHome>
    with WidgetsBindingObserver {
  final board = PeerSession(controller: true),
      player = PeerSession(controller: false);
  final secure = const FlutterSecureStorage();
  final displayText = TextEditingController(),
      mediaTarget = TextEditingController();
  final draftName = TextEditingController(),
      draftProgram = TextEditingController(text: '{"steps": []}');
  final logs = <String>[];
  int section = 0, boardTab = 0, playerTab = 0;
  bool busy = false;
  int activeRequests = 0;
  String? lastError, bridgeTarget;
  String boardOrigin = '', playerOrigin = '', boardToken = '', playerToken = '';
  List<Json> drafts = [];
  List<Json> deliveries = [];
  String displayTarget = 'segments';
  double toneHz = 880, toneMs = 115;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    board.addListener(_changed);
    player.addListener(_changed);
    if (widget.restore) unawaited(_restore());
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  Future<void> _restore() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      boardOrigin =
          prefs.getString('pccontroller.origin') ??
          const String.fromEnvironment('PC_ORIGIN');
      playerOrigin = prefs.getString('pealayer.origin') ?? '';
      boardToken = await secure.read(key: 'pccontroller.token') ?? '';
      playerToken = await secure.read(key: 'pealayer.token') ?? '';
      drafts = objects(jsonDecode(prefs.getString('local.drafts') ?? '[]'));
      if (!mounted) return;
      setState(() {});
      if (boardOrigin.isNotEmpty) {
        unawaited(board.connect(boardOrigin, boardToken));
      }
      if (playerOrigin.isNotEmpty) {
        unawaited(player.connect(playerOrigin, playerToken));
      }
    } catch (_) {
      if (mounted) {
        setState(
          () => lastError =
              'Saved connection unavailable. Configure peers again.',
        );
      }
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.detached) {
      board.disconnect();
      player.disconnect();
    } else if (state == AppLifecycleState.resumed && widget.restore) {
      if (boardOrigin.isNotEmpty) {
        unawaited(board.connect(boardOrigin, boardToken));
      }
      if (playerOrigin.isNotEmpty) {
        unawaited(player.connect(playerOrigin, playerToken));
      }
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    board.removeListener(_changed);
    player.removeListener(_changed);
    board.dispose();
    player.dispose();
    displayText.dispose();
    mediaTarget.dispose();
    draftName.dispose();
    draftProgram.dispose();
    super.dispose();
  }

  Future<void> perform(
    String label,
    Future<dynamic> Function() action, {
    bool priority = false,
  }) async {
    if (busy && !priority) return;
    setState(() {
      activeRequests++;
      busy = true;
      lastError = null;
    });
    try {
      final result = await action();
      if (!mounted) return;
      logs.insert(
        0,
        '$label · acknowledged${result == null ? '' : ': ${_summary(result)}'}',
      );
      if (logs.length > 100) logs.removeLast();
    } catch (e) {
      if (mounted) setState(() => lastError = '$e');
    } finally {
      if (mounted) {
        setState(() {
          activeRequests--;
          busy = activeRequests > 0;
        });
      }
    }
  }

  String _summary(dynamic value) {
    final map = object(value);
    final text =
        '${map['output'] ?? map['state'] ?? (map['accepted'] == true ? 'accepted, not yet applied' : 'request returned')}';
    return text.length > 300 ? '${text.substring(0, 300)}…' : text;
  }

  Future<bool> confirm(String title, String description) async =>
      await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(title),
          content: Text(description),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Confirm'),
            ),
          ],
        ),
      ) ??
      false;
  Future<void> boardCommand(
    String command, {
    bool sensitive = false,
    bool safetyStop = false,
  }) async {
    final stamp = board.targetStamp;
    if (!board.boardReady || (busy && !safetyStop)) return;
    if (sensitive && !await confirm('Apply board action?', command)) return;
    if (stamp != board.targetStamp || !board.boardReady) {
      setState(() => lastError = 'Board changed; nothing dispatched.');
      return;
    }
    await perform(
      command.split(' ').first,
      () => board.transport!.rpc('controller.command.execute', {
        'command': command,
      }),
      priority: safetyStop,
    );
  }

  Future<void> invoke(Json control, Json action) async {
    final stamp = board.targetStamp;
    final safetyStop = ['stop', 'off'].contains(action['verb']);
    if (!board.boardReady ||
        (busy && !safetyStop) ||
        (!safetyStop &&
            object(board.state['emergency_stop'])['active'] == true)) {
      return;
    }
    if (control['kind'] == 'seat' &&
        !safetyStop &&
        !await confirm(
          'Move ${control['name']}?',
          'Keep the physical area clear. Motion continues until Stop or the board safety limit.',
        )) {
      return;
    }
    if (stamp != board.targetStamp || !board.boardReady) return;
    await perform(
      '${control['name']} ${action['name']}',
      () => board.transport!.rpc('controller.action.invoke', {
        'action_id': action['id'],
      }),
      priority: safetyStop,
    );
  }

  Future<void> playerCommand(String method, [Json params = const {}]) async {
    if (player.fresh) {
      await perform(method, () => player.transport!.rpc(method, params));
    } else if (board.fresh && bridgeTarget != null) {
      final target = bridgeTarget;
      if (!board.instances.any((item) => item['id'] == target)) {
        throw const Rejected('Pealayer instance expired.');
      }
      final value =
          params['seconds'] ??
          params['value'] ??
          params['target'] ??
          params['workspace'] ??
          '';
      await perform(method, () async {
        final receipt = object(
          await board.transport!.rpc('controller.app.action', {
            'kind': method,
            'value': '$value',
            'target': target,
            'source': 'pccontroller-mobile',
          }),
        );
        final operation = object(receipt['operation']);
        if (operation['operation_id'] is String) {
          deliveries = [operation, ...deliveries].take(30).toList();
        }
        // A receipt is tracked, never presented as successful application.
        return {
          'output':
              'Queued for $target. Check its application outcome in Activities.',
          ...receipt,
        };
      });
    }
  }

  Future<void> configure(bool controller) async {
    final origin = TextEditingController(
      text: controller ? boardOrigin : playerOrigin,
    );
    final token = TextEditingController(
      text: controller ? boardToken : playerToken,
    );
    final saved = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('Connect ${controller ? 'PCController' : 'Pealayer'}'),
        content: SizedBox(
          width: 440,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: origin,
                keyboardType: TextInputType.url,
                autocorrect: false,
                decoration: const InputDecoration(
                  labelText: 'Host URL',
                  hintText: 'http://192.168.1.20:8787',
                ),
              ),
              const SizedBox(height: 16),
              TextField(
                controller: token,
                obscureText: true,
                autocorrect: false,
                enableSuggestions: false,
                decoration: const InputDecoration(
                  labelText: 'Access token (if required)',
                ),
              ),
              const SizedBox(height: 12),
              const Text(
                'Use HTTPS on untrusted networks. Tokens stay in Android secure storage.',
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () {
              try {
                endpoint(origin.text);
                Navigator.pop(dialogContext, true);
              } catch (e) {
                ScaffoldMessenger.of(context)
                    .showSnackBar(SnackBar(content: Text('$e')));
              }
            },
            child: const Text('Connect'),
          ),
        ],
      ),
    );
    if (saved == true && mounted) {
      final address = endpoint(origin.text).toString(), secret = token.text;
      await perform('Connect', () async {
        final prefs = await SharedPreferences.getInstance();
        await prefs.setString(
          controller ? 'pccontroller.origin' : 'pealayer.origin',
          address,
        );
        await secure.write(
          key: controller ? 'pccontroller.token' : 'pealayer.token',
          value: secret,
        );
        if (controller) {
          boardOrigin = address;
          boardToken = secret;
          await board.connect(address, secret);
        } else {
          playerOrigin = address;
          playerToken = secret;
          await player.connect(address, secret);
        }
      });
    }
    origin.dispose();
    token.dispose();
  }

  Widget action(
    String label,
    IconData icon,
    VoidCallback? callback, {
    bool priority = false,
  }) => FilledButton.tonalIcon(
    onPressed: busy && !priority ? null : callback,
    icon: Icon(icon, size: 18),
    label: Text(label),
  );
  Widget card(
    String title,
    IconData icon,
    List<Widget> children, {
    Color? color,
  }) => Card(
    child: Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, color: color),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  title,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          ...children,
        ],
      ),
    ),
  );
  Widget grid(List<Widget> children, {double minimum = 340}) => LayoutBuilder(
    builder: (context, bounds) {
      final columns = bounds.maxWidth >= minimum * 2 + 16
          ? (bounds.maxWidth / (minimum + 16)).floor().clamp(2, 3)
          : 1;
      final width = (bounds.maxWidth - (columns - 1) * 16) / columns;
      return Wrap(
        spacing: 16,
        runSpacing: 16,
        children: children
            .map((child) => SizedBox(width: width, child: child))
            .toList(),
      );
    },
  );
  Widget tabs(List<String> values, int selected, ValueChanged<int> change) =>
      SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            for (var i = 0; i < values.length; i++)
              Padding(
                padding: const EdgeInsetsDirectional.only(end: 8),
                child: ChoiceChip(
                  label: Text(values[i]),
                  selected: selected == i,
                  onSelected: (_) => change(i),
                ),
              ),
          ],
        ),
      );
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, bounds) {
      final tablet = bounds.maxWidth >= 760;
      final pages = ['PCController', 'Pealayer', 'Activities', 'Workspace'];
      final icons = [
        Icons.memory_outlined,
        Icons.play_circle_outline,
        Icons.insights,
        Icons.dashboard_customize_outlined,
      ];
      final content = Column(
        children: [
          if (busy) const LinearProgressIndicator(),
          if (lastError != null)
            MaterialBanner(
              content: Text(lastError!),
              leading: const Icon(Icons.warning_amber),
              actions: [
                TextButton(
                  onPressed: () => setState(() => lastError = null),
                  child: const Text('Dismiss'),
                ),
              ],
            ),
          Expanded(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(16),
              child: switch (section) {
                0 => boardPage(),
                1 => Theme(
                  data: Theme.of(context).copyWith(
                    colorScheme: ColorScheme.fromSeed(
                      seedColor: const Color(0xff95bc66),
                      brightness: Theme.of(context).brightness,
                    ),
                  ),
                  child: playerPage(),
                ),
                2 => activities(),
                _ => workspace(),
              },
            ),
          ),
        ],
      );
      return Scaffold(
        appBar: AppBar(
          title: Row(
            children: [
              Image.asset(
                'assets/branding/${section == 1 ? 'pealayer' : 'pccontroller'}.png',
                width: 30,
                height: 30,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  pages[section],
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          actions: [
            IconButton(
              tooltip: 'Connect peers',
              icon: const Icon(Icons.link),
              onPressed: () => configure(section != 1),
            ),
          ],
        ),
        body: tablet
            ? Row(
                children: [
                  NavigationRail(
                    selectedIndex: section,
                    labelType: NavigationRailLabelType.all,
                    onDestinationSelected: (value) =>
                        setState(() => section = value),
                    destinations: [
                      for (var i = 0; i < pages.length; i++)
                        NavigationRailDestination(
                          icon: Icon(icons[i]),
                          label: Text(pages[i]),
                        ),
                    ],
                  ),
                  const VerticalDivider(width: 1),
                  Expanded(child: content),
                ],
              )
            : content,
        bottomNavigationBar: tablet
            ? null
            : NavigationBar(
                selectedIndex: section,
                onDestinationSelected: (value) =>
                    setState(() => section = value),
                destinations: [
                  for (var i = 0; i < pages.length; i++)
                    NavigationDestination(
                      icon: Icon(icons[i]),
                      label: pages[i],
                    ),
                ],
              ),
      );
    },
  );
  Widget connection(PeerSession peer, String name) =>
      card(name, Icons.lan_outlined, [
        Text(
          peer.fresh
              ? (peer.controller
                    ? (peer.state['connected'] == true
                          ? 'Board connected'
                          : 'Host connected · board offline')
                    : 'Player connected')
              : 'Offline · last values unavailable',
          style: Theme.of(context).textTheme.titleLarge,
        ),
        if (peer.error != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(peer.error!),
          ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            action('Connect', Icons.link, () => configure(peer.controller)),
            action(
              'Refresh',
              Icons.refresh,
              peer.transport == null ? null : () => unawaited(peer.refresh()),
            ),
            action(
              'Disconnect',
              Icons.link_off,
              peer.enabled ? peer.disconnect : null,
            ),
          ],
        ),
      ]);
  Widget boardPage() => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      connection(board, 'PCController'),
      const SizedBox(height: 16),
      tabs(
        ['Monitor', 'Outputs', 'Front panel', 'Firmware', 'Effects'],
        boardTab,
        (value) {
          setState(() => boardTab = value);
          if (value == 3 && board.online) {
            unawaited(perform('Artifact catalog', board.loadArtifacts));
          }
        },
      ),
      const SizedBox(height: 16),
      switch (boardTab) {
        0 => monitor(),
        1 => outputs(),
        2 => frontPanel(),
        3 => firmware(),
        _ => effects(),
      },
    ],
  );
  Widget monitor() {
    final status = object(board.state['status']);
    String measurement(
      String key,
      num scale,
      String unit, [
      String? available,
    ]) =>
        board.measurementsFresh &&
            board.state['have_status'] == true &&
            (available == null || status[available] == true) &&
            status[key] is num
        ? '${((status[key] as num) / scale).toStringAsFixed(scale == 1 ? 0 : 2)} $unit'
        : 'Unavailable';
    return grid([
      card('Power', Icons.bolt, [
        fact('Supply', measurement('supply_mv', 1000, 'V')),
        fact('Bus', measurement('bus_mv', 1000, 'V', 'ina219_available')),
        fact('Current', measurement('current_ma', 1, 'mA', 'ina219_available')),
        fact('Power', measurement('power_mw', 1000, 'W', 'ina219_available')),
      ]),
      card('Temperature', Icons.thermostat, [
        fact(
          'Lighting',
          measurement(
            'temperature_led_centi_c',
            100,
            '°C',
            'temperature_led_available',
          ),
        ),
        fact(
          'Audio',
          measurement(
            'temperature_bt_audio_centi_c',
            100,
            '°C',
            'temperature_bt_audio_available',
          ),
        ),
        if (board.fresh && status['hot'] == true)
          const Text(
            'HOT · board reports over-temperature',
            style: TextStyle(color: Colors.red),
          ),
      ]),
      card('Board state', Icons.memory, [
        fact(
          'Name',
          board.fresh
              ? '${board.state['board_name'] ?? object(board.state['hello'])['name']}'
              : 'Unavailable',
        ),
        fact(
          'Mode',
          board.fresh
              ? '${status['program_mode'] ?? 'Unavailable'}'
              : 'Unavailable',
        ),
        fact('Door', board.fresh ? '${status['door_open']}' : 'Unavailable'),
        fact(
          'E-STOP',
          board.fresh
              ? '${object(board.state['emergency_stop'])['active']}'
              : 'Unavailable',
        ),
        action(
          'Emergency stop',
          Icons.front_hand,
          board.boardReady
              ? () => unawaited(boardCommand('estop on', safetyStop: true))
              : null,
          priority: true,
        ),
      ]),
    ]);
  }

  Widget fact(String name, String value) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 5),
    child: Row(
      children: [
        Expanded(child: Text(name)),
        Flexible(
          child: Text(
            value,
            textAlign: TextAlign.end,
            style: const TextStyle(
              fontFeatures: [FontFeature.tabularFigures()],
            ),
          ),
        ),
      ],
    ),
  );
  Widget outputs() {
    final controls = objects(board.peripherals['controls'])
        .where((control) => control['hidden'] != true)
        .toList();
    if (controls.isEmpty) {
      return const Text('Connect to load the authoritative channel catalog.');
    }
    final groups = <String, List<Json>>{};
    for (final control in controls) {
      groups
          .putIfAbsent('${control['group'] ?? 'Other outputs'}', () => [])
          .add(control);
    }
    final estop = object(board.state['emergency_stop'])['active'] == true;
    return Column(
      children: [
        for (final group in groups.entries)
          ExpansionTile(
            initiallyExpanded: true,
            title: Text(group.key),
            trailing: Text('${group.value.length}'),
            children: [
              Padding(
                padding: const EdgeInsets.only(bottom: 16),
                child: grid([
                  for (final control in group.value)
                    card(
                      '${control['name']}',
                      control['kind'] == 'seat'
                          ? Icons.airline_seat_recline_normal
                          : control['kind'] == 'relay'
                          ? Icons.power
                          : Icons.lightbulb_outline,
                      [
                        if (control['control'] == 'pwm-user')
                          _PwmControl(
                            control: control,
                            enabled:
                                board.boardReady &&
                                !busy &&
                                !estop &&
                                control['locked'] != true,
                            changed: (value) => unawaited(
                              boardCommand(
                                'pwm set ${control['key'].toString().split('.').last} $value',
                              ),
                            ),
                          ),
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            for (final entry in objects(control['actions']))
                              action(
                                '${entry['name']}',
                                entry['verb'] == 'stop'
                                    ? Icons.stop
                                    : entry['verb'] == 'up'
                                    ? Icons.arrow_upward
                                    : entry['verb'] == 'down'
                                    ? Icons.arrow_downward
                                    : Icons.power_settings_new,
                                board.boardReady &&
                                        control['locked'] != true &&
                                        (!estop ||
                                            [
                                              'stop',
                                              'off',
                                            ].contains(entry['verb']))
                                    ? () => unawaited(invoke(control, entry))
                                    : null,
                                priority: [
                                  'stop',
                                  'off',
                                ].contains(entry['verb']),
                              ),
                          ],
                        ),
                      ],
                    ),
                ]),
              ),
            ],
          ),
      ],
    );
  }

  Widget frontPanel() {
    final panel = object(board.state['front_panel']), ready = board.boardReady;
    return grid([
      card('Live front panel', Icons.dialpad, [
        if (board.fresh &&
            board.state['have_front_panel_segments'] == true &&
            panel['raw_segments'] is List)
          SevenSegmentPreview(
            segments: (panel['raw_segments'] as List).whereType<int>().toList(),
            brightness: panel['brightness'] as int? ?? 0,
            active: panel['segments_active'] == true,
          )
        else
          const Text('Waiting for physical segment frames'),
        const SizedBox(height: 12),
        fact(
          'TM1637',
          board.fresh && panel['tm1637_detection_known'] == true
              ? panel['tm1637_detected'] == true
                    ? 'Detected'
                    : 'Not detected'
              : 'Unknown',
        ),
        if (board.fresh && panel['lcd_available'] == true)
          Text(
            '${panel['lcd_line_1']}\n${panel['lcd_line_2']}',
            style: const TextStyle(fontFamily: 'monospace'),
          ),
        fact(
          'Pressed keys',
          board.fresh
              ? '${panel['pressed_keys'] ?? 'Unavailable'}'
              : 'Unavailable',
        ),
        fact(
          'Page',
          board.fresh
              ? '${panel['menu_page'] ?? 'Unavailable'}'
              : 'Unavailable',
        ),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final entry in [
              ('prev', Icons.arrow_back),
              ('next', Icons.arrow_forward),
              ('dec', Icons.remove),
              ('inc', Icons.add),
            ])
              action(
                entry.$1,
                entry.$2,
                ready
                    ? () => unawaited(boardCommand('menu ${entry.$1}'))
                    : null,
              ),
          ],
        ),
        const SizedBox(height: 8),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (var key = 1; key <= 4; key++)
              action(
                'K$key',
                Icons.touch_app,
                ready && panel['host_captured'] == true
                    ? () => unawaited(boardCommand('host-menu key K$key press'))
                    : null,
              ),
          ],
        ),
      ]),
      card('Display message', Icons.text_fields, [
        DropdownButtonFormField<String>(
          initialValue: displayTarget,
          decoration: const InputDecoration(labelText: 'Display'),
          items: [
            const DropdownMenuItem(value: 'segments', child: Text('7-segment')),
            if (panel['lcd_available'] == true)
              const DropdownMenuItem(value: 'lcd', child: Text('LCD')),
          ],
          onChanged: (value) {
            if (value != null) setState(() => displayTarget = value);
          },
        ),
        const SizedBox(height: 12),
        TextField(
          controller: displayText,
          onChanged: (_) => setState(() {}),
          maxLength: 240,
          decoration: const InputDecoration(labelText: 'Message'),
        ),
        action(
          'Show for 5 seconds',
          Icons.send,
          ready && displayText.text.trim().isNotEmpty
              ? () => unawaited(
                  boardCommand(
                    'display $displayTarget --duration 5s --repeat once -- ${jsonEncode(displayText.text)}',
                  ),
                )
              : null,
        ),
      ]),
      card('Buzzer', Icons.volume_up_outlined, [
        fact('Frequency', '${toneHz.round()} Hz'),
        Slider(
          value: toneHz,
          min: 100,
          max: 3000,
          onChanged: (value) => setState(() => toneHz = value),
        ),
        fact('Duration', '${toneMs.round()} ms'),
        Slider(
          value: toneMs,
          min: 20,
          max: 1000,
          onChanged: (value) => setState(() => toneMs = value),
        ),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            action(
              'Play bounded tone',
              Icons.play_arrow,
              ready
                  ? () => unawaited(
                      boardCommand(
                        'buzzer ${toneHz.round()} ${toneMs.round()}',
                      ),
                    )
                  : null,
            ),
            action(
              'Stop buzzer',
              Icons.stop,
              ready
                  ? () =>
                        unawaited(boardCommand('melody stop', safetyStop: true))
                  : null,
              priority: true,
            ),
          ],
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          children: [
            for (final mode in ['idle', 'running'])
              action(
                mode,
                Icons.settings,
                ready
                    ? () => unawaited(
                        perform(
                          'Host mode',
                          () => board.transport!.rpc(
                            'controller.program_state.set',
                            {'mode': mode, 'owner': 'mobile'},
                          ),
                        ),
                      )
                    : null,
              ),
          ],
        ),
      ]),
    ]);
  }

  Future<void> flash(Json artifact) async {
    final stamp = board.targetStamp, peer = board.transport;
    if (peer == null || !board.boardReady) return;
    if (!await confirm(
      'Upload board firmware?',
      '${artifact['name']}\nSHA-256: ${artifact['sha256']}\nThe host must ensure its toolchain, back up and verify the board. Do not disconnect power.',
    )) {
      return;
    }
    if (!board.boardReady || stamp != board.targetStamp) return;
    await perform('Firmware programming', () async {
      final value = object(
        await peer.rpc('controller.board.firmware.upload', {
          'artifact_sha256': artifact['sha256'],
          'authorized': true,
          'deployment': 'production',
        }),
      );
      board.operation = object(value['operation']);
      return value;
    });
  }

  Widget firmware() => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      card('Firmware & verified backups', Icons.system_update, [
        const Text(
          'Firmware is prepared on the host with its configured toolchain. A queued operation is not a successful flash.',
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            action(
              'Import firmware',
              Icons.file_upload_outlined,
              board.fresh
                  ? () => unawaited(
                      perform('Artifact import', () async {
                        final epoch = board.generation, peer = board.transport!;
                        final document = await AndroidDocuments().pick(
                          extensions: const ['hex', 'bin'],
                        );
                        if (document == null ||
                            epoch != board.generation ||
                            !board.fresh) {
                          return null;
                        }
                        final result = await peer.upload(
                          document.name,
                          document.bytes,
                        );
                        await board.loadArtifacts();
                        return result;
                      }),
                    )
                  : null,
            ),
            action(
              'Read board flash',
              Icons.download,
              board.boardReady
                  ? () => unawaited(
                      perform('Flash backup', () async {
                        final stamp = board.targetStamp,
                            peer = board.transport!;
                        if (!await confirm(
                          'Read board flash?',
                          'The host temporarily enters the programming latch, reads flash and reconnects. No firmware write is requested.',
                        )) {
                          return null;
                        }
                        if (stamp != board.targetStamp || !board.boardReady) {
                          return null;
                        }
                        final result = object(
                          await peer.rpc('controller.board.firmware.download', {
                            'components': ['flash'],
                            'authorized': true,
                          }),
                        );
                        board.operation = object(result['operation']);
                        return result;
                      }),
                    )
                  : null,
            ),
            action(
              'Refresh artifacts',
              Icons.refresh,
              board.fresh
                  ? () => unawaited(perform('Artifacts', board.loadArtifacts))
                  : null,
            ),
          ],
        ),
      ]),
      const SizedBox(height: 16),
      operationCard(),
      const SizedBox(height: 16),
      for (final artifact in board.artifacts)
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: card('${artifact['name']}', Icons.insert_drive_file_outlined, [
            fact('Kind', '${artifact['kind']}'),
            fact('Size', '${artifact['bytes']} bytes'),
            SelectableText(
              'SHA-256 ${artifact['sha256']}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                if (artifact['kind'] == 'firmware')
                  action(
                    'Program board',
                    Icons.system_update,
                    board.boardReady ? () => unawaited(flash(artifact)) : null,
                  ),
                action(
                  'Export verified file',
                  Icons.save_alt,
                  board.fresh
                      ? () => unawaited(
                          perform('Export', () async {
                            final stamp = board.targetStamp,
                                peer = board.transport!;
                            final bytes = await peer.download(artifact);
                            return AndroidDocuments().export(
                              'pccontroller-${artifact['sha256'].toString().substring(0, 12)}.bin',
                              bytes,
                              stillOwned: () =>
                                  board.targetStamp == stamp && board.fresh,
                            );
                          }),
                        )
                      : null,
                ),
              ],
            ),
          ]),
        ),
    ],
  );
  Widget operationCard() {
    final operation = board.operation;
    return card('Current operation', Icons.sync, [
      if (operation['id'] == null)
        const Text('No operation loaded')
      else ...[
        Text('${operation['stage'] ?? operation['state']}'),
        const SizedBox(height: 12),
        if (!['completed', 'failed', 'cancelled'].contains(operation['state']))
          LinearProgressIndicator(value: operationProgress(operation)),
        if (operationProgress(operation) != null)
          Text('${(operationProgress(operation)! * 100).round()}%'),
        if (operation['detail'] != null) Text('${operation['detail']}'),
        if (operation['error_code'] != null)
          Text(
            '${operation['error_code']}',
            style: const TextStyle(color: Colors.red),
          ),
      ],
      const SizedBox(height: 12),
      action(
        'Read durable status',
        Icons.refresh,
        board.fresh ? () => unawaited(board.loadOperation()) : null,
      ),
    ]);
  }

  Widget effects() {
    final value = board.state['effects'];
    final effects = value is List
        ? objects(value)
        : objects(object(value)['effects']);
    return card('Board effect library', Icons.auto_awesome, [
      action(
        'New effect draft',
        Icons.add,
        () => unawaited(
          editEffect({
            'id': '',
            'name': '',
            'category': 'Effects',
            'kind': 'sequence',
            'steps': [],
          }),
        ),
      ),
      if (effects.isEmpty)
        const Text('No effects advertised in the current snapshot.'),
      for (final effect in effects)
        ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text('${effect['name'] ?? effect['id']}'),
          subtitle: Text('${effect['category'] ?? ''}'),
          onTap: () => unawaited(editEffect(effect)),
          trailing: IconButton(
            icon: const Icon(Icons.play_arrow),
            onPressed: board.boardReady && !busy
                ? () => unawaited(
                    boardCommand(
                      'effect play ${jsonEncode(effect['reference'])}',
                      sensitive: true,
                    ),
                  )
                : null,
          ),
        ),
      action(
        'Cancel playback',
        Icons.stop,
        board.boardReady
            ? () => unawaited(boardCommand('effect cancel', safetyStop: true))
            : null,
        priority: true,
      ),
    ]);
  }

  bool get playerReady =>
      !busy && (player.fresh || (board.fresh && bridgeTarget != null));
  Widget playerPage() => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      connection(player, 'Pealayer'),
      const SizedBox(height: 16),
      tabs(
        ['Playback', 'Effects & cues', 'Peers'],
        playerTab,
        (value) => setState(() => playerTab = value),
      ),
      const SizedBox(height: 16),
      switch (playerTab) {
        0 => playback(),
        1 => playerEffects(),
        _ => peers(),
      },
    ],
  );
  Widget playback() {
    final state = player.state;
    return grid([
      card('Now playing', Icons.movie_outlined, [
        Text(
          player.fresh
              ? '${state['current_video'] ?? 'No media loaded'}'
              : bridgeTarget != null
              ? 'Remote via PCController'
              : 'Connect Pealayer to load media state',
          maxLines: 3,
        ),
        const SizedBox(height: 16),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final entry in [
              ('previous', Icons.skip_previous),
              ('play', Icons.play_arrow),
              ('pause', Icons.pause),
              ('stop', Icons.stop),
              ('next', Icons.skip_next),
            ])
              action(
                entry.$1,
                entry.$2,
                playerReady
                    ? () => unawaited(playerCommand('pealayer.${entry.$1}'))
                    : null,
              ),
          ],
        ),
        if (player.fresh &&
            state['duration'] is num &&
            state['playback_time'] is num &&
            state['seekable'] == true &&
            (state['duration'] as num) > 0) ...[
          const SizedBox(height: 16),
          fact(
            'Position',
            '${(state['playback_time'] as num).toStringAsFixed(1)} / ${(state['duration'] as num).toStringAsFixed(1)} s',
          ),
          Slider(
            value: (state['playback_time'] as num).toDouble().clamp(
              0,
              (state['duration'] as num).toDouble(),
            ),
            max: (state['duration'] as num).toDouble(),
            onChanged: busy ? null : (_) {},
            onChangeEnd: (value) => unawaited(
              playerCommand('pealayer.seek_to', {'seconds': value}),
            ),
          ),
        ],
        if (player.fresh && state['volume'] is num) ...[
          fact('Volume', '${(state['volume'] as num).round()}%'),
          Slider(
            value: (state['volume'] as num).toDouble().clamp(0, 100),
            max: 100,
            onChanged: busy ? null : (_) {},
            onChangeEnd: (value) => unawaited(
              playerCommand('pealayer.volume.set', {'value': value}),
            ),
          ),
        ],
        action(
          'Toggle mute',
          Icons.volume_off_outlined,
          playerReady
              ? () => unawaited(playerCommand('pealayer.mute.toggle'))
              : null,
        ),
      ], color: const Color(0xff79bc63)),
      card('Open media', Icons.folder_open, [
        TextField(
          controller: mediaTarget,
          onChanged: (_) => setState(() {}),
          decoration: const InputDecoration(
            labelText: 'Host path or media URL',
          ),
        ),
        const SizedBox(height: 12),
        action(
          'Open on Pealayer',
          Icons.play_circle_outline,
          playerReady && mediaTarget.text.trim().isNotEmpty
              ? () => unawaited(
                  playerCommand('pealayer.open', {
                    'target': mediaTarget.text.trim(),
                  }),
                )
              : null,
        ),
        const SizedBox(height: 16),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final mode in ['simple', 'nle'])
              action(
                mode,
                Icons.dashboard_outlined,
                playerReady
                    ? () => unawaited(
                        playerCommand('pealayer.workspace.set', {
                          'workspace': mode,
                        }),
                      )
                    : null,
              ),
          ],
        ),
      ], color: const Color(0xff79bc63)),
    ]);
  }

  Widget playerEffects() => card('Effects & timeline', Icons.auto_awesome, [
    if (!player.fresh)
      const Text(
        'Connect directly to Pealayer to load the current effect and cue catalog.',
      ),
    for (final effect in objects(player.state['controller_effects']))
      ListTile(
        contentPadding: EdgeInsets.zero,
        title: Text('${effect['name']}'),
        subtitle: Text('${effect['category'] ?? ''}'),
        trailing: Wrap(
          children: [
            IconButton(
              tooltip: 'Play effect',
              icon: const Icon(Icons.play_arrow),
              onPressed: playerReady
                  ? () => unawaited(
                      playerCommand('pealayer.controller_effect.play', {
                        'reference': effect['reference'],
                      }),
                    )
                  : null,
            ),
            IconButton(
              tooltip: 'Place cue at current position',
              icon: const Icon(Icons.add),
              onPressed: player.fresh && !busy
                  ? () => unawaited(
                      playerCommand('pealayer.controller_effect_cue.add', {
                        'reference': effect['reference'],
                        'start_time_ms':
                            ((player.state['playback_time'] as num? ?? 0) *
                                    1000)
                                .round(),
                      }),
                    )
                  : null,
            ),
          ],
        ),
      ),
    const Divider(),
    for (final cue in objects(player.state['cues']))
      ListTile(
        title: Text('${cue['name'] ?? cue['reference'] ?? cue['id']}'),
        subtitle: Text('${cue['start_time_ms'] ?? cue['start_time'] ?? ''}'),
        onTap: player.fresh && !busy
            ? () => unawaited(
                playerCommand('timeline.cue.edit', {'instance_id': cue['id']}),
              )
            : null,
      ),
    action(
      'Stop effects',
      Icons.stop,
      player.fresh
          ? () => unawaited(playerCommand('pealayer.controller_effect.stop'))
          : null,
    ),
  ]);
  Widget peers() => card('Connected peers', Icons.hub_outlined, [
    const Text(
      'Select a registered Pealayer instance to control it through PCController. Commands retain the host’s authority and delivery checks.',
    ),
    const SizedBox(height: 12),
    for (final instance in board.instances.where(
      (entry) =>
          '${object(entry['values'])['app_actions']}'.contains('pealayer.play'),
    ))
      ListTile(
        leading: Icon(
          bridgeTarget == instance['id']
              ? Icons.radio_button_checked
              : Icons.radio_button_unchecked,
        ),
        onTap: () => setState(() => bridgeTarget = '${instance['id']}'),
        title: Text('${instance['id']}'),
        subtitle: Text('${instance['surface']} · ${instance['state']}'),
      ),
    if (board.instances.isEmpty)
      const Text('No registered Pealayer instance available.'),
    action(
      'Refresh peers',
      Icons.refresh,
      board.fresh ? () => unawaited(board.refresh()) : null,
    ),
    const Divider(),
    action(
      'Read board through Pealayer',
      Icons.memory,
      player.fresh
          ? () => unawaited(
              perform(
                'Pealayer board bridge',
                () => player.transport!.request(
                  '/api/peer/controller',
                  body: {'method': 'controller.snapshot', 'params': {}},
                ),
              ),
            )
          : null,
    ),
    const SizedBox(height: 12),
    action(
      'Control board through Pealayer',
      Icons.hub,
      player.fresh
          ? () => unawaited(
              perform('Use Pealayer bridge', () async {
                await board.connectRelay(playerOrigin, playerToken);
                if (mounted) {
                  setState(() {
                    section = 0;
                    boardTab = 0;
                  });
                }
              }),
            )
          : null,
    ),
  ]);
  Widget activities() => Column(
    children: [
      operationCard(),
      const SizedBox(height: 16),
      if (deliveries.isNotEmpty)
        card('Peer delivery outcomes', Icons.checklist, [
          for (final operation in deliveries)
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: Text('${operation['kind']} · ${operation['state']}'),
              subtitle: Text('${operation['selector']}'),
              trailing: IconButton(
                tooltip: 'Refresh application outcome',
                icon: const Icon(Icons.refresh),
                onPressed: board.fresh && !busy
                    ? () => unawaited(
                        perform('Delivery outcome', () async {
                          final epoch = board.generation,
                              peer = board.transport!;
                          final value = object(
                            await peer.rpc('controller.app.action.outcome', {
                              'operation_id': operation['operation_id'],
                            }),
                          );
                          if (epoch != board.generation) return null;
                          final outcome = value['operation'] is Map
                              ? object(value['operation'])
                              : value;
                          deliveries = deliveries
                              .map(
                                (item) =>
                                    item['operation_id'] ==
                                        outcome['operation_id']
                                    ? outcome
                                    : item,
                              )
                              .toList();
                          return outcome;
                        }),
                      )
                    : null,
              ),
            ),
        ]),
      const SizedBox(height: 16),
      card('Activity', Icons.history, [
        for (final value in logs)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Text(value),
          ),
        for (final event in board.events.take(40))
          ListTile(
            contentPadding: EdgeInsets.zero,
            dense: true,
            leading: Icon(
              '${event['kind']}'.contains('hot')
                  ? Icons.thermostat
                  : Icons.bolt,
              size: 18,
            ),
            title: Text('${event['text'] ?? event['kind']}'),
            subtitle: Text('${event['time'] ?? ''}'),
          ),
        if (logs.isEmpty && board.events.isEmpty)
          const Text(
            'No activity yet. History is bounded and stays in memory.',
          ),
      ]),
    ],
  );
  Widget workspace() => Column(
    children: [
      action(
        'Native effect designer',
        Icons.auto_awesome,
        () => unawaited(
          editEffect({
            'id': '',
            'name': '',
            'category': 'Effects',
            'kind': 'sequence',
            'steps': [],
          }),
        ),
      ),
      const SizedBox(height: 16),
      card('Native offline workspace', Icons.edit_note, [
        const Text(
          'Drafts are local and work without a host. They do not run a board or publish until explicitly submitted to a connected owner.',
        ),
        const SizedBox(height: 12),
        TextField(
          controller: draftName,
          decoration: const InputDecoration(labelText: 'Effect name'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: draftProgram,
          minLines: 4,
          maxLines: 12,
          decoration: const InputDecoration(labelText: 'Effect program JSON'),
        ),
        const SizedBox(height: 12),
        action(
          'Save local draft',
          Icons.save_outlined,
          () => unawaited(
            perform('Local draft', () async {
              final program = jsonDecode(draftProgram.text);
              if (program is! Map || draftName.text.trim().isEmpty) {
                throw const Rejected('Enter a name and a JSON object.');
              }
              final draft = {
                'name': draftName.text.trim(),
                'program': program,
                'saved_at': DateTime.now().toUtc().toIso8601String(),
              };
              final next = [draft, ...drafts].take(100).toList();
              final prefs = await SharedPreferences.getInstance();
              await prefs.setString('local.drafts', jsonEncode(next));
              drafts = next;
              return null;
            }),
          ),
        ),
        for (final draft in drafts)
          ListTile(
            contentPadding: EdgeInsets.zero,
            title: Text('${draft['name']}'),
            subtitle: Text('${draft['saved_at']}'),
            trailing: draft['effect'] is Map
                ? IconButton(
                    tooltip: 'Publish draft to PCController',
                    icon: const Icon(Icons.cloud_upload_outlined),
                    onPressed: board.fresh && !busy
                        ? () => unawaited(publishDraft(object(draft['effect'])))
                        : null,
                  )
                : null,
            onTap: () => setState(() {
              draftName.text = '${draft['name']}';
              draftProgram.text = const JsonEncoder.withIndent('  ')
                  .convert(draft['program']);
            }),
          ),
      ]),
      const SizedBox(height: 16),
      card('Connections', Icons.settings_outlined, [
        action('PCController connection', Icons.memory, () => configure(true)),
        const SizedBox(height: 8),
        action(
          'Pealayer connection',
          Icons.play_circle_outline,
          () => configure(false),
        ),
      ]),
    ],
  );
  Future<void> editEffect(Json initial) => showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (_) => EffectEditor(
      initial: initial,
      saved: (effect) async {
        final next = [
          {
            'name': effect['name'],
            'effect': effect,
            'program': effect,
            'saved_at': DateTime.now().toUtc().toIso8601String(),
          },
          ...drafts,
        ].take(100).toList();
        final prefs = await SharedPreferences.getInstance();
        await prefs.setString('local.drafts', jsonEncode(next));
        if (mounted) setState(() => drafts = next);
      },
    ),
  );
  Future<void> publishDraft(Json effect) async {
    final stamp = board.targetStamp, peer = board.transport;
    if (peer == null || !board.fresh) return;
    if (!await confirm(
      'Publish ${effect['name']}?',
      'Save effect ID ${effect['id']} on PCController. An existing matching ID is replaced. This does not run the effect.',
    )) {
      return;
    }
    if (stamp != board.targetStamp || !board.fresh) return;
    await perform('Publish effect', () async {
      final bytes = utf8.encode(jsonEncode(effect));
      final hex = bytes
          .map((value) => value.toRadixString(16).padLeft(2, '0'))
          .join();
      final result = await peer.rpc('controller.command.execute', {
        'command': 'effect upsert-json $hex',
      });
      await board.refresh();
      return result;
    });
  }
}

class _PwmControl extends StatefulWidget {
  const _PwmControl({
    required this.control,
    required this.enabled,
    required this.changed,
  });
  final Json control;
  final bool enabled;
  final ValueChanged<int> changed;
  @override
  State<_PwmControl> createState() => _PwmControlState();
}

class _PwmControlState extends State<_PwmControl> {
  double value = 0;
  @override
  Widget build(BuildContext context) => Column(
    children: [
      Text('Setpoint ${value.round()}%'),
      Slider(
        value: value,
        max: 100,
        onChanged: widget.enabled
            ? (next) => setState(() => value = next)
            : null,
        onChangeEnd: widget.enabled
            ? (next) => widget.changed(next.round())
            : null,
      ),
    ],
  );
}
