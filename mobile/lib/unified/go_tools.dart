import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'controller_dialog.dart';
import 'endpoint_dialog.dart' show AddressCharacterFormatter;
import 'go_commands.dart';
import 'preferences.dart';
import 'transport.dart';
import 'pixel_diagnostic.dart';

class GoToolsPage extends StatefulWidget {
  const GoToolsPage({
    super.key,
    required this.session,
    required this.preferences,
  });
  final PeerSession session;
  final AppPreferences preferences;
  @override
  State<GoToolsPage> createState() => _GoToolsPageState();
}

class _GoToolsPageState extends State<GoToolsPage> {
  late final client = GoCommandClient(widget.session);
  final search = TextEditingController(), command = TextEditingController();
  final color = TextEditingController(), frame = TextEditingController();
  final name = TextEditingController(), recordName = TextEditingController();
  List<Json> melodies = [];
  final receipts = <(String, String)>[];
  bool loading = true;
  bool seeded = false;
  String? failure;
  int tab = 0, repeats = 1, brightness = 128, pixels = 1, fps = 20, pixel = 0;
  String stripMode = 'solid', path = 'board', silentTarget = 'board';
  String? melody, stripEffect;
  bool get ready => client.current && !client.busy && !loading;
  bool get boardReady => ready && widget.session.boardReady;
  @override
  void initState() {
    super.initState();
    client.addListener(changed);
    widget.session.addListener(changed);
    unawaited(load());
  }

  void changed() {
    if (mounted) setState(() {});
  }

  Future<void> load() async {
    setState(() {
      loading = true;
      failure = null;
    });
    try {
      await client.load();
      if (client.supports('melody')) {
        final value = await client.rpc(
          'controller.melodies.list',
          {},
          board: false,
          mutation: false,
        );
        if (value is! List ||
            value.length > 128 ||
            value.any((v) => v is! Map || v['name'] is! String)) {
          throw const Rejected('Invalid melody catalog.');
        }
        melodies = objects(value);
      }
      if (!seeded) {
        final strip = object(widget.session.peripherals['strip']);
        if (strip['default_pixels'] is int) pixels = strip['default_pixels'];
        if (strip['default_fps'] is int) fps = strip['default_fps'];
        final led = object(widget.session.state['status_led']);
        if (['red', 'green', 'blue'].every((k) => led[k] is int)) {
          color.text =
              '#${[led['red'], led['green'], led['blue']].map((v) => (v as int).toRadixString(16).padLeft(2, '0')).join('').toUpperCase()}';
          if (led['brightness'] is int) {
            brightness = (led['brightness'] as int).clamp(0, 255);
          }
        }
        seeded = true;
      }
    } catch (e) {
      if (mounted) failure = '$e';
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<bool> approve(String text) async =>
      await showDialog<bool>(
        context: context,
        builder: (context) => ControllerDialog(
          title: 'Run controller command?',
          icon: Icons.terminal,
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Run'),
            ),
          ],
          child: SelectableText(
            text,
            textDirection: TextDirection.ltr,
            style: const TextStyle(fontFamily: 'DroidSansMono'),
          ),
        ),
      ) ??
      false;
  Future<void> operation(
    String label,
    Future<dynamic> Function() action,
  ) async {
    try {
      if (mounted) setState(() => failure = null);
      final result = await action();
      if (!mounted || result == null) return;
      final output = result is Map && result['output'] is String
          ? result['output'] as String
          : const JsonEncoder.withIndent('  ').convert(result);
      setState(() {
        receipts.insert(0, (
          label,
          output.length > 65536 ? '${output.substring(0, 65536)}…' : output,
        ));
        if (receipts.length > 10) receipts.removeLast();
      });
    } catch (e) {
      if (mounted) setState(() => failure = '$e');
    }
  }

  Future<void> run(String text, {bool manual = false}) => operation(
    text.split(' ').first,
    () => client.run(text, confirm: manual ? approve : null),
  );
  Widget action(
    String label,
    IconData icon,
    VoidCallback? callback, {
    bool danger = false,
  }) {
    final colors = Theme.of(context).colorScheme;
    final style = OutlinedButton.styleFrom(
      minimumSize: const Size(44, 44),
      foregroundColor: danger ? colors.error : null,
      padding: const EdgeInsets.symmetric(horizontal: 12),
    );
    return widget.preferences.buttonStyle == 'outlined'
        ? OutlinedButton.icon(
            onPressed: callback,
            style: style,
            icon: Icon(icon, size: 18),
            label: Text(label),
          )
        : widget.preferences.buttonStyle == 'filled'
        ? FilledButton.icon(
            onPressed: callback,
            style: style,
            icon: Icon(icon, size: 18),
            label: Text(label),
          )
        : FilledButton.tonalIcon(
            onPressed: callback,
            style: style,
            icon: Icon(icon, size: 18),
            label: Text(label),
          );
  }

