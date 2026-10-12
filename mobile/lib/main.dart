import 'dart:async';
import 'dart:convert';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'documents.dart';
import 'pakar_reference.dart' show SevenSegmentPreview;
import 'unified/transport.dart';
import 'unified/effect_editor.dart';
import 'unified/live_widgets.dart';
import 'unified/automation.dart';
import 'unified/preferences.dart';
import 'unified/buzzer_mirror.dart';
import 'unified/channel_editor.dart';
import 'unified/cue_editor.dart';

void main() => runApp(const UnifiedControllerApp());

class UnifiedControllerApp extends StatefulWidget {
  const UnifiedControllerApp({super.key, this.restore = true});
  final bool restore;
  @override
  State<UnifiedControllerApp> createState() => _UnifiedControllerAppState();
}

class _UnifiedControllerAppState extends State<UnifiedControllerApp> {
  final preferences = AppPreferences();
  @override
  void dispose() {
    preferences.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: preferences,
    builder: (context, _) => MaterialApp(
      title: 'PCController',
      debugShowCheckedModeBanner: false,
      themeMode: preferences.theme,
      theme: _theme(Brightness.light),
      darkTheme: _theme(Brightness.dark),
      home: ControllerHome(restore: widget.restore, preferences: preferences),
    ),
  );
  ThemeData _theme(Brightness brightness) => ThemeData(
    useMaterial3: true,
    brightness: brightness,
    fontFamily: 'Vazirmatn',
    colorScheme:
        ColorScheme.fromSeed(
          seedColor: const Color(0xff7166ce),
          brightness: brightness,
        ).copyWith(
          surface: brightness == Brightness.dark
              ? const Color(0xff232323)
              : const Color(0xfffafafa),
          surfaceContainer: brightness == Brightness.dark
              ? const Color(0xff292929)
              : const Color(0xfff2f2f3),
          onSurface: brightness == Brightness.dark
              ? const Color(0xffededed)
              : const Color(0xff242428),
          onSurfaceVariant: brightness == Brightness.dark
              ? const Color(0xffaaaaae)
              : const Color(0xff64646a),
          outline: brightness == Brightness.dark
              ? const Color(0xff414145)
              : const Color(0xffd2d2d6),
        ),
    scaffoldBackgroundColor: brightness == Brightness.dark
        ? const Color(0xff1c1c1e)
        : const Color(0xfff5f5f7),
    appBarTheme: const AppBarTheme(
      centerTitle: false,
      scrolledUnderElevation: 0,
    ),
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
      isDense: true,
    ),
    cardTheme: CardThemeData(
      margin: EdgeInsets.zero,
      elevation: 0,
      color: brightness == Brightness.dark
          ? const Color(0xff232323)
          : const Color(0xfffafafa),
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(
          color: brightness == Brightness.dark
              ? const Color(0xff3c3c40)
              : const Color(0xffdddddf),
        ),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(40, 42),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
      ),
    ),
    chipTheme: ChipThemeData(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
    ),
    visualDensity: preferences.compact
        ? VisualDensity.compact
        : VisualDensity.standard,
  );
}

