import 'dart:convert';
import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'transport.dart';
import 'sequence_timeline.dart';

/// Pure native authoring; never creates a connection or actuates an output.
class EffectEditor extends StatefulWidget {
  const EffectEditor({super.key, required this.initial, required this.saved});
  final Json initial;
  final Future<void> Function(Json) saved;
  @override
  State<EffectEditor> createState() => _EffectEditorState();
}

class _EffectEditorState extends State<EffectEditor> {
  late Json effect;
  late TextEditingController name, id, category, raw;
  int selected = -1;
  int timingRevision = 0;
  bool saving = false;
  String? error;
  @override
  void initState() {
    super.initState();
    effect = object(jsonDecode(jsonEncode(widget.initial)));
    name = TextEditingController(text: '${effect['name'] ?? ''}');
    id = TextEditingController(text: '${effect['id'] ?? ''}');
    category = TextEditingController(
      text: '${effect['category'] ?? 'Effects'}',
    );
    raw = TextEditingController(
      text: const JsonEncoder.withIndent('  ').convert(effect),
    );
  }

  @override
  void dispose() {
    name.dispose();
    id.dispose();
    category.dispose();
    raw.dispose();
    super.dispose();
  }

  List<Json> get steps => objects(effect['steps']);
  Future<void> dismiss() async {
    if (saving) return;
    final dirty =
        jsonEncode(effect) != jsonEncode(widget.initial) ||
        name.text != '${widget.initial['name'] ?? ''}' ||
        id.text != '${widget.initial['id'] ?? ''}' ||
        category.text != '${widget.initial['category'] ?? 'Effects'}' ||
        raw.text != const JsonEncoder.withIndent('  ').convert(effect);
    if (dirty) {
      final discard = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('Unsaved effect'),
          content: const Text(
            'Discard editor changes? Saved local drafts are retained.',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Keep editing'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Discard changes'),
            ),
          ],
        ),
      );
      if (discard != true) return;
    }
    if (mounted) Navigator.pop(context);
  }

  void changeStep(String key, dynamic value) {
    final next = steps;
    if (selected < 0 || selected >= next.length) return;
    next[selected] = {...next[selected], key: value};
    setState(() {
      effect['steps'] = next;
      raw.text = const JsonEncoder.withIndent('  ').convert(effect);
    });
  }

  Widget number(String label, String key, num value, {num scale = 1}) =>
      SizedBox(
        width: 140,
        child: TextFormField(
          key: ValueKey('$selected/$key/$timingRevision'),
          initialValue: '${value / scale}',
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          decoration: InputDecoration(labelText: label),
          onChanged: (text) {
            final parsed = num.tryParse(text);
            if (parsed != null && parsed.isFinite && parsed >= 0) {
              changeStep(key, (parsed * scale).round());
            }
          },
        ),
      );
  @override
  Widget build(BuildContext context) {
    final current = selected >= 0 && selected < steps.length
        ? steps[selected]
        : <String, dynamic>{};
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) unawaited(dismiss());
      },
      child: Dialog(
        insetPadding: const EdgeInsets.all(16),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 1000),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    const Icon(Icons.auto_awesome),
                    const SizedBox(width: 10),
                    const Expanded(
                      child: Text(
                        'Effect designer',
                        style: TextStyle(fontSize: 20),
                      ),
                    ),
                    IconButton(
                      tooltip: 'Discard editor changes',
                      onPressed: saving ? null : () => unawaited(dismiss()),
                      icon: const Icon(Icons.close),
                    ),
                  ],
                ),
              ),
              const Divider(height: 1),
              Flexible(
                child: SingleChildScrollView(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Wrap(
                        spacing: 12,
                        runSpacing: 12,
                        children: [
                          SizedBox(
                            width: 250,
                            child: TextField(
                              controller: name,
                              decoration: const InputDecoration(
                                labelText: 'Name',
                              ),
                            ),
                          ),
                          SizedBox(
                            width: 160,
                            child: TextField(
                              controller: id,
                              decoration: const InputDecoration(
                                labelText: 'Stable effect ID',
                              ),
                            ),
                          ),
                          SizedBox(
                            width: 220,
                            child: TextField(
                              controller: category,
                              decoration: const InputDecoration(
                                labelText: 'Folder',
                              ),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 16),
                      if (effect['kind'] == 'sequence') ...[
                        SequenceTimeline(
                          steps: steps,
                          selected: selected,
                          enabled: !saving,
                          select: (index) => setState(() => selected = index),
                          edit: (index, start, length) {
                            final next = steps;
                            if (index < 0 || index >= next.length) return;
                            next[index] = {
                              ...next[index],
                              'at_us': start,
                              'duration_ms': ?length,
                            };
                            setState(() {
                              timingRevision++;
                              effect['steps'] = next;
                              raw.text = const JsonEncoder.withIndent('  ')
                                  .convert(effect);
                            });
                          },
                        ),
                        const SizedBox(height: 12),
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            for (final kind in ['buzzer', 'display', 'rgb'])
                              FilledButton.tonalIcon(
                                onPressed: () => setState(() {
                                  final next = steps;
                                  next.add({
                                    'kind': kind,
                                    'at_us': (sequenceDuration(next) * 1000)
                                        .round(),
                                    'duration_ms': kind == 'buzzer'
                                        ? 115
                                        : 1000,
                                    if (kind == 'buzzer') 'frequency_hz': 880,
                                    if (kind == 'display') ...{
                                      'text': 'HELLO',
                                      'destination': 'segments',
                                    },
                                    if (kind == 'rgb') ...{
                                      'red': 128,
                                      'green': 0,
                                      'blue': 255,
                                      'brightness': 255,
                                    },
                                  });
                                  effect['steps'] = next;
                                  selected = next.length - 1;
                                  raw.text = const JsonEncoder.withIndent('  ')
                                      .convert(effect);
                                }),
                                icon: const Icon(Icons.add, size: 18),
                                label: Text(kind),
                              ),
                          ],
                        ),
                        const SizedBox(height: 16),
                        if (current.isNotEmpty) ...[
                          Wrap(
                            spacing: 12,
                            runSpacing: 12,
                            children: [
                              number(
                                'Start (seconds)',
                                'at_us',
                                current['at_us'] as num? ?? 0,
                                scale: 1000000,
                              ),
                              number(
                                'Duration (ms)',
                                'duration_ms',
                                current['duration_ms'] as num? ?? 0,
                              ),
                              number(
                                'Step repeats',
                                'repeat_count',
                                current['repeat_count'] as num? ?? 1,
                              ),
                              number(
                                'Interval (ms)',
                                'repeat_interval_ms',
                                current['repeat_interval_ms'] as num? ?? 0,
                              ),
                              if (current['kind'] == 'buzzer')
                                number(
                                  'Frequency (Hz)',
                                  'frequency_hz',
                                  current['frequency_hz'] as num? ?? 0,
                                ),
                            ],
                          ),
                          if (current['kind'] == 'display')
                            Padding(
                              padding: const EdgeInsets.only(top: 12),
                              child: TextFormField(
                                key: ValueKey('display/$selected'),
                                initialValue: '${current['text'] ?? ''}',
                                maxLength: 40,
                                decoration: const InputDecoration(
                                  labelText: 'Display text',
                                ),
                                onChanged: (text) => changeStep('text', text),
                              ),
                            ),
                          if (current['kind'] == 'rgb') ...[
                            const SizedBox(height: 12),
                            for (final entry in [
                              ('red', 'Red', Colors.red),
                              ('green', 'Green', Colors.green),
                              ('blue', 'Blue', Colors.blue),
                              (
                                'brightness',
                                'Brightness',
                                Theme.of(context).colorScheme.primary,
                              ),
                            ])
                              Row(
                                children: [
                                  SizedBox(width: 86, child: Text(entry.$2)),
                                  Expanded(
                                    child: Slider(
                                      min: 0,
                                      max: 255,
                                      divisions: 255,
                                      activeColor: entry.$3,
                                      value:
                                          (current[entry.$1] is num &&
                                                      (current[entry.$1] as num)
                                                          .isFinite
                                                  ? (current[entry.$1] as num)
                                                        .toDouble()
                                                  : 0)
                                              .clamp(0, 255)
                                              .toDouble(),
                                      label: '${current[entry.$1] ?? 0}',
                                      onChanged: saving
                                          ? null
                                          : (v) =>
                                                changeStep(entry.$1, v.round()),
                                    ),
                                  ),
                                  SizedBox(
                                    width: 32,
                                    child: Text(
                                      '${current[entry.$1] ?? 0}',
                                      textAlign: TextAlign.end,
                                    ),
                                  ),
                                ],
                              ),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              children: [
                                for (final color in [
                                  const Color(0xffff4040),
                                  const Color(0xff40dd80),
                                  const Color(0xff4080ff),
                                  const Color(0xffaa70dd),
                                  Colors.white,
                                ])
                                  IconButton(
                                    tooltip:
                                        '#${color.toARGB32().toRadixString(16).substring(2).toUpperCase()}',
                                    icon: Icon(Icons.circle, color: color),
                                    onPressed: saving
                                        ? null
                                        : () {
                                            final next = steps;
                                            next[selected] = {
                                              ...next[selected],
                                              'red': (color.r * 255).round(),
                                              'green': (color.g * 255).round(),
                                              'blue': (color.b * 255).round(),
                                            };
                                            setState(() {
                                              effect['steps'] = next;
                                              raw.text =
                                                  const JsonEncoder.withIndent(
                                                    '  ',
                                                  ).convert(effect);
                                            });
                                          },
                                  ),
                              ],
                            ),
                          ],
                          TextButton.icon(
                            onPressed: () => setState(() {
                              final next = steps;
                              next.removeAt(selected);
                              effect['steps'] = next;
                              selected = -1;
                              raw.text = const JsonEncoder.withIndent('  ')
                                  .convert(effect);
                            }),
                            icon: const Icon(Icons.delete_outline),
                            label: const Text('Remove selected step'),
                          ),
                        ],
                      ],
                      ExpansionTile(
                        title: const Text('Technical definition'),
                        children: [
                          TextField(
                            controller: raw,
                            minLines: 5,
                            maxLines: 14,
                            decoration: const InputDecoration(
                              labelText: 'Complete current effect descriptor',
                            ),
                          ),
                          TextButton(
                            onPressed: () {
                              try {
                                final value = jsonDecode(raw.text);
                                if (value is! Map) {
                                  throw const FormatException();
                                }
                                setState(() {
                                  effect = object(value);
                                  selected = -1;
                                  name.text = '${effect['name'] ?? ''}';
                                  id.text = '${effect['id'] ?? ''}';
                                  category.text = '${effect['category'] ?? ''}';
                                  error = null;
                                });
                              } catch (_) {
                                setState(
                                  () => error =
                                      'Enter one valid effect JSON object.',
                                );
                              }
                            },
                            child: const Text('Apply technical edits'),
                          ),
                        ],
                      ),
                      if (error != null)
                        Text(
                          error!,
                          style: TextStyle(
                            color: Theme.of(context).colorScheme.error,
                          ),
                        ),
                    ],
                  ),
                ),
              ),
              const Divider(height: 1),
              Padding(
                padding: const EdgeInsets.all(16),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    TextButton(
                      onPressed: saving ? null : () => unawaited(dismiss()),
                      child: const Text('Discard'),
                    ),
                    const SizedBox(width: 8),
                    FilledButton(
                      onPressed: saving
                          ? null
                          : () async {
                              if (name.text.trim().isEmpty ||
                                  id.text.trim().isEmpty) {
                                setState(
                                  () => error =
                                      'A name and stable ID are required.',
                                );
                                return;
                              }
                              setState(() => saving = true);
                              try {
                                await widget.saved({
                                  ...effect,
                                  'id': id.text.trim(),
                                  'name': name.text.trim(),
                                  'category': category.text.trim(),
                                  'engine': 'host',
                                  'editable': true,
                                });
                                if (context.mounted) {
                                  Navigator.pop(context);
                                }
                              } catch (e) {
                                if (mounted) {
                                  setState(() {
                                    error = '$e';
                                    saving = false;
                                  });
                                }
                              }
                            },
                      child: Text(saving ? 'Saving…' : 'Save local draft'),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

double sequenceDuration(List<Json> steps) => steps.fold<double>(
  1000,
  (duration, step) => math.max(
    duration,
    (step['at_us'] as num? ?? 0) / 1000 +
        (step['duration_ms'] as num? ?? 0) +
        ((step['repeat_count'] as num? ?? 1) - 1).clamp(0, 64) *
            (step['repeat_interval_ms'] as num? ?? 0),
  ),
);

class StepPainter extends CustomPainter {
  StepPainter(this.step, this.selected, this.duration, this.color);
  final Json step;
  final bool selected;
  final double duration;
  final Color color;
  @override
  void paint(Canvas canvas, Size size) {
    final start = (step['at_us'] as num? ?? 0) / 1000;
    final width = math.max(
      6.0,
      (step['duration_ms'] as num? ?? 50) / duration * size.width,
    );
    final repeats = (step['repeat_count'] as num? ?? 1).toInt().clamp(1, 64);
    canvas.save();
    canvas.clipRect(Offset.zero & size);
    for (var repeat = 0; repeat < repeats; repeat++) {
      final offset = start + repeat * (step['repeat_interval_ms'] as num? ?? 0);
      final rect = RRect.fromRectAndRadius(
        Rect.fromLTWH(offset / duration * size.width, 5, width, 22),
        const Radius.circular(4),
      );
      canvas.drawRRect(
        rect,
        Paint()
          ..color = color.withValues(
            alpha: repeat == 0
                ? selected
                      ? 1
                      : .65
                : .2,
          ),
      );
    }
    canvas.restore();
  }

  @override
  bool shouldRepaint(StepPainter old) =>
      old.step != step ||
      old.selected != selected ||
      old.duration != duration ||
      old.color != color;
}
