import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'transport.dart';
import 'sequence_timeline.dart';
import 'controller_dialog.dart';
import 'strip_designer.dart';

class EffectEditor extends StatefulWidget {
  const EffectEditor({
    super.key,
    required this.initial,
    required this.saved,
    this.folders = const [],
  });
  final Json initial;
  final List<String> folders;
  final Future<void> Function(Json) saved;
  @override
  State<EffectEditor> createState() => _EffectEditorState();
}

class _EffectEditorState extends State<EffectEditor> {
  final form = GlobalKey<FormState>();
  late Json effect;
  late TextEditingController name, id, raw;
  late String folder;
  int selected = -1, timingRevision = 0;
  bool saving = false;
  String? error;
  final invalidNumbers = <String, String>{};
  final numberDrafts = <String, String>{};
  final addedFolders = <String>{};
  late String appliedRaw;
  @override
  void initState() {
    super.initState();
    effect = object(jsonDecode(jsonEncode(widget.initial)));
    name = TextEditingController(text: '${effect['name'] ?? ''}');
    id = TextEditingController(text: '${effect['id'] ?? ''}');
    folder = '${effect['category'] ?? 'Effects'}';
    raw = TextEditingController(
      text: const JsonEncoder.withIndent('  ').convert(effect),
    );
    appliedRaw = raw.text;
  }

  @override
  void dispose() {
    name.dispose();
    id.dispose();
    raw.dispose();
    super.dispose();
  }

  List<Json> get steps => objects(effect['steps']);
  bool get typedEditing => !saving && raw.text == appliedRaw;
  void syncRaw() {
    appliedRaw = const JsonEncoder.withIndent('  ').convert(effect);
    raw.text = appliedRaw;
  }