  Widget card(
    String title,
    IconData icon,
    List<Widget> children, {
    Widget? trailing,
  }) => Card(
    child: Padding(
      padding: EdgeInsets.all(widget.preferences.compact ? 12 : 18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                padding: EdgeInsets.all(
                  widget.preferences.effectiveHeader == 'tile' ? 8 : 0,
                ),
                decoration: widget.preferences.effectiveHeader == 'tile'
                    ? BoxDecoration(
                        color: Theme.of(context).colorScheme.primaryContainer,
                        borderRadius: BorderRadius.circular(10),
                      )
                    : null,
                child: Icon(
                  icon,
                  size: 20,
                  color: Theme.of(context).colorScheme.primary,
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
          if (children.isNotEmpty) const SizedBox(height: 14),
          ...children,
        ],
      ),
    ),
  );
  Widget number(
    String label,
    int value,
    int min,
    int max,
    ValueChanged<int> changed, {
    String? key,
  }) => TextFormField(
    key: ValueKey(key ?? label),
    initialValue: '$value',
    keyboardType: TextInputType.number,
    inputFormatters: [AddressCharacterFormatter()],
    decoration: InputDecoration(labelText: label),
    autovalidateMode: AutovalidateMode.onUserInteraction,
    validator: (v) {
      final n = int.tryParse(v ?? '');
      return n == null || n < min || n > max ? '$min–$max' : null;
    },
    onChanged: (v) {
      final n = int.tryParse(v);
      changed(n ?? min - 1);
    },
  );
  Widget choices(
    String label,
    String value,
    Map<String, String> values,
    ValueChanged<String> changed,
  ) => DropdownButtonFormField<String>(
    key: ValueKey('$label/$value'),
    initialValue: values.containsKey(value) ? value : null,
    isExpanded: true,
    decoration: InputDecoration(labelText: label),
    items: [
      for (final e in values.entries)
        DropdownMenuItem(value: e.key, child: Text(e.value)),
    ],
    onChanged: (v) {
      if (v != null) setState(() => changed(v));
    },
  );
  Widget pair(Widget a, Widget b) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(child: a),
        const SizedBox(width: 12),
        Expanded(child: b),
      ],
    ),
  );
  Future<void> boardSettings() async {
    try {
      final result = object(await client.run('settings export-live'));
      final value = object(jsonDecode('${result['output']}'));
      final settings = GoBoardSettings.fromJson(object(value['settings']));
      if (!mounted || !client.current) return;
      final baseline = settings.command();
      await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (_) => GoSettingsDialog(
          settings: settings,
          submit: (next) async {
            if (next == baseline) return;
            final result = await client.saveSettings(
              next,
              baseline,
              confirm: approve,
            );
            if (result == null) {
              throw const Rejected('Changes were not submitted.');
            }
            if (mounted) {
              setState(
                () => receipts.insert(0, (
                  'Board settings',
                  '${object(result)['output'] ?? result}',
                )),
              );
            }
          },
        ),
      );
    } catch (e) {
      if (mounted) setState(() => failure = '$e');
    }
  }

  Widget toolGrid(List<Widget> children) => LayoutBuilder(
    builder: (context, bounds) {
      final columns = bounds.maxWidth >= 900 ? 2 : 1;
      final width = (bounds.maxWidth - (columns - 1) * 12) / columns;
      return SizedBox(
        width: bounds.maxWidth,
        child: Wrap(
          spacing: 12,
          runSpacing: 12,
          children: [
            for (final child in children) SizedBox(width: width, child: child),
          ],
        ),
      );
    },
  );
  Widget boardTools() => toolGrid([
    if (client.supports('settings'))
      card('Board settings', Icons.tune, [
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            action(
              'Edit settings',
              Icons.settings_outlined,
              boardReady ? () => unawaited(boardSettings()) : null,
            ),
            action(
              'Export live',
              Icons.download,
              boardReady ? () => unawaited(run('settings export-live')) : null,
            ),
          ],
        ),
      ]),
    if (client.supports('board'))
      card('Board name', Icons.badge_outlined, [
        TextField(
          controller: name,
          maxLength: 8,
          onChanged: (_) => setState(() {}),
          decoration: const InputDecoration(labelText: 'Name'),
        ),
        Wrap(
          spacing: 8,
          children: [
            action(
              'Save',
              Icons.check,
              boardReady && validNativeName(name.text, 8)
                  ? () => unawaited(
                      run(boardNameCommand(name.text), manual: true),
                    )
                  : null,
            ),
            action(
              'Read',
              Icons.refresh,
              boardReady ? () => unawaited(run('board name get')) : null,
            ),
          ],
        ),
      ]),
    if (client.supports('menu'))
      card('Front-panel menu', Icons.view_list_outlined, [
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            action(
              'Pages',
              Icons.list,
              boardReady ? () => unawaited(run('menu list')) : null,
            ),
            action(
              'Layout',
              Icons.view_agenda_outlined,
              boardReady ? () => unawaited(run('menu layout')) : null,
            ),
          ],
        ),
      ]),
    if ([
      'ports',
      'port-process',
      'os',
      'hotkeys',
      'webhook',
    ].any(client.supports))
      card('Host & diagnostics', Icons.computer, [
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final e in [
              ('ports', 'Serial ports', Icons.usb),
              ('port-process', 'Port owner', Icons.manage_search),
              ('os facts', 'Host facts', Icons.computer),
              ('os policy', 'OS policy', Icons.policy_outlined),
              ('hotkeys status', 'Hotkeys', Icons.keyboard),
              ('webhook status', 'Webhooks', Icons.webhook),
            ])
              if (client.supports(e.$1.split(' ').first))
                action(e.$2, e.$3, ready ? () => unawaited(run(e.$1)) : null),
          ],
        ),
      ]),
  ]);
  List<int>? get rgb {
    final text = color.text.trim();
    if (!RegExp(r'^#[0-9a-fA-F]{6}$').hasMatch(text) ||
        brightness < 0 ||
        brightness > 255) {
      return null;
    }
    return [
      for (var i = 1; i < 7; i += 2)
        int.parse(text.substring(i, i + 2), radix: 16),
    ];
  }

  Widget colorFields() => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      TextFormField(
        controller: color,
        decoration: const InputDecoration(
          labelText: 'Color',
          hintText: '#RRGGBB',
          prefixIcon: Icon(Icons.palette_outlined),
        ),
        onChanged: (_) => setState(() {}),
        autovalidateMode: AutovalidateMode.onUserInteraction,
        validator: (v) => RegExp(r'^#[0-9a-fA-F]{6}$').hasMatch(v ?? '')
            ? null
            : 'Use #RRGGBB',
      ),
      const SizedBox(height: 8),
      Wrap(
        spacing: 4,
        children: [
          for (final value in [
            '#FF4040',
            '#40DD80',
            '#4080FF',
            '#AA70DD',
            '#FFFFFF',
            '#000000',
          ])
            IconButton(
              tooltip: value,
              icon: Icon(
                color.text.toUpperCase() == value
                    ? Icons.check_circle
                    : Icons.circle,
                color: Color(
                  0xff000000 | int.parse(value.substring(1), radix: 16),
                ),
              ),
              onPressed: () => setState(() => color.text = value),
            ),
        ],
      ),
      number(
        'Brightness',
        brightness,
        0,
        255,
        (v) => setState(() => brightness = v),
      ),
      const SizedBox(height: 12),
    ],
  );
  Widget lighting() {
    final strip = object(widget.session.peripherals['strip']);
    final modes = strip['modes'] is List
        ? (strip['modes'] as List).whereType<String>().toList()
        : <String>[];
    final maxPixels = strip['maximum_pixels'], maxFps = strip['maximum_fps'];
    final usable =
        modes.isNotEmpty &&
        maxPixels is int &&
        maxPixels >= 1 &&
        maxPixels <= 100 &&
        maxFps is int &&
        maxFps >= 1 &&
        maxFps <= 30;
    final hasRgb = widget.session.state['have_status_led'] == true;
    final stripEffects = objects(widget.session.peripherals['strip_effects']);
    final valid =
        modes.contains(stripMode) &&
        (!['solid', 'pixel'].contains(stripMode) || rgb != null) &&
        pixels >= 1 &&
        pixels <= (maxPixels is int ? maxPixels : 0) &&
        fps >= 1 &&
        fps <= (maxFps is int ? maxFps : 0);
    return toolGrid([
      if (hasRgb && client.supports('rgb'))
        card('Status RGB', Icons.lightbulb_outline, [
          colorFields(),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              action(
                'Apply color',
                Icons.check,
                boardReady && rgb != null
                    ? () => unawaited(
                        operation(
                          'Status RGB',
                          () => client.rpc('controller.status_led.set', {
                            'red': rgb![0],
                            'green': rgb![1],
                            'blue': rgb![2],
                            'brightness': brightness,
                          }),
                        ),
                      )
                    : null,
              ),
              if (object(widget.session.state['outputs'])['status_owner'] ==
                  'effect')
                action(
                  'Automatic',
                  Icons.auto_mode,
                  boardReady
                      ? () => unawaited(
                          operation(
                            'Status RGB',
                            () => client.rpc(
                              'controller.status_led.release',
                              {},
                              stop: true,
                            ),
                          ),
                        )
                      : null,
                ),
            ],
          ),
        ]),
      if (usable && client.supports('strip'))
        card('Addressable strip', Icons.gradient, [
          choices('Mode', stripMode, {
            for (final mode in modes)
              mode:
                  {
                    'solid': 'Solid',
                    'pixel': 'Pixel',
                    'frame': 'RGB frame',
                    'rainbow': 'Rainbow',
                    'effect': 'Effect',
                  }[mode] ??
                  mode,
          }, (v) => stripMode = v),
          if (modes.contains('frame')) ExpansionTile(title: const Text('Pixel diagnostics'), children: [PixelDiagnostic(client: client, count: pixels.clamp(1,maxPixels))]),
          const SizedBox(height: 12),
          pair(
            number(
              'Pixels',
              pixels,
              1,
              maxPixels,
              (v) => setState(() => pixels = v),
            ),
            number('FPS', fps, 1, maxFps, (v) => setState(() => fps = v)),
          ),
          if (['solid', 'pixel'].contains(stripMode)) colorFields(),
          if (stripMode == 'pixel')
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: number(
                'Pixel index',
                pixel,
                0,
                maxPixels - 1,
                (v) => setState(() => pixel = v),
              ),
            ),
          if (stripMode == 'frame')
            TextFormField(
              controller: frame,
              maxLength: maxPixels * 6,
              decoration: const InputDecoration(labelText: 'RGB frame (hex)'),
              onChanged: (_) => setState(() {}),
            ),
          if (stripMode == 'effect')
            choices('Effect', stripEffect ?? '', {
              for (final effect in stripEffects)
                if (effect['id'] is String && effect['name'] is String)
                  '${effect['id']}': '${effect['name']}',
            }, (v) => stripEffect = v),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              action(
                stripMode == 'rainbow' || stripMode == 'effect'
                    ? 'Play'
                    : 'Apply',
                Icons.play_arrow,
                boardReady &&
                        valid &&
                        (stripMode != 'pixel' ||
                            pixel >= 0 && pixel < maxPixels) &&
                        (stripMode != 'effect' ||
                            stripEffects.any((e) => e['id'] == stripEffect)) &&
                        (stripMode != 'frame' ||
                            RegExp(r'^(?:[0-9a-fA-F]{6})+$')
                                    .hasMatch(frame.text) &&
                                frame.text.length <= maxPixels * 6)
                    ? () {
                        final value = switch (stripMode) {
                          'solid' => 'strip fill ${rgb!.join(' ')} $brightness',
                          'pixel' =>
                            'strip pixel $pixel ${rgb!.join(' ')} $brightness',
                          'rainbow' => 'strip rainbow $pixels $fps',
                          'effect' =>
                            'effect play ${nativeArgument(stripEffect!)} $pixels $fps',
                          _ => 'strip frame ${frame.text}',
                        };
                        unawaited(run(value));
                      }
                    : null,
              ),
              action(
                'Configure count',
                Icons.settings_outlined,
                boardReady && pixels >= 1 && pixels <= maxPixels
                    ? () => unawaited(run('strip config $pixels', manual: true))
                    : null,
              ),
              if ((object(widget.session.state['outputs'])['strip_id']
                          as num? ??
                      0) >
                  0)
                action(
                  'Stop',
                  Icons.stop,
                  boardReady ? () => unawaited(run('strip stop')) : null,
                  danger: true,
                ),
              action(
                'Clear',
                Icons.clear,
                boardReady ? () => unawaited(run('strip clear')) : null,
              ),
            ],
          ),
        ]),
    ]);
  }

  Widget sound() => toolGrid([
    if (client.supports('buzzer') && client.supports('silent'))
      card('Buzzer routing', Icons.route_outlined, [
        choices('Output path', path, const {
          'board': 'Board',
          'host': 'Host',
          'both': 'Board + host',
          'none': 'None',
        }, (v) => path = v),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            action(
              'Apply path',
              Icons.check,
              boardReady
                  ? () => unawaited(run('buzzer path $path', manual: true))
                  : null,
            ),
            action(
              'Read routing',
              Icons.refresh,
              ready ? () => unawaited(run('buzzer status')) : null,
            ),
          ],
        ),
        const SizedBox(height: 12),
        choices('Silence target', silentTarget, const {
          'board': 'Board',
          'host': 'Host',
          'both': 'Board + host',
        }, (v) => silentTarget = v),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          children: [
            action(
              'Mute',
              Icons.volume_off_outlined,
              boardReady
                  ? () => unawaited(run('silent $silentTarget on'))
                  : null,
            ),
            action(
              'Unmute',
              Icons.volume_up_outlined,
              boardReady
                  ? () => unawaited(run('silent $silentTarget off'))
                  : null,
            ),
          ],
        ),
      ]),
    if (melodies.isNotEmpty)
      card('Melodies', Icons.music_note_outlined, [
        choices('Melody', melody ?? '', {
          for (final m in melodies) '${m['name']}': '${m['name']}',
        }, (v) => melody = v),
        const SizedBox(height: 12),
        number('Repeats', repeats, 1, 20, (v) => setState(() => repeats = v)),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          children: [
            action(
              'Play',
              Icons.play_arrow,
              boardReady && melody != null && repeats >= 1 && repeats <= 20
                  ? () => unawaited(run(melodyPlayCommand(melody!, repeats)))
                  : null,
            ),
            if ((object(widget.session.state['outputs'])['melody_id'] as num? ??
                    0) >
                0)
              action(
                'Stop',
                Icons.stop,
                boardReady ? () => unawaited(run('melody stop')) : null,
                danger: true,
              ),
          ],
        ),
      ]),
  ]);
  Widget recording() {
    final macros = object(widget.session.state['macros']);
    final active = object(macros['recording'])['active'] == true;
    return toolGrid([
      if (client.supports('macro'))
        card('Effect recording', Icons.fiber_manual_record_outlined, [
          if (!active)
            TextField(
              controller: recordName,
              maxLength: 64,
              decoration: const InputDecoration(labelText: 'Recording name'),
              onChanged: (_) => setState(() {}),
            ),
          if (active) ...[
            SelectableText(
              const JsonEncoder.withIndent('  ')
                  .convert(object(macros['recording'])),
            ),
            Wrap(
              spacing: 8,
              children: [
                action(
                  'Save recording',
                  Icons.save_outlined,
                  boardReady
                      ? () => unawaited(run('macro record save', manual: true))
                      : null,
                ),
                action(
                  'Discard',
                  Icons.delete_outline,
                  boardReady
                      ? () =>
                            unawaited(run('macro record discard', manual: true))
                      : null,
                ),
              ],
            ),
          ] else
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final e in [
                  ('start', 'Automatic'),
                  if (object(widget.session.state['hello'])['capabilities']
                          is int &&
                      (object(widget.session.state['hello'])['capabilities']
                                  as int) &
                              (1 << 22) !=
                          0) ...[
                    ('start-mcu', 'Device clock'),
                    ('start-board', 'Board retained'),
                  ],
                ])
                  action(
                    'Record ${e.$2}',
                    Icons.fiber_manual_record,
                    boardReady && validNativeName(recordName.text, 64)
                        ? () => unawaited(
                            run(
                              'macro record ${e.$1} ${nativeArgument(recordName.text.trim())}',
                              manual: true,
                            ),
                          )
                        : null,
                  ),
              ],
            ),
          const SizedBox(height: 12),
          action(
            'Read recording',
            Icons.refresh,
            ready ? () => unawaited(run('macro record status')) : null,
          ),
        ]),
      if (client.supports('rf') &&
          object(widget.session.state['hello'])['capabilities'] is int &&
          (object(widget.session.state['hello'])['capabilities'] as int) &
                  (1 << 4) !=
              0)
        card('RF controls', Icons.settings_remote_outlined, [
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              if (object(widget.session.state['rf_learning'])['active'] != true)
                action(
                  'Learn 30 seconds',
                  Icons.sensors,
                  boardReady
                      ? () => unawaited(
                          operation(
                            'RF learning',
                            () => client.rpc('controller.rf.learn.start', {
                              'mode': 'timer',
                              'timeout_ms': 30000,
                            }),
                          ),
                        )
                      : null,
                ),
              if (object(widget.session.state['rf_learning'])['active'] == true)
                action(
                  'Cancel learning',
                  Icons.close,
                  boardReady
                      ? () => unawaited(
                          operation(
                            'RF learning',
                            () => client.rpc(
                              'controller.rf.learn.cancel',
                              {},
                              stop: true,
                            ),
                          ),
                        )
                      : null,
                ),
              action(
                'Learned controls',
                Icons.list,
                boardReady
                    ? () => unawaited(
                        operation(
                          'RF catalog',
                          () => client.rpc(
                            'controller.rf.catalog',
                            {},
                            mutation: false,
                          ),
                        ),
                      )
                    : null,
              ),
            ],
          ),
        ]),
      if (client.supports('automation'))
        card('Automation rules', Icons.rule, [
          Wrap(
            spacing: 8,
            children: [
              action(
                'Host rules',
                Icons.computer,
                ready ? () => unawaited(run('automation list')) : null,
              ),
              if (client.supports('board-automation'))
                action(
                  'Board rules',
                  Icons.memory,
                  boardReady
                      ? () => unawaited(run('board-automation list'))
                      : null,
                ),
            ],
          ),
        ]),
    ]);
  }

  Widget commands() {
    final found = client.commands.where((v) => v.matches(search.text)).toList();
    final words = command.text.trim().toLowerCase().split(RegExp(r'\s+'));
    final selected = client.commands
        .where((v) => v.name == words.first || v.aliases.contains(words.first))
        .firstOrNull;
    return Column(
      children: [
        card('Command workbench', Icons.terminal, [
          TextField(
            controller: command,
            textDirection: TextDirection.ltr,
            maxLength: 32768,
            minLines: 1,
            maxLines: 5,
            style: const TextStyle(fontFamily: 'DroidSansMono', fontSize: 13),
            onChanged: (_) => setState(() {}),
            decoration: const InputDecoration(
              labelText: 'Command',
              prefixIcon: Icon(Icons.chevron_right),
            ),
          ),
          if (selected != null)
            ExpansionTile(
              key: ValueKey(selected.name),
              tilePadding: EdgeInsets.zero,
              title: const Text('Usage'),
              children: [
                Align(
                  alignment: AlignmentDirectional.centerStart,
                  child: SelectableText(
                    selected.usage,
                    textDirection: TextDirection.ltr,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
              ],
            ),
          Align(
            alignment: AlignmentDirectional.centerEnd,
            child: action(
              'Run',
              Icons.play_arrow,
              ready && selected != null && command.text.trim().isNotEmpty
                  ? () => unawaited(run(command.text.trim(), manual: true))
                  : null,
            ),
          ),
        ]),
        card('Command catalog', Icons.manage_search, [
          TextField(
            controller: search,
            onChanged: (_) => setState(() {}),
            decoration: const InputDecoration(
              labelText: 'Search commands, options or categories',
              prefixIcon: Icon(Icons.search),
            ),
          ),
          const SizedBox(height: 8),
          for (final entry in found)
            ListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              title: Text(entry.name),
              subtitle: Text(
                entry.summary,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
              trailing: const Icon(Icons.arrow_forward, size: 18),
              onTap: () => setState(() {
                command.text = '${entry.name} ';
                command.selection = TextSelection.collapsed(
                  offset: command.text.length,
                );
              }),
            ),
        ]),
      ],
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('Controller tools'),
      actions: [
        if (object(widget.session.state['emergency_stop'])['active'] == true)
          IconButton(
            tooltip: 'Release emergency stop',
            color: Theme.of(context).colorScheme.error,
            icon: const Icon(Icons.lock_open),
            onPressed: ready
                ? () => unawaited(run('estop off', manual: true))
                : null,
          )
        else if (widget.session.boardReady)
          IconButton(
            tooltip: 'Emergency stop',
            color: Theme.of(context).colorScheme.error,
            icon: const Icon(Icons.front_hand_outlined),
            onPressed: () =>
                unawaited(operation('Emergency stop', client.emergencyStop)),
          ),
        IconButton(
          tooltip: 'Refresh tools',
          icon: const Icon(Icons.refresh),
          onPressed: client.busy || loading || !client.current
              ? null
              : () => unawaited(load()),
        ),
      ],
    ),
    body: Column(
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: SegmentedButton<int>(
              segments: const [
                ButtonSegment(
                  value: 0,
                  icon: Icon(Icons.memory),
                  label: Text('Board'),
                ),
                ButtonSegment(
                  value: 1,
                  icon: Icon(Icons.lightbulb_outline),
                  label: Text('Lighting'),
                ),
                ButtonSegment(
                  value: 2,
                  icon: Icon(Icons.music_note),
                  label: Text('Sound'),
                ),
                ButtonSegment(
                  value: 3,
                  icon: Icon(Icons.fiber_manual_record_outlined),
                  label: Text('Recording'),
                ),
                ButtonSegment(
                  value: 4,
                  icon: Icon(Icons.terminal),
                  label: Text('Commands'),
                ),
              ],
              selected: {tab},
              onSelectionChanged: (v) => setState(() => tab = v.single),
            ),
          ),
        ),
        const SizedBox(height: 8),
        SizedBox(
          height: 2,
          child: loading || client.busy
              ? const LinearProgressIndicator(minHeight: 2)
              : null,
        ),
        if (!client.current && !loading)
          MaterialBanner(
            content: const Text('Connection changed. Reopen controller tools.'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('Close'),
              ),
            ],
          ),
        if (failure != null)
          MaterialBanner(
            content: Text(failure!),
            leading: const Icon(Icons.warning_amber),
            actions: [
              TextButton(
                onPressed: () => setState(() => failure = null),
                child: const Text('Dismiss'),
              ),
            ],
          ),
        Expanded(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(14),
            child: Column(
              children: [
                switch (tab) {
                  0 => boardTools(),
                  1 => lighting(),
                  2 => sound(),
                  3 => recording(),
                  _ => commands(),
                },
                if (receipts.isNotEmpty)
                  card('Results', Icons.receipt_long_outlined, [
                    for (final entry in receipts)
                      ExpansionTile(
                        title: Text(entry.$1),
                        children: [
                          Align(
                            alignment: AlignmentDirectional.centerStart,
                            child: SelectableText(
                              entry.$2,
                              style: const TextStyle(
                                fontFamily: 'DroidSansMono',
                                fontSize: 12,
                              ),
                            ),
                          ),
                          Align(
                            alignment: AlignmentDirectional.centerEnd,
                            child: IconButton(
                              tooltip: 'Copy result',
                              icon: const Icon(Icons.copy),
                              onPressed: () => Clipboard.setData(
                                ClipboardData(text: entry.$2),
                              ),
                            ),
                          ),
                        ],
                      ),
                  ]),
              ],
            ),
          ),
        ),
      ],
    ),
  );
  @override
  void dispose() {
    client.removeListener(changed);
    widget.session.removeListener(changed);
    client.dispose();
    for (final v in [search, command, color, frame, name, recordName]) {
      v.dispose();
    }
    super.dispose();
  }
}