class ControllerHome extends StatefulWidget {
  const ControllerHome({
    super.key,
    this.restore = true,
    required this.preferences,
  });
  final AppPreferences preferences;
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
  final history = TelemetryHistory();
  AppPreferences get preferences => widget.preferences;
  late final buzzerMirror = BuzzerMirror(
    failed: (error) {
      preferences.mirrorBuzzer = false;
      unawaited(preferences.save());
      if (mounted) setState(() => lastError = error);
    },
  );
  final captureKey = GlobalKey();
  late final automation = LocalAutomation(
    dispatch: _automationCall,
    snapshot: automationSnapshot,
  );
  final automationChannel = const MethodChannel(
    'ir.digitalogic.pccontroller/automation',
  );
  bool automationEnabled = false, automationActuation = false;
  String automationToken = '';
  bool _captureInFlight = false;
  DateTime? _lastCapture;
  int section = 0, boardTab = 0, playerTab = 0;
  bool busy = false;
  int activeRequests = 0;
  String? lastError, bridgeTarget;
  String boardOrigin = '', playerOrigin = '', boardToken = '', playerToken = '';
  bool viaPealayer = false,
      boardWanted = false,
      playerWanted = false,
      foreground = true;
  int connectionChoiceRevision = 0;
  List<Json> drafts = [];
  List<Json> deliveries = [];
  String displayTarget = 'segments';
  double toneHz = 880, toneMs = 115;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    board.addListener(_changed);
    preferences.addListener(_changed);
    board.onBuzzerNote = (event) => unawaited(buzzerMirror.event(event));
    if (widget.restore) {
      AppPreferences.native.setMethodCallHandler((call) async {
        if (call.method == 'launch' && mounted) {
          Navigator.of(context).popUntil((route) => route.isFirst);
          setState(() => section = call.arguments == 'pealayer' ? 1 : 0);
        }
      });
    }
    player.addListener(_changed);
    if (widget.restore) {
      automationChannel.setMethodCallHandler((call) async {
        if (call.method != 'request') throw MissingPluginException();
        final arguments = object(call.arguments);
        return automation.receive(
          object(jsonDecode('${arguments['rpc']}')),
          '${arguments['auth'] ?? ''}',
          intent: true,
        );
      });
    }
    if (widget.restore) unawaited(_restore());
  }

  void _changed() {
    history.record(board.state);
    buzzerMirror.configure(
      owner: board.identity,
      generation: board.state['connection_generation'],
      enabled:
          foreground &&
          preferences.mirrorBuzzer &&
          board.boardReady &&
          board.subscribed,
      volume: preferences.mirrorVolume,
    );
    if (mounted) setState(() {});
  }

  Future<void> _restore() async {
    try {
      await preferences.load();
      final launch = await AppPreferences.native.invokeMethod<String>(
        'initialSection',
      );
      if (launch == 'pealayer') section = 1;
      final prefs = await SharedPreferences.getInstance();
      boardOrigin =
          prefs.getString('pccontroller.origin') ??
          const String.fromEnvironment('PC_ORIGIN');
      playerOrigin = prefs.getString('pealayer.origin') ?? '';
      viaPealayer = prefs.getBool('pccontroller.via-pealayer') ?? false;
      boardWanted =
          prefs.getBool('pccontroller.connect') ?? boardOrigin.isNotEmpty;
      playerWanted =
          prefs.getBool('pealayer.connect') ?? playerOrigin.isNotEmpty;
      boardToken = await secure.read(key: 'pccontroller.token') ?? '';
      playerToken = await secure.read(key: 'pealayer.token') ?? '';
      drafts = objects(jsonDecode(prefs.getString('local.drafts') ?? '[]'));
      automationEnabled = prefs.getBool('automation.enabled') ?? false;
      automationActuation = prefs.getBool('automation.actuation') ?? false;
      if (automationEnabled) {
        automationToken =
            await secure.read(key: 'automation.token') ?? newAutomationToken();
        await secure.write(key: 'automation.token', value: automationToken);
        await _startAutomation();
      }
      if (!mounted) return;
      setState(() {});
      unawaited(connectBoard());
      if (foreground && playerWanted && playerOrigin.isNotEmpty) {
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
      foreground = false;
      unawaited(automation.close());
      board.disconnect();
      player.disconnect();
    } else if (state == AppLifecycleState.resumed && widget.restore) {
      foreground = true;
      if (automationEnabled && automationToken.isNotEmpty) {
        unawaited(_startAutomation());
      }
      unawaited(connectBoard());
      if (playerWanted && playerOrigin.isNotEmpty) {
        unawaited(player.connect(playerOrigin, playerToken));
      }
    }
  }

  @override
  void dispose() {
    unawaited(automation.close());
    if (widget.restore) automationChannel.setMethodCallHandler(null);
    WidgetsBinding.instance.removeObserver(this);
    preferences.removeListener(_changed);
    if (widget.restore) AppPreferences.native.setMethodCallHandler(null);
    board.onBuzzerNote = null;
    buzzerMirror.dispose();
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

  Future<void> _startAutomation() async {
    if (!foreground || !automationEnabled || automationToken.isEmpty) return;
    try {
      await automation.start(automationToken);
    } catch (_) {
      // An optional IPC bind failure must not break saved board/player routes.
      if (mounted) {
        setState(
          () => lastError =
              'Local API unavailable; check loopback ports 18990 and 18991.',
        );
      }
    }
    if (mounted) setState(() {});
  }

  Future<void> connectBoard() async {
    if (!foreground) return;
    final selected = selectedControllerOrigin(
      boardWanted,
      viaPealayer,
      boardOrigin,
      playerOrigin,
    );
    if (selected == null) return;
    if (viaPealayer) {
      await board.connectRelay(selected, playerToken);
    } else {
      await board.connect(selected, boardToken);
    }
  }

  Future<void> disconnectPeer(PeerSession peer) async {
    if (peer.controller) connectionChoiceRevision++;
    if (peer.controller) {
      boardWanted = false;
    } else {
      playerWanted = false;
    }
    peer.disconnect();
    await perform('Disconnect', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setBool(
        peer.controller ? 'pccontroller.connect' : 'pealayer.connect',
        false,
      );
    }, priority: true);
  }

  Future<bool> reacquireDocumentOwner(String pinned, int choice) async {
    final deadline = DateTime.now().add(const Duration(seconds: 6));
    while (mounted &&
        connectionChoiceRevision == choice &&
        boardWanted &&
        DateTime.now().isBefore(deadline)) {
      if (foreground &&
          board.fresh &&
          board.peripherals['board_profile'] is Map) {
        return board.stableTargetStamp == pinned;
      }
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    return false;
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
    if (['seat', 'side', 'motion'].contains(control['kind']) &&
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
          params['profile'] ??
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
          connectionChoiceRevision++;
          boardOrigin = address;
          boardToken = secret;
          viaPealayer = false;
          boardWanted = true;
          await prefs.setBool('pccontroller.via-pealayer', false);
          await prefs.setBool('pccontroller.connect', true);
          await connectBoard();
        } else {
          if (viaPealayer) connectionChoiceRevision++;
          playerOrigin = address;
          playerToken = secret;
          playerWanted = true;
          await prefs.setBool('pealayer.connect', true);
          await player.connect(address, secret);
          if (viaPealayer && boardWanted) await connectBoard();
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
    bool? danger,
  }) => OutlinedButton.icon(
    onPressed: busy && !priority ? null : callback,
    style: OutlinedButton.styleFrom(
      minimumSize: const Size(40, 42),
      foregroundColor: (danger ?? priority)
          ? Theme.of(context).colorScheme.error
          : Theme.of(context).colorScheme.onSurface,
      backgroundColor: (danger ?? priority)
          ? Theme.of(context).colorScheme.error.withValues(alpha: .08)
          : Colors.transparent,
      side: BorderSide(
        color: (danger ?? priority)
            ? Theme.of(context).colorScheme.error.withValues(alpha: .4)
            : Theme.of(context).colorScheme.outline,
      ),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
    ),
    icon: Icon(icon, size: 18),
    label: Text(label),
  );
  Widget card(
    String title,
    IconData icon,
    List<Widget> children, {
    Color? color,
    Widget? trailing,
  }) => Card(
    child: Padding(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 32,
                height: 32,
                decoration: BoxDecoration(
                  color: (color ?? Theme.of(context).colorScheme.primary)
                      .withValues(alpha: .10),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Icon(
                  icon,
                  color: color ?? Theme.of(context).colorScheme.primary,
                  size: 20,
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  title,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              ?trailing,
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
      Container(
        padding: const EdgeInsets.all(4),
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surfaceContainer,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: Theme.of(context).colorScheme.outlineVariant,
          ),
        ),
        child: SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: Row(
            children: [
              for (var i = 0; i < values.length; i++)
                Padding(
                  padding: const EdgeInsetsDirectional.only(end: 4),
                  child: TextButton(
                    onPressed: () => change(i),
                    style: TextButton.styleFrom(
                      backgroundColor: selected == i
                          ? Theme.of(context).colorScheme.surface
                          : null,
                      foregroundColor: selected == i
                          ? Theme.of(context).colorScheme.primary
                          : Theme.of(context).colorScheme.onSurfaceVariant,
                      minimumSize: const Size(44, 44),
                      padding: const EdgeInsets.symmetric(horizontal: 18),
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(8),
                      ),
                      side: selected == i
                          ? BorderSide(
                              color: Theme.of(context)
                                  .colorScheme
                                  .outlineVariant,
                            )
                          : BorderSide.none,
                      textStyle: Theme.of(context).textTheme.labelLarge
                          ?.copyWith(
                            fontWeight: selected == i
                                ? FontWeight.w700
                                : FontWeight.w500,
                          ),
                    ),
                    child: Semantics(
                      selected: selected == i,
                      child: Text(values[i]),
                    ),
                  ),
                ),
            ],
          ),
        ),
      );
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, bounds) {
      final tablet =
          preferences.layout == 'tablet' && bounds.maxWidth >= 600 ||
          preferences.layout == 'auto' && bounds.maxWidth >= 760;
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
      return RepaintBoundary(
        key: captureKey,
        child: Scaffold(
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
                tooltip: 'Settings',
                icon: const Icon(Icons.settings_outlined),
                onPressed: openSettings,
              ),
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
        ),
      );
    },
  );
  Widget connection(PeerSession peer, String name) => card(
    name,
    Icons.lan_outlined,
    [
      if (!peer.fresh) const Text('Offline · last values unavailable'),
      if (peer.error != null)
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Text(peer.error!),
        ),
      if (!peer.fresh || peer.error != null) const SizedBox(height: 12),
      Wrap(
        spacing: 8,
        runSpacing: 8,
        children: [
          action(
            peer.fresh ? 'Connection' : 'Connect',
            Icons.link,
            () => configure(peer.controller),
          ),
          IconButton.outlined(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh, size: 20),
            onPressed: peer.transport == null
                ? null
                : () => unawaited(peer.refresh()),
            style: IconButton.styleFrom(
              fixedSize: const Size(42, 42),
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(8),
              ),
            ),
          ),
          IconButton.outlined(
            tooltip: 'Disconnect',
            icon: const Icon(Icons.link_off, size: 20),
            onPressed: peer.enabled
                ? () => unawaited(disconnectPeer(peer))
                : null,
            style: IconButton.styleFrom(
              fixedSize: const Size(42, 42),
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(8),
              ),
            ),
          ),
        ],
      ),
    ],
    trailing: StatePill(
      peer.fresh
          ? peer.controller
                ? peer.state['connected'] == true
                      ? 'Connected'
                      : 'Host only'
                : 'Connected'
          : 'Offline',
      active:
          peer.fresh && (!peer.controller || peer.state['connected'] == true),
    ),
  );
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
        : '—';
    String peripheral(String key, String fallback) {
      for (final entry in objects(board.peripherals['peripherals'])) {
        if (entry['key'] == key && entry['name'] is String) {
          return entry['name'];
        }
      }
      return fallback;
    }

    Widget metric(
      String title,
      IconData icon,
      String value,
      String field,
      String unit,
      Color color,
    ) => card(title, icon, [
      Text(
        value,
        style: Theme.of(context).textTheme.headlineSmall?.copyWith(
          fontFeatures: const [FontFeature.tabularFigures()],
          fontWeight: FontWeight.w600,
        ),
      ),
      const SizedBox(height: 12),
      TelemetryChart(
        points: history.points,
        field: field,
        color: color,
        unit: unit,
        height: 64,
      ),
    ], color: color);
    return grid([
      metric(
        peripheral('sensor.bus-voltage', 'Voltage'),
        Icons.bolt,
        measurement('bus_mv', 1000, 'V', 'ina219_available'),
        'voltage',
        'V',
        const Color(0xff9786ed),
      ),
      metric(
        peripheral('sensor.current', 'Current'),
        Icons.electric_meter,
        measurement('current_ma', 1, 'mA', 'ina219_available'),
        'current',
        'mA',
        const Color(0xff62bcae),
      ),
      metric(
        peripheral('sensor.power', 'Power'),
        Icons.power,
        measurement('power_mw', 1000, 'W', 'ina219_available'),
        'power',
        'W',
        const Color(0xffe0ac62),
      ),
      card('Temperature', Icons.thermostat, [
        fact(
          peripheral('sensor.temperature-led', 'Lighting'),
          measurement(
            'temperature_led_centi_c',
            100,
            '°C',
            'temperature_led_available',
          ),
        ),
        const SizedBox(height: 8),
        TelemetryChart(
          points: history.points,
          field: 'temperature',
          color: const Color(0xffe68b85),
          unit: '°C',
          height: 64,
        ),
        const SizedBox(height: 12),
        fact(
          peripheral('sensor.temperature-audio', 'Audio'),
          measurement(
            'temperature_bt_audio_centi_c',
            100,
            '°C',
            'temperature_bt_audio_available',
          ),
        ),
        TelemetryChart(
          points: history.points,
          field: 'audio',
          color: const Color(0xff62bcae),
          unit: '°C',
          height: 40,
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
              ? '${board.state['have_board_name'] == true && '${object(board.state['board_name'])['name'] ?? ''}'.trim().isNotEmpty ? object(board.state['board_name'])['name'] : object(board.state['hello'])['name'] ?? '—'}'
              : 'Unavailable',
        ),
        fact(
          'Mode',
          board.measurementsFresh
              ? '${status['program_mode'] ?? 'Unavailable'}'
              : 'Unavailable',
        ),
        fact(
          'Door',
          board.measurementsFresh && status['door_open'] is bool
              ? status['door_open'] == true
                    ? 'Open'
                    : 'Closed'
              : 'Unavailable',
        ),
        fact(
          'E-STOP',
          board.fresh && object(board.state['emergency_stop'])['active'] is bool
              ? object(board.state['emergency_stop'])['active'] == true
                    ? 'Active'
                    : 'Released'
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
      if (board.fresh && board.state['have_status_led'] == true)
        card('Status LED', Icons.lightbulb_outline, [
          Row(
            children: [
              Container(
                width: 28,
                height: 28,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: Color.fromARGB(
                    255,
                    object(board.state['status_led'])['red'] as int? ?? 0,
                    object(board.state['status_led'])['green'] as int? ?? 0,
                    object(board.state['status_led'])['blue'] as int? ?? 0,
                  ),
                  border: Border.all(
                    color: Theme.of(context).colorScheme.outline,
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Text(
                'RGB ${object(board.state['status_led'])['red']} · ${object(board.state['status_led'])['green']} · ${object(board.state['status_led'])['blue']}',
              ),
            ],
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
          .putIfAbsent(
            '${control['group'] ?? ''}'.trim().isEmpty
                ? (control['kind'] == 'relay'
                      ? 'Relay outputs'
                      : control['kind'] == 'pwm'
                      ? 'PWM outputs'
                      : 'Motion / seat controls')
                : '${control['group']}',
            () => [],
          )
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
                      channelIcon(control),
                      [
                        if (control['control'] == 'pwm-user')
                          _PwmControl(
                            key: ValueKey(
                              '${board.identity}/${control['key']}',
                            ),
                            control: control,
                            observed: observeChannel(
                              control,
                              board.state,
                              board.pwmUpdated != null &&
                                      DateTime.now()
                                              .difference(board.pwmUpdated!)
                                              .inMilliseconds <
                                          board.freshnessMs
                                  ? board.pwmValues
                                  : {},
                              board.measurementsFresh,
                            ).percent,
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
                                danger: entry['verb'] == 'stop',
                              ),
                          ],
                        ),
                      ],
                      color: channelColor(control['color']),
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Builder(
                            builder: (context) {
                              final observed = channelObservation(control);
                              return StatePill(
                                observed.label,
                                active: observed.active,
                              );
                            },
                          ),
                          channelMenu(control),
                        ],
                      ),
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
    final full = ready && board.state['have_front_panel'] == true;
    final lcdAvailable = full && panel['lcd_available'] == true;
    final validDisplay =
        displayTarget == 'segments' || (displayTarget == 'lcd' && lcdAvailable);
    return grid([
      card('Live front panel', Icons.dialpad, [
        if (ready &&
            (full || board.state['have_front_panel_segments'] == true) &&
            panel['raw_segments'] is List &&
            (panel['raw_segments'] as List).length == 4 &&
            (panel['raw_segments'] as List).every(
              (v) => v is int && v >= 0 && v <= 255,
            ))
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
          ready && panel['tm1637_detection_known'] == true
              ? panel['tm1637_detected'] == true
                    ? 'Detected'
                    : 'Not detected'
              : 'Unknown',
        ),
        if (lcdAvailable)
          Text(
            '${panel['lcd_line_1']}\n${panel['lcd_line_2']}',
            style: const TextStyle(fontFamily: 'monospace'),
          ),
        fact(
          'Pressed keys',
          full
              ? '${panel['pressed_keys'] ?? 'Unavailable'}'
              : board.measurementsFresh &&
                    object(board.state['status'])['active_keys'] is int
              ? '${object(board.state['status'])['active_keys']}'
              : 'Unavailable',
        ),
        fact(
          'Page',
          full
              ? '${panel['menu_page'] ?? 'Unavailable'}'
              : board.measurementsFresh &&
                    object(board.state['status'])['menu_page'] is int
              ? '${object(board.state['status'])['menu_page']}'
              : 'Unavailable',
        ),
        const SizedBox(height: 12),
        FrontPanelButtons(
          captured: full && panel['host_captured'] == true,
          enabled: full && !busy,
          pressedKeys: full && panel['pressed_keys'] is int
              ? panel['pressed_keys'] as int
              : board.measurementsFresh
              ? object(board.state['status'])['active_keys'] as int?
              : null,
          dispatch: (command) => unawaited(boardCommand(command)),
        ),
      ]),
      card('Display message', Icons.text_fields, [
        DropdownButtonFormField<String>(
          key: ValueKey('${board.targetStamp}/$lcdAvailable/$displayTarget'),
          initialValue: validDisplay ? displayTarget : null,
          decoration: const InputDecoration(labelText: 'Display'),
          items: [
            const DropdownMenuItem(value: 'segments', child: Text('7-segment')),
            if (lcdAvailable)
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
          ready && validDisplay && displayText.text.trim().isNotEmpty
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
      ]),
      card(
        'Host mode',
        Icons.tune,
        [
          SegmentedButton<String>(
            emptySelectionAllowed: true,
            segments: const [
              ButtonSegment(
                value: 'idle',
                label: Text('Idle'),
                icon: Icon(Icons.pause_circle_outline),
              ),
              ButtonSegment(
                value: 'running',
                label: Text('Running'),
                icon: Icon(Icons.play_circle_outline),
              ),
            ],
            selected:
                board.fresh &&
                    [
                      'Idle',
                      'Running',
                    ].contains(object(board.state['program_state'])['mode'])
                ? {
                    '${object(board.state['program_state'])['mode']}'
                        .toLowerCase(),
                  }
                : {},
            onSelectionChanged: ready && !busy
                ? (values) {
                    if (values.isEmpty) return;
                    final stamp = board.targetStamp, peer = board.transport;
                    unawaited(
                      perform('Host mode', () async {
                        if (peer == null ||
                            stamp != board.targetStamp ||
                            !board.boardReady) {
                          throw const Rejected(
                            'Board changed; reopen mode controls.',
                          );
                        }
                        final result = await peer.rpc(
                          'controller.program_state.set',
                          {'mode': values.single, 'owner': 'mobile'},
                        );
                        await board.refresh();
                        return result;
                      }),
                    );
                  }
                : null,
          ),
        ],
        trailing: StatePill(
          board.fresh
              ? '${object(board.state['program_state'])['mode'] ?? 'Unknown'}'
              : 'Unknown',
          active:
              board.fresh &&
                  [
                    'Idle',
                    'Running',
                  ].contains(object(board.state['program_state'])['mode'])
              ? object(board.state['program_state'])['mode'] == 'Running'
              : null,
        ),
      ),
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
                        final pinned = board.stableTargetStamp,
                            choice = connectionChoiceRevision;
                        final document = await AndroidDocuments().pick(
                          extensions: const ['hex', 'bin'],
                        );
                        if (document == null) {
                          return null;
                        }
                        if (!await reacquireDocumentOwner(pinned, choice)) {
                          throw const Rejected(
                            'Selected board ownership could not be reverified; firmware was not staged.',
                          );
                        }
                        final result = await board.transport!.upload(
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
                            final pinned = board.stableTargetStamp,
                                choice = connectionChoiceRevision,
                                peer = board.transport!;
                            final bytes = await peer.download(artifact);
                            return AndroidDocuments().export(
                              'pccontroller-${artifact['sha256'].toString().substring(0, 12)}.bin',
                              bytes,
                              stillOwned: () =>
                                  connectionChoiceRevision == choice &&
                                  board.stableTargetStamp == pinned &&
                                  board.fresh,
                              reacquire: () =>
                                  reacquireDocumentOwner(pinned, choice),
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
        Text(operationLabel(operation)),
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
  bool playerCan(String action) {
    if (!playerReady) return false;
    if (player.fresh) {
      return object(
            player.state['media_session_capabilities'],
          )['can_$action'] ==
          true;
    }
    final selected = board.instances
        .where((v) => v['id'] == bridgeTarget)
        .firstOrNull;
    return '${object(selected?['values'])['app_actions']}'
        .split(',')
        .map((v) => v.trim())
        .contains('pealayer.$action');
  }

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
        Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            for (final entry in [
              ('previous', Icons.skip_previous),
              (
                player.fresh && state['playing'] == true ? 'pause' : 'play',
                player.fresh && state['playing'] == true
                    ? Icons.pause
                    : Icons.play_arrow,
              ),
              ('stop', Icons.stop),
              ('next', Icons.skip_next),
            ])
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4),
                child: IconButton.filledTonal(
                  tooltip: entry.$1[0].toUpperCase() + entry.$1.substring(1),
                  icon: Icon(
                    entry.$2,
                    size: entry.$1 == 'play' || entry.$1 == 'pause' ? 30 : 24,
                  ),
                  style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
                  onPressed: playerCan(entry.$1)
                      ? () => unawaited(playerCommand('pealayer.${entry.$1}'))
                      : null,
                ),
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
          RemoteSlider(
            key: ValueKey('${player.targetStamp}/seek'),
            value: (state['playback_time'] as num).toDouble(),
            maximum: (state['duration'] as num).toDouble(),
            enabled: !busy,
            label: (v) => '${v.toStringAsFixed(1)} s',
            changed: (v) =>
                unawaited(playerCommand('pealayer.seek_to', {'seconds': v})),
          ),
        ],
        if (player.fresh && state['volume'] is num) ...[
          fact('Volume', '${(state['volume'] as num).round()}%'),
          RemoteSlider(
            key: ValueKey('${player.targetStamp}/volume'),
            value: (state['volume'] as num).toDouble(),
            maximum: 130,
            enabled: !busy,
            label: (v) => '${v.round()}%',
            changed: (v) =>
                unawaited(playerCommand('pealayer.volume.set', {'value': v})),
          ),
        ],
        action(
          player.fresh && state['muted'] == true ? 'Unmute' : 'Mute',
          player.fresh && state['muted'] == true
              ? Icons.volume_off_outlined
              : Icons.volume_up_outlined,
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
        if (player.fresh &&
            objects(state['workspace_profiles']).isNotEmpty) ...[
          const SizedBox(height: 16),
          DropdownButtonFormField<String>(
            key: ValueKey(
              '${player.targetStamp}/${state['active_workspace_profile']}',
            ),
            initialValue:
                objects(state['workspace_profiles'])
                    .any((v) => v['id'] == state['active_workspace_profile'])
                ? state['active_workspace_profile'] as String
                : null,
            decoration: const InputDecoration(labelText: 'Workspace'),
            items: [
              for (final profile in objects(state['workspace_profiles']))
                DropdownMenuItem(
                  value: '${profile['id']}',
                  child: Text(
                    '${profile['name']}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
            onChanged: !busy
                ? (v) {
                    if (v != null) {
                      unawaited(
                        playerCommand('pealayer.workspace.set', {'profile': v}),
                      );
                    }
                  }
                : null,
          ),
        ],
      ], color: const Color(0xff79bc63)),
      if (player.fresh && state['playback_rate'] is num)
        card('Playback options', Icons.tune, [
          DropdownButtonFormField<double>(
            key: ValueKey('${player.targetStamp}/${state['playback_rate']}'),
            initialValue: (state['playback_rate'] as num).toDouble(),
            decoration: const InputDecoration(labelText: 'Speed'),
            items: [
              for (final rate in ({
                .25,
                .5,
                .75,
                1.0,
                1.25,
                1.5,
                2.0,
                (state['playback_rate'] as num).toDouble(),
              }.toList()..sort()))
                DropdownMenuItem(
                  value: rate,
                  child: Text('${rate.toStringAsFixed(2)}×'),
                ),
            ],
            onChanged: !busy
                ? (v) {
                    if (v != null) {
                      unawaited(
                        playerCommand('pealayer.rate.set', {'rate': v}),
                      );
                    }
                  }
                : null,
          ),
          if (objects(state['chapters']).isNotEmpty) ...[
            const SizedBox(height: 16),
            DropdownButtonFormField<int>(
              key: ValueKey(
                '${player.targetStamp}/${state['current_chapter_index']}',
              ),
              initialValue:
                  objects(state['chapters'])
                      .any((v) => v['index'] == state['current_chapter_index'])
                  ? state['current_chapter_index'] as int
                  : null,
              decoration: const InputDecoration(labelText: 'Chapter'),
              items: [
                for (final chapter in objects(state['chapters']))
                  DropdownMenuItem(
                    value: chapter['index'] as int,
                    child: Text(
                      '${chapter['title']}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
              ],
              onChanged: !busy
                  ? (v) {
                      if (v != null) {
                        unawaited(
                          playerCommand('pealayer.chapter.set', {'index': v}),
                        );
                      }
                    }
                  : null,
            ),
          ],
          for (final kind in ['audio', 'sub'])
            if (objects(state['media_tracks'])
                .any((v) => v['kind'] == kind && v['id'] is int)) ...[
              const SizedBox(height: 16),
              DropdownButtonFormField<int>(
                key: ValueKey(
                  '${player.targetStamp}/$kind/${objects(state['media_tracks']).where((v) => v['kind'] == kind && v['selected'] == true).map((v) => v['id']).join()}',
                ),
                initialValue:
                    objects(state['media_tracks'])
                            .where(
                              (v) =>
                                  v['kind'] == kind &&
                                  v['selected'] == true &&
                                  v['id'] is int,
                            )
                            .firstOrNull?['id']
                        as int? ??
                    -1,
                isExpanded: true,
                decoration: InputDecoration(
                  labelText: kind == 'audio' ? 'Audio track' : 'Subtitles',
                ),
                items: [
                  const DropdownMenuItem(value: -1, child: Text('Off')),
                  for (final track in objects(
                    state['media_tracks'],
                  ).where((v) => v['kind'] == kind && v['id'] is int))
                    DropdownMenuItem(
                      value: track['id'] as int,
                      child: Text(
                        '${track['title'] ?? track['language'] ?? track['codec'] ?? track['id']}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                ],
                onChanged: !busy
                    ? (v) {
                        if (v != null) {
                          unawaited(
                            playerCommand(
                              v == -1
                                  ? 'pealayer.media.track.disable'
                                  : 'pealayer.media.track.select',
                              {'kind': kind, if (v != -1) 'id': v},
                            ),
                          );
                        }
                      }
                    : null,
              ),
            ],
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
        subtitle: Text('${cue['start_time_ms']} ms'),
        onTap: player.fresh && !busy
            ? () => unawaited(editPlayerCue(cue))
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
  Future<void> editPlayerCue(Json cue) async {
    if (!player.fresh ||
        busy ||
        cue['id'] is! String ||
        cue['start_time_ms'] is! int ||
        cue['duration_ms'] is! int) {
      return;
    }
    final owner = player.targetStamp, peer = player.transport;
    final fingerprint = jsonEncode(cue);
    await showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (_) => CueEditor(
        cue: cue,
        save: (start, length) async {
          final current = objects(player.state['cues'])
              .where((v) => v['id'] == cue['id'])
              .firstOrNull;
          if (!player.fresh ||
              player.targetStamp != owner ||
              peer == null ||
              current == null ||
              jsonEncode(current) != fingerprint) {
            throw const Rejected(
              'Cue or player changed. Reopen the cue to edit its current values.',
            );
          }
          if (length != cue['duration_ms'] && current['resizable'] != true) {
            throw const Rejected('This cue has an intrinsic duration.');
          }
          setState(() {
            activeRequests++;
            busy = true;
          });
          try {
            await peer.rpc(
              length == cue['duration_ms']
                  ? 'timeline.cue.place'
                  : 'effect_cue.update',
              {
                'instance_id': cue['id'],
                'start_time_ms': start,
                if (length != cue['duration_ms']) 'duration_ms': length,
              },
            );
          } finally {
            if (mounted) {
              setState(() {
                activeRequests--;
                busy = activeRequests > 0;
              });
            }
          }
        },
      ),
    );
  }

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
                connectionChoiceRevision++;
                final prefs = await SharedPreferences.getInstance();
                viaPealayer = true;
                boardWanted = true;
                await prefs.setBool('pccontroller.via-pealayer', true);
                await prefs.setBool('pccontroller.connect', true);
                await connectBoard();
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
      const SizedBox(height: 16),
      automationSettings(),
    ],
  );
  Widget automationSettings() =>
      card('Automation / local API', Icons.hub_outlined, [
        SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('Enable local automation'),
          value: automationEnabled,
          onChanged: (value) => unawaited(_setAutomation(value)),
        ),
        if (automationEnabled) ...[
          fact('HTTP / WebSocket', '127.0.0.1:${LocalAutomation.httpPort}'),
          fact('NDJSON / TCP', '127.0.0.1:${LocalAutomation.tcpPort}'),
          StatePill(
            automation.running ? 'Listening' : 'Paused',
            active: automation.running,
          ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Allow output / playback actions'),
            value: automationActuation,
            onChanged: (value) async {
              if (value &&
                  !await confirm(
                    'Allow automation actions?',
                    'Authorized local clients can operate the selected board and player. Motion also requires an explicit confirmed target.',
                  )) {
                return;
              }
              final prefs = await SharedPreferences.getInstance();
              await prefs.setBool('automation.actuation', value);
              if (mounted) {
                setState(() => automationActuation = value);
                preferences.refresh();
              }
            },
          ),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              action(
                'Copy access token',
                Icons.key,
                () => Clipboard.setData(ClipboardData(text: automationToken)),
              ),
              action(
                'Revoke token',
                Icons.refresh,
                () => unawaited(_setAutomation(true, rotate: true)),
              ),
            ],
          ),
        ],
      ]);

  void openSettings() {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => AnimatedBuilder(
          animation: Listenable.merge([preferences, board, player]),
          builder: (context, _) => AppSettings(
            preferences: preferences,
            changed: _changed,
            connections: card('Connections', Icons.link, [
              action(
                'PCController connection',
                Icons.memory_outlined,
                () => configure(true),
              ),
              const SizedBox(height: 8),
              action(
                'Pealayer connection',
                Icons.play_circle_outline,
                () => configure(false),
              ),
            ]),
            automation: automationSettings(),
          ),
        ),
      ),
    );
  }

  ChannelObservation channelObservation(Json control) => observeChannel(
    control,
    board.state,
    board.pwmUpdated != null &&
            DateTime.now().difference(board.pwmUpdated!).inMilliseconds <
                board.freshnessMs
        ? board.pwmValues
        : {},
    board.measurementsFresh,
  );

  Widget channelMenu(Json control) {
    final profile = object(board.peripherals['board_profile']);
    final editable =
        board.fresh &&
        !busy &&
        profile['attached'] == true &&
        profile['configured'] == true;
    return PopupMenuButton<String>(
      tooltip: 'Manage ${control['name']}',
      icon: const Icon(Icons.more_vert, size: 20),
      onSelected: (value) => unawaited(editChannel(control, value)),
      itemBuilder: (_) => [
        PopupMenuItem(
          value: 'edit',
          enabled: editable,
          child: const Text('Edit channel'),
        ),
        PopupMenuItem(
          value: 'lock',
          enabled: editable,
          child: Text(
            control['locked'] == true ? 'Unlock channel' : 'Lock channel',
          ),
        ),
      ],
    );
  }

  Future<void> editChannel(Json control, String operation) async {
    final stamp = board.targetStamp, transport = board.transport;
    final revision = object(board.peripherals['board_profile'])['revision'];
    Json? changes;
    if (operation == 'lock') {
      changes = {'locked': control['locked'] != true};
    } else {
      changes = await showDialog<Json>(
        context: context,
        builder: (_) => ChannelEditor(control: control),
      );
    }
    if (changes == null || !mounted) return;
    await perform('Channel presentation', () async {
      if (!board.fresh || board.targetStamp != stamp || transport == null) {
        throw const Rejected('Channel owner changed. Reopen the channel menu.');
      }
      await transport.rpc('controller.peripheral.presentation.update', {
        'key': control['key'],
        'expected_revision': revision,
        ...changes!,
      });
      await board.refresh();
      return null;
    });
  }

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
  Future<void> _setAutomation(bool enabled, {bool rotate = false}) async {
    await perform('Automation', () async {
      await automation.close();
      if (enabled) {
        automationToken = rotate
            ? newAutomationToken()
            : await secure.read(key: 'automation.token') ??
                  newAutomationToken();
        await secure.write(key: 'automation.token', value: automationToken);
        if (foreground) await automation.start(automationToken);
      }
      automationEnabled = enabled;
      preferences.refresh();
      final prefs = await SharedPreferences.getInstance();
      await prefs.setBool('automation.enabled', enabled);
      return null;
    });
  }

  Json automationSnapshot() => {
    'foreground': foreground,
    'section': ['pccontroller', 'pealayer', 'activities', 'workspace'][section],
    'board': {
      'connected': board.boardReady,
      'target': board.targetStamp,
      'measurements_fresh': board.measurementsFresh,
      'status': board.measurementsFresh ? board.state['status'] : null,
      'front_panel': board.fresh ? board.state['front_panel'] : null,
      'pwm':
          board.fresh &&
              board.pwmUpdated != null &&
              DateTime.now().difference(board.pwmUpdated!).inMilliseconds <
                  board.freshnessMs
          ? board.pwmValues
          : null,
    },
    'player': {
      'connected': player.fresh,
      'target': player.targetStamp,
      for (final key in [
        'playing',
        'playback_time',
        'duration',
        'volume',
        'seekable',
      ])
        key: player.fresh ? player.state[key] : null,
    },
  };
  Future<dynamic> _automationCall(String method, Json params) async {
    switch (method) {
      case 'app.ping':
        return {'ok': true, 'app': 'PCController', 'version': '0.1.3'};
      case 'app.capabilities':
        return {
          'jsonrpc': '2.0',
          'http': true,
          'websocket': true,
          'ndjson_tcp': true,
          'android_intent': defaultTargetPlatform == TargetPlatform.android,
          'grpc': false,
          'foreground_required': true,
          'actuation_enabled': automationActuation,
          'batch': false,
          'notifications_dispatch': false,
          'board_methods': [
            'board.refresh',
            'board.connect',
            'board.disconnect',
            'board.action',
            'board.pwm.set',
            'board.stop',
            'board.estop',
            'board.menu.press',
            'board.display.show',
            'board.buzzer.play',
            'board.buzzer.stop',
            'board.mode.set',
          ],
        };
      case 'app.state':
        return automationSnapshot();
      case 'app.subscribe':
        return {
          'subscribed': true,
          'events_transport': 'websocket',
          'interval_ms': 250,
        };
      case 'app.navigation.set':
        final next = [
          'pccontroller',
          'pealayer',
          'activities',
          'workspace',
        ].indexOf('${params['section']}');
        if (next < 0) throw const AutomationFailure(-32602, 'Unknown section');
        if (!foreground) {
          throw const AutomationFailure(-32002, 'App is not foreground');
        }
        final page = params['page'];
        final boardPage = [
          'monitor',
          'outputs',
          'front_panel',
          'firmware',
          'effects',
        ].indexOf('$page');
        if (page != null && (next != 0 || boardPage < 0)) {
          throw const AutomationFailure(-32602, 'Unknown section page');
        }
        setState(() {
          section = next;
          if (page != null) boardTab = boardPage;
        });
        return {'section': params['section'], 'page': ?page};
      case 'app.capture':
        if (!foreground) {
          throw const AutomationFailure(-32002, 'App is not foreground');
        }
        if (_captureInFlight ||
            (_lastCapture != null &&
                DateTime.now().difference(_lastCapture!).inMilliseconds <
                    1000)) {
          throw const AutomationFailure(
            -32003,
            'Capture is busy; limited to one per second',
          );
        }
        _captureInFlight = true;
        try {
          await WidgetsBinding.instance.endOfFrame;
          final boundary = captureKey.currentContext?.findRenderObject();
          if (boundary is! RenderRepaintBoundary) {
            throw const AutomationFailure(
              -32002,
              'No rendered application frame',
            );
          }
          final image = await boundary.toImage(pixelRatio: 1.5);
          try {
            final bytes = await image.toByteData(
              format: ui.ImageByteFormat.png,
            );
            if (bytes == null || bytes.lengthInBytes > 2097152) {
              throw const AutomationFailure(-32003, 'Capture exceeds 2 MiB');
            }
            return {
              'mime_type': 'image/png',
              'png_base64': base64Encode(bytes.buffer.asUint8List()),
              'width': image.width,
              'height': image.height,
            };
          } finally {
            image.dispose();
          }
        } finally {
          _lastCapture = DateTime.now();
          _captureInFlight = false;
        }
      case 'board.refresh':
        await board.refresh();
        return automationSnapshot()['board'];
      case 'board.connect':
        if (!foreground) {
          throw const AutomationFailure(-32002, 'App is not foreground');
        }
        boardWanted = true;
        connectionChoiceRevision++;
        await connectBoard();
        return automationSnapshot()['board'];
      case 'board.disconnect':
        await disconnectPeer(board);
        return {'disconnected': true};
      case 'board.action':
      case 'board.pwm.set':
      case 'board.stop':
      case 'board.estop':
      case 'board.menu.press':
      case 'board.display.show':
      case 'board.buzzer.play':
      case 'board.buzzer.stop':
      case 'board.mode.set':
        if (!foreground ||
            !board.boardReady ||
            params['expected_target'] != board.targetStamp) {
          throw const AutomationFailure(
            -32002,
            'Board is unavailable or target changed',
          );
        }
        final peer = board.transport!,
            stop =
                method == 'board.stop' ||
                method == 'board.estop' ||
                method == 'board.buzzer.stop';
        if (busy && !stop) {
          throw const AutomationFailure(-32003, 'Application is busy');
        }
        if (method == 'board.estop') {
          return peer.rpc('controller.command.execute', {
            'command': 'estop on',
          });
        }
        if (method == 'board.stop') {
          final stamp = board.targetStamp;
          await peer.rpc('controller.command.execute', {
            'command': 'relay off',
          });
          if (stamp != board.targetStamp || !board.boardReady) {
            throw const AutomationFailure(
              -32002,
              'Target changed during stop; refresh to check outcome',
            );
          }
          return peer.rpc('controller.pwm.off');
        }
        if (method == 'board.buzzer.stop') {
          buzzerMirror.stop();
          return peer.rpc('controller.command.execute', {
            'command': 'melody stop',
          });
        }
        if (!automationActuation ||
            object(board.state['emergency_stop'])['active'] == true) {
          throw const AutomationFailure(
            -32004,
            'Automation actuation is not enabled or E-STOP is active',
          );
        }
        if (method == 'board.menu.press') {
          if (board.state['have_front_panel'] != true) {
            throw const AutomationFailure(
              -32002,
              'Front-panel mode is not yet available',
            );
          }
          final button = params['button'];
          final panel = object(board.state['front_panel']);
          final captured =
              board.state['have_front_panel'] == true &&
              panel['host_captured'] == true;
          final allowed = captured
              ? ['K1', 'K2', 'K3', 'K4']
              : ['prev', 'next', 'dec', 'inc'];
          if (!allowed.contains(button)) {
            throw const AutomationFailure(
              -32602,
              'Button is not available in the current panel mode',
            );
          }
          return peer.rpc('controller.command.execute', {
            'command': captured
                ? 'host-menu key $button press'
                : 'menu $button',
          });
        }
        if (method == 'board.display.show') {
          final target = params['display'], text = params['text'];
          final seconds = params['duration_seconds'] ?? 5;
          if (!['segments', 'lcd'].contains(target) ||
              text is! String ||
              text.trim().isEmpty ||
              text.length > 240 ||
              seconds is! int ||
              seconds < 1 ||
              seconds > 30 ||
              (target == 'lcd' &&
                  (board.state['have_front_panel'] != true ||
                      object(board.state['front_panel'])['lcd_available'] !=
                          true))) {
            throw const AutomationFailure(
              -32602,
              'Display or bounded message is invalid',
            );
          }
          return peer.rpc('controller.command.execute', {
            'command':
                'display $target --duration ${seconds}s --repeat once -- ${jsonEncode(text)}',
          });
        }
        if (method == 'board.buzzer.play') {
          final hz = params['frequency_hz'], ms = params['duration_ms'];
          if (hz is! int ||
              hz < 20 ||
              hz > 20000 ||
              ms is! int ||
              ms < 1 ||
              ms > 1000) {
            throw const AutomationFailure(
              -32602,
              'Tone requires 20..20000 Hz and 1..1000 ms',
            );
          }
          return peer.rpc('controller.command.execute', {
            'command': 'buzzer $hz $ms',
          });
        }
        if (method == 'board.mode.set') {
          if (!['idle', 'running'].contains(params['mode'])) {
            throw const AutomationFailure(-32602, 'Unknown mode');
          }
          return peer.rpc('controller.program_state.set', {
            'mode': params['mode'],
            'owner': 'mobile-automation',
          });
        }
        if (method == 'board.pwm.set') {
          final channel = params['channel'], percent = params['percent'];
          if (channel is! int ||
              percent is! num ||
              !percent.isFinite ||
              percent < 0 ||
              percent > 100 ||
              !objects(board.peripherals['controls']).any(
                (c) =>
                    c['key'] == 'pwm.$channel' &&
                    c['control'] == 'pwm-user' &&
                    c['locked'] != true,
              )) {
            throw const AutomationFailure(
              -32602,
              'PWM target/value is not advertised',
            );
          }
          return peer.rpc('controller.pwm.set', {
            'channel': channel,
            'percent': percent,
          });
        }
        for (final control in objects(board.peripherals['controls'])) {
          for (final action in objects(control['actions'])) {
            if (action['id'] != params['action_id']) continue;
            if (control['locked'] == true) {
              throw const AutomationFailure(-32004, 'Channel is locked');
            }
            if (['seat', 'side', 'motion'].contains(control['kind']) &&
                action['verb'] != 'stop' &&
                params['confirm_motion'] != true) {
              throw const AutomationFailure(
                -32004,
                'Motion requires an explicit confirmed target',
              );
            }
            return peer.rpc('controller.action.invoke', {
              'action_id': action['id'],
            });
          }
        }
        throw const AutomationFailure(
          -32602,
          'Action is not advertised by this board',
        );
      case 'player.call':
        if (!foreground ||
            !player.fresh ||
            params['expected_target'] != player.targetStamp ||
            !automationActuation ||
            busy) {
          throw const AutomationFailure(
            -32002,
            'Player is unavailable, target changed or actuation disabled',
          );
        }
        final action = params['method'];
        if (![
          'pealayer.play',
          'pealayer.pause',
          'pealayer.stop',
          'pealayer.previous',
          'pealayer.next',
          'pealayer.seek_to',
          'pealayer.volume.set',
          'pealayer.mute.toggle',
          'pealayer.workspace.set',
        ].contains(action)) {
          throw const AutomationFailure(-32602, 'Player method is not allowed');
        }
        return player.transport!.rpc(action, object(params['params']));
      default:
        throw const AutomationFailure(-32601, 'Method not found');
    }
  }

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
    super.key,
    required this.control,
    required this.enabled,
    required this.changed,
    this.observed,
  });
  final Json control;
  final bool enabled;
  final ValueChanged<int> changed;
  final double? observed;
  @override
  State<_PwmControl> createState() => _PwmControlState();
}

class _PwmControlState extends State<_PwmControl> {
  double value = 0;
  bool editing = false;
  @override
  void initState() {
    super.initState();
    value = widget.observed ?? 0;
  }

  @override
  void didUpdateWidget(covariant _PwmControl oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!editing && widget.observed != null) value = widget.observed!;
  }

  @override
  Widget build(BuildContext context) => Column(
    children: [
      Text(
        'Setpoint ${value.round()}%',
        style: TextStyle(
          color: Theme.of(context).colorScheme.onSurfaceVariant,
          fontSize: 12,
        ),
      ),
      Slider(
        value: value,
        max: 100,
        onChanged: widget.enabled
            ? (next) => setState(() {
                editing = true;
                value = next;
              })
            : null,
        onChangeEnd: widget.enabled
            ? (next) {
                editing = false;
                widget.changed(next.round());
              }
            : null,
      ),
    ],
  );
}