  bool get dirty =>
      jsonEncode(effect) != jsonEncode(widget.initial) ||
      name.text != '${widget.initial['name'] ?? ''}' ||
      id.text != '${widget.initial['id'] ?? ''}' ||
      folder != '${widget.initial['category'] ?? 'Effects'}' ||
      raw.text != appliedRaw ||
      invalidNumbers.isNotEmpty;
  Future<void> dismiss() async {
    if (saving) return;
    if (dirty) {
      final discard = await showDialog<bool>(
        context: context,
        builder: (dialog) => ControllerDialog(
          title: 'Unsaved effect',
          icon: Icons.edit_note,
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog, false),
              child: const Text('Keep editing'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: const Text('Discard changes'),
            ),
          ],
          child: const Text('Discard editor changes?'),
        ),
      );
      if (discard != true) return;
    }
    if (mounted) Navigator.pop(context);
  }

  void changeStep(String key, dynamic value) {
    if (!typedEditing || selected < 0 || selected >= steps.length) return;
    final next = steps;
    next[selected] = {...next[selected], key: value};
    setState(() {
      effect['steps'] = next;
      syncRaw();
    });
  }

  String? numberError(String key, String text, num scale) {
    final parsed = num.tryParse(text.trim());
    final maximum = switch (key) {
      'repeat_count' => 1000,
      'repeat_interval_ms' => 3600000,
      'duration_ms' => 65535,
      'frequency_hz' => 20000,
      _ => 2147483647,
    };
    final minimum =
        key == 'repeat_count' ||
            key == 'duration_ms' &&
                selected >= 0 &&
                ['buzzer', 'beep', 'tone'].contains(steps[selected]['kind'])
        ? 1
        : 0;
    if (parsed == null ||
        !parsed.isFinite ||
        parsed * scale < minimum ||
        parsed * scale > maximum ||
        scale == 1 && parsed != parsed.roundToDouble()) {
      return 'Enter ${minimum / scale}–${maximum / scale}${scale == 1 ? ' (whole number)' : ''}';
    }
    if (key == 'frequency_hz' && parsed > 0 && parsed < 20) {
      return 'Use 0 or 20–20000 Hz';
    }
    return null;
  }

  Widget number(String label, String key, num value, {num scale = 1}) =>
      SizedBox(
        width: 166,
        child: TextFormField(
          key: ValueKey('$selected/$key/$timingRevision'),
          initialValue: numberDrafts['$selected/$key'] ?? '${value / scale}',
          enabled: typedEditing,
          autovalidateMode: AutovalidateMode.onUserInteraction,
          keyboardType: TextInputType.numberWithOptions(decimal: scale != 1),
          decoration: InputDecoration(labelText: label),
          validator: (text) => numberError(key, text ?? '', scale),
          onChanged: (text) {
            final issue = numberError(key, text, scale),
                stamp = '$selected/$key';
            numberDrafts[stamp] = text;
            if (issue != null) {
              setState(() => invalidNumbers[stamp] = issue);
              return;
            }
            invalidNumbers.remove(stamp);
            changeStep(key, (num.parse(text) * scale).round());
          },
        ),
      );
  Future<void> newFolder() async {
    final input = TextEditingController();
    final result = await showDialog<String>(
      context: context,
      builder: (dialog) => ControllerDialog(
        title: 'New effect folder',
        icon: Icons.create_new_folder_outlined,
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () {
              if (input.text.trim().isNotEmpty) {
                Navigator.pop(dialog, input.text.trim());
              }
            },
            child: const Text('Create'),
          ),
        ],
        child: TextField(
          controller: input,
          autofocus: true,
          maxLength: 64,
          decoration: const InputDecoration(labelText: 'Folder name'),
          onSubmitted: (value) {
            if (value.trim().isNotEmpty) Navigator.pop(dialog, value.trim());
          },
        ),
      ),
    );
    input.dispose();
    if (result != null && mounted) {
      setState(() {
        addedFolders.add(result);
        folder = result;
      });
    }
  }

  Future<void> save() async {
    if (saving || !form.currentState!.validate()) return;
    if (invalidNumbers.isNotEmpty) {
      setState(() => error = 'Correct the invalid step values before saving.');
      return;
    }
    if (raw.text != appliedRaw) {
      setState(() => error = 'Apply or revert technical edits before saving.');
      return;
    }
    final definitionError = effectDefinitionError(effect);
    if (definitionError != null) {
      setState(() => error = definitionError);
      return;
    }
    if (!dirty) {
      Navigator.pop(context);
      return;
    }
    setState(() {
      saving = true;
      error = null;
    });
    try {
      await widget.saved({
        ...effect,
        'id': id.text.trim(),
        'name': name.text.trim(),
        'category': folder,
        'engine': 'host',
        'editable': true,
      });
      if (mounted) Navigator.pop(context);
    } catch (e) {
      if (mounted) {
        setState(() {
          saving = false;
          error = '$e';
        });
      }
    }
  }

  void addStep(String kind) {
    setState(() {
      final next = steps;
      next.add({
        'kind': kind,
        'at_us': effectSpanUs(next),
        'duration_ms': kind == 'buzzer' ? 115 : 1000,
        if (kind == 'buzzer') 'frequency_hz': 880,
        if (kind == 'display') ...{'text': 'HELLO', 'destination': 'segments'},
        if (kind == 'rgb') ...{
          'red': 128,
          'green': 0,
          'blue': 255,
          'brightness': 255,
        },
      });
      effect['steps'] = next;
      selected = next.length - 1;
      syncRaw();
    });
  }

  @override
  Widget build(BuildContext context) {
    final current = selected >= 0 && selected < steps.length
        ? steps[selected]
        : <String, dynamic>{};
    final folders = {
      '',
      folder,
      'Effects',
      ...widget.folders,
      ...addedFolders,
    }.toList()..sort();
    final colors = Theme.of(context).colorScheme;
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) unawaited(dismiss());
      },
      child: ControllerDialog(
        title: 'Effect designer',
        icon: Icons.auto_awesome_outlined,
        maxWidth: 1000,
        closeEnabled: !saving,
        onClose: () => unawaited(dismiss()),
        actions: [
          TextButton(
            onPressed: saving ? null : () => unawaited(dismiss()),
            child: const Text('Cancel'),
          ),
          FilledButton.icon(
            onPressed: saving ? null : () => unawaited(save()),
            icon: const Icon(Icons.save_outlined, size: 18),
            label: Text(saving ? 'Saving…' : 'Save local draft'),
          ),
        ],
        child: Form(
          key: form,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              LayoutBuilder(
                builder: (context, bounds) => Wrap(
                  spacing: 12,
                  runSpacing: 12,
                  children: [
                    SizedBox(
                      width: bounds.maxWidth < 580
                          ? bounds.maxWidth
                          : (bounds.maxWidth - 12) / 2,
                      child: TextFormField(
                        controller: name,
                        enabled: typedEditing,
                        decoration: const InputDecoration(labelText: 'Name'),
                        validator: (value) =>
                            value!.trim().isEmpty ? 'Enter a name' : null,
                      ),
                    ),
                    SizedBox(
                      width: bounds.maxWidth < 580
                          ? bounds.maxWidth
                          : (bounds.maxWidth - 12) / 2,
                      child: TextFormField(
                        controller: id,
                        enabled: typedEditing,
                        decoration: const InputDecoration(
                          labelText: 'Effect ID',
                        ),
                        validator: (value) =>
                            value!.trim().isEmpty ? 'Enter an effect ID' : null,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: DropdownButtonFormField<String>(
                      key: ValueKey(folder),
                      initialValue: folder,
                      decoration: const InputDecoration(
                        labelText: 'Folder',
                        prefixIcon: Icon(Icons.folder_outlined),
                      ),
                      isExpanded: true,
                      items: [
                        for (final value in folders)
                          DropdownMenuItem(
                            value: value,
                            child: Text(
                              value.isEmpty ? 'No folder' : value,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                      ],
                      onChanged: !typedEditing
                          ? null
                          : (value) => setState(() => folder = value!),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton(
                    tooltip: 'New effect folder',
                    onPressed: !typedEditing
                        ? null
                        : () => unawaited(newFolder()),
                    icon: const Icon(Icons.create_new_folder_outlined),
                  ),
                ],
              ),
              if (effect['kind'] == 'strip-stream') StripDesigner(effect: effect, enabled: typedEditing, changed: (value) => setState(() { effect = value; syncRaw(); })),
              if (effect['kind'] == 'sequence') ...[
                const SizedBox(height: 20),
                SequenceTimeline(
                  steps: steps,
                  selected: selected,
                  enabled: typedEditing,
                  select: (index) => setState(() => selected = index),
                  edit: (index, start, length) {
                    if (saving || index < 0 || index >= steps.length) return;
                    final next = steps;
                    next[index] = {
                      ...next[index],
                      'at_us': start,
                      'duration_ms': ?length,
                    };
                    setState(() {
                      timingRevision++;
                      effect['steps'] = next;
                      syncRaw();
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
                        onPressed: !typedEditing ? null : () => addStep(kind),
                        icon: const Icon(Icons.add, size: 18),
                        label: Text(
                          {
                            'buzzer': 'Tone',
                            'display': 'Display',
                            'rgb': 'RGB',
                          }[kind]!,
                        ),
                      ),
                  ],
                ),
                if (steps.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    children: [
                      for (var i = 0; i < steps.length; i++)
                        ChoiceChip(
                          label: Text('${i + 1} · ${steps[i]['kind']}'),
                          selected: selected == i,
                          avatar:
                              invalidNumbers.keys.any(
                                (key) => key.startsWith('$i/'),
                              )
                              ? Icon(
                                  Icons.error_outline,
                                  color: colors.error,
                                  size: 16,
                                )
                              : null,
                          onSelected: !typedEditing
                              ? null
                              : (_) => setState(() => selected = i),
                        ),
                    ],
                  ),
                ],
                if (current.isNotEmpty) ...[
                  const SizedBox(height: 16),
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
                        enabled: typedEditing,
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
                      ('brightness', 'Brightness', colors.primary),
                    ])
                      Row(
                        children: [
                          SizedBox(width: 78, child: Text(entry.$2)),
                          Expanded(
                            child: Slider(
                              min: 0,
                              max: 255,
                              divisions: 255,
                              activeColor: entry.$3,
                              value: (current[entry.$1] as num? ?? 0)
                                  .clamp(0, 255)
                                  .toDouble(),
                              onChanged: !typedEditing
                                  ? null
                                  : (value) =>
                                        changeStep(entry.$1, value.round()),
                            ),
                          ),
                          SizedBox(
                            width: 30,
                            child: Text('${current[entry.$1] ?? 0}'),
                          ),
                        ],
                      ),
                    Wrap(
                      spacing: 6,
                      children: [
                        for (final color in [
                          Colors.red,
                          Colors.green,
                          Colors.blue,
                          const Color(0xffaa70dd),
                          Colors.white,
                        ])
                          IconButton(
                            tooltip:
                                '#${color.toARGB32().toRadixString(16).substring(2).toUpperCase()}',
                            icon: Icon(Icons.circle, color: color),
                            onPressed: !typedEditing
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
                                      syncRaw();
                                    });
                                  },
                          ),
                      ],
                    ),
                  ],
                  TextButton.icon(
                    onPressed: !typedEditing
                        ? null
                        : () {
                            final next = steps..removeAt(selected);
                            setState(() {
                              effect['steps'] = next;
                              for (final map in [
                                invalidNumbers,
                                numberDrafts,
                              ]) {
                                final retained = <String, String>{};
                                for (final entry in map.entries) {
                                  final parts = entry.key.split('/'),
                                      index = int.parse(parts[0]);
                                  if (index != selected) {
                                    retained['${index > selected ? index - 1 : index}/${parts[1]}'] =
                                        entry.value;
                                  }
                                }
                                map
                                  ..clear()
                                  ..addAll(retained);
                              }
                              selected = -1;
                              syncRaw();
                            });
                          },
                    icon: const Icon(Icons.delete_outline),
                    label: const Text('Remove selected step'),
                  ),
                ],
              ],
              const SizedBox(height: 12),
              ExpansionTile(
                tilePadding: EdgeInsets.zero,
                title: const Text('Technical definition'),
                children: [
                  TextField(
                    controller: raw,
                    onChanged: (_) => setState(() {}),
                    enabled: !saving,
                    minLines: 5,
                    maxLines: 14,
                    style: const TextStyle(
                      fontFamily: 'DroidSansMono',
                      fontSize: 12,
                    ),
                    decoration: const InputDecoration(labelText: 'Effect JSON'),
                  ),
                  Wrap(
                    spacing: 8,
                    children: [
                      TextButton(
                        onPressed: saving
                            ? null
                            : () {
                                try {
                                  final value = jsonDecode(raw.text);
                                  if (value is! Map) {
                                    throw const FormatException();
                                  }
                                  setState(() {
                                    final problem = effectDefinitionError(
                                      object(value),
                                    );
                                    if (problem != null) {
                                      throw FormatException(problem);
                                    }
                                    effect = object(value);
                                    selected = -1;
                                    invalidNumbers.clear();
                                    numberDrafts.clear();
                                    name.text = '${effect['name'] ?? ''}';
                                    id.text = '${effect['id'] ?? ''}';
                                    folder = '${effect['category'] ?? ''}';
                                    error = null;
                                    syncRaw();
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
                      TextButton(
                        onPressed: saving ? null : () => setState(syncRaw),
                        child: const Text('Revert technical edits'),
                      ),
                    ],
                  ),
                ],
              ),
              if (error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text(error!, style: TextStyle(color: colors.error)),
                ),
              const SizedBox(height: 12),
              SizedBox(
                height: 2,
                child: saving
                    ? const LinearProgressIndicator(minHeight: 2)
                    : null,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

int effectSpanUs(List<Json> steps) => steps.fold<int>(0, (span, step) {
  final duration = (step['duration_ms'] as num? ?? 0).toInt();
  final repeats = (step['repeat_count'] as num? ?? 1).toInt();
  final interval = (step['repeat_interval_ms'] as num? ?? 0).toInt();
  return math
      .max(
        span,
        (step['at_us'] as num? ?? 0).toInt() +
            duration * 1000 +
            (repeats - 1) *
                (interval == 0 ? math.max(duration, 1) : interval) *
                1000,
      )
      .toInt();
});

String? effectDefinitionError(Json effect) {
  if (effect['kind'] == 'strip-stream') {
    for(final entry in {'default_pixels':100,'default_fps':30,'duration_ms':3600000}.entries) {
      final v=effect[entry.key];
      if(v is! int || v < (entry.key=='duration_ms'?100:1) || v > entry.value)return 'Invalid ${entry.key.replaceAll('_',' ')}.';
    }
    return stripProgramError(object(effect['program']));
  }
  if (effect['steps'] != null &&
      (effect['steps'] is! List ||
          (effect['steps'] as List).any((value) => value is! Map))) {
    return 'Steps must be a list of objects.';
  }
  final steps = objects(effect['steps']);
  for (var index = 0; index < steps.length; index++) {
    final step = steps[index];
    for (final entry in {
      'at_us': 2147483647,
      'duration_ms': 65535,
      'repeat_count': 1000,
      'repeat_interval_ms': 3600000,
      'frequency_hz': 20000,
      'red': 255,
      'green': 255,
      'blue': 255,
      'brightness': 255,
    }.entries) {
      final value = step[entry.key];
      if (value != null &&
          (value is! num ||
              !value.isFinite ||
              value != value.roundToDouble() ||
              value < 0 ||
              value > entry.value ||
              entry.key == 'repeat_count' && value < 1)) {
        return 'Step ${index + 1}: invalid ${entry.key.replaceAll('_', ' ')}.';
      }
    }
    if (['buzzer', 'beep', 'tone'].contains(step['kind'])) {
      final frequency =
          step['frequency_hz'] as num? ?? step['value'] as num? ?? 0;
      if ((step['duration_ms'] as num? ?? 0) < 1 ||
          frequency != 0 && (frequency < 20 || frequency > 20000)) {
        return 'Step ${index + 1}: use a duration and 0 or 20–20000 Hz.';
      }
    }
    if (['display', 'message'].contains(step['kind'])) {
      final text = step['text'];
      if (text is! String ||
          text.length > 40 ||
          text.codeUnits.any((c) => c < 32 || c > 126)) {
        return 'Step ${index + 1}: use up to 40 printable ASCII characters.';
      }
    }
    if (effectSpanUs([step]) > 2147483647) {
      return 'Step ${index + 1}: repetition exceeds 2147.483647 seconds.';
    }
  }
  return null;
}

double sequenceDuration(List<Json> steps) =>
    math.max(1000, effectSpanUs(steps) / 1000);

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