class GoSettingsDialog extends StatefulWidget {
  const GoSettingsDialog({super.key, required this.settings, this.submit});
  final GoBoardSettings settings;
  final Future<void> Function(String)? submit;
  @override
  State<GoSettingsDialog> createState() => _GoSettingsDialogState();
}

class _GoSettingsDialogState extends State<GoSettingsDialog> {
  final form = GlobalKey<FormState>();
  String? error;
  bool saving = false;
  Json get v => widget.settings.values;
  void flag(int mask, bool enabled, {bool inverse = false}) => setState(() {
    final set = inverse ? !enabled : enabled;
    v['flags'] = set ? v['flags'] | mask : v['flags'] & ~mask;
  });
  @override
  Widget build(BuildContext context) => ControllerDialog(
    title: 'Board settings',
    icon: Icons.tune,
    onClose: () {
      if (!saving) Navigator.pop(context);
    },
    actions: [
      TextButton(
        onPressed: saving ? null : () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      FilledButton(
        onPressed: saving
            ? null
            : () async {
                if (!form.currentState!.validate()) return;
                try {
                  final command = widget.settings.command();
                  if (widget.submit == null) {
                    Navigator.pop(context, command);
                    return;
                  }
                  setState(() {
                    saving = true;
                    error = null;
                  });
                  await widget.submit!(command);
                  if (context.mounted) Navigator.pop(context);
                } catch (e) {
                  if (mounted) setState(() => error = '$e');
                } finally {
                  if (mounted) setState(() => saving = false);
                }
              },
        child: const Text('Review changes'),
      ),
    ],
    child: Form(
      key: form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          SizedBox(
            height: 2,
            child: saving ? const LinearProgressIndicator(minHeight: 2) : null,
          ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Board silent'),
            value: (v['flags'] & 1) != 0,
            onChanged: saving ? null : (b) => flag(1, b),
          ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Door sounds'),
            value: (v['flags'] & 32) == 0,
            onChanged: saving ? null : (b) => flag(32, b, inverse: true),
          ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Relay sounds'),
            value: (v['flags'] & 64) == 0,
            onChanged: saving ? null : (b) => flag(64, b, inverse: true),
          ),
          DropdownButtonFormField<int>(
            initialValue: (v['flags'] >> 3) & 3,
            decoration: const InputDecoration(labelText: 'Motion door policy'),
            items: [
              for (final e in const {
                0: 'Always',
                1: 'Door closed',
                2: 'Door open',
                3: 'Never',
              }.entries)
                DropdownMenuItem(value: e.key, child: Text(e.value)),
            ],
            onChanged: saving
                ? null
                : (n) {
                    if (n != null) {
                      setState(
                        () => v['flags'] = (v['flags'] & ~24) | (n << 3),
                      );
                    }
                  },
          ),
          const SizedBox(height: 12),
          for (final f in GoBoardSettings.fields)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: TextFormField(
                enabled: !saving,
                key: ValueKey(f.$1),
                initialValue: '${v[f.$1]}',
                keyboardType: TextInputType.number,
                inputFormatters: [AddressCharacterFormatter()],
                decoration: InputDecoration(labelText: f.$2),
                validator: (text) {
                  final n = int.tryParse(text ?? '');
                  return n == null ||
                          n < f.$3 ||
                          n > f.$4 ||
                          (f.$1 == 'stream_period_ms' && n != 0 && n < 100)
                      ? '${f.$3}–${f.$4}'
                      : null;
                },
                onChanged: saving
                    ? null
                    : (text) => v[f.$1] = int.tryParse(text) ?? -1,
              ),
            ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Restore last menu page'),
            value: v['save_last_page'],
            onChanged: saving
                ? null
                : (b) => setState(() => v['save_last_page'] = b),
          ),
          for (final e in const {
            1: 'Persist default motion',
            2: 'Persist user relays',
            4: 'Persist user PWM',
            8: 'Persist direction only',
          }.entries)
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: Text(e.value),
              value: (v['output_persistence'] & e.key) != 0,
              onChanged: saving
                  ? null
                  : (b) => setState(
                      () => v['output_persistence'] = b
                          ? v['output_persistence'] | e.key
                          : v['output_persistence'] & ~e.key,
                    ),
            ),
          if (error != null)
            Text(
              error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
        ],
      ),
    ),
  );
}

class GoDisplayDialog extends StatefulWidget {
  const GoDisplayDialog({
    super.key,
    required this.text,
    required this.target,
    required this.lcd,
  });
  final String text, target;
  final bool lcd;
  @override
  State<GoDisplayDialog> createState() => _GoDisplayDialogState();
}

class _GoDisplayDialogState extends State<GoDisplayDialog> {
  final form = GlobalKey<FormState>();
  late final options = GoDisplayOptions()
    ..text = widget.text
    ..target = widget.target;
  String? failure;
  Widget number(
    String key,
    String label,
    int value,
    int min,
    int max,
    ValueChanged<int> changed,
  ) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: TextFormField(
      key: ValueKey(key),
      initialValue: '$value',
      keyboardType: TextInputType.number,
      inputFormatters: [AddressCharacterFormatter()],
      decoration: InputDecoration(labelText: label),
      validator: (s) {
        final n = int.tryParse(s ?? '');
        return n == null || n < min || n > max ? '$min–$max' : null;
      },
      onChanged: (s) => changed(int.tryParse(s) ?? -1),
    ),
  );
  @override
  Widget build(BuildContext context) => ControllerDialog(
    title: 'Display message',
    icon: Icons.text_fields,
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      FilledButton(
        onPressed: () {
          if (!form.currentState!.validate()) return;
          try {
            Navigator.pop(context, options.params());
          } catch (e) {
            setState(() => failure = '$e');
          }
        },
        child: const Text('Send'),
      ),
    ],
    child: Form(
      key: form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          DropdownButtonFormField<String>(
            initialValue: options.target,
            decoration: const InputDecoration(labelText: 'Display'),
            items: [
              const DropdownMenuItem(
                value: 'segments',
                child: Text('7-segment'),
              ),
              if (widget.lcd) ...[
                const DropdownMenuItem(value: 'lcd', child: Text('LCD')),
                const DropdownMenuItem(value: 'both', child: Text('Both')),
              ],
            ],
            onChanged: (v) {
              if (v != null) setState(() => options.target = v);
            },
          ),
          const SizedBox(height: 12),
          TextFormField(
            initialValue: options.text,
            maxLength: 40,
            decoration: const InputDecoration(labelText: 'Message'),
            onChanged: (v) => options.text = v,
            validator: (v) => (v ?? '').codeUnits.any((n) => n < 32 || n > 126)
                ? 'Use printable ASCII.'
                : null,
          ),
          DropdownButtonFormField<String>(
            initialValue: options.repeat,
            decoration: const InputDecoration(labelText: 'Repeat'),
            items: const [
              DropdownMenuItem(value: 'once', child: Text('Once')),
              DropdownMenuItem(value: 'loop', child: Text('Loop')),
              DropdownMenuItem(value: 'interval', child: Text('Interval')),
            ],
            onChanged: (v) {
              if (v != null) setState(() => options.repeat = v);
            },
          ),
          const SizedBox(height: 12),
          number(
            'display-speed',
            'Scroll speed (ms)',
            options.speedMs,
            80,
            5000,
            (v) => options.speedMs = v,
          ),
          number(
            'display-duration',
            'Duration (ms)',
            options.durationMs,
            80,
            65535,
            (v) => options.durationMs = v,
          ),
          if (options.repeat == 'interval')
            number(
              'display-interval',
              'Interval (s)',
              options.intervalSeconds,
              1,
              255,
              (v) => options.intervalSeconds = v,
            ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Force scroll'),
            value: options.scroll,
            onChanged: (v) => setState(() => options.scroll = v),
          ),
          if (failure != null)
            Text(
              failure!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
        ],
      ),
    ),
  );
}
