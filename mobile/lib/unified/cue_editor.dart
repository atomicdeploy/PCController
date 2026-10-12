import 'dart:async';

import 'package:flutter/material.dart';

import 'transport.dart' show Json;

int? cueSecondsToMs(String text) {
  final match = RegExp(r'^\+?(?:(\d+)(?:\.(\d*))?|\.(\d+))$')
      .firstMatch(text.trim());
  if (match == null) return null;
  final fraction = (match.group(2) ?? match.group(3) ?? '').padRight(4, '0');
  final milliseconds =
      BigInt.parse(match.group(1) ?? '0') * BigInt.from(1000) +
      BigInt.from(int.parse(fraction.substring(0, 3))) +
      BigInt.from(int.parse(fraction[3]) >= 5 ? 1 : 0);
  return milliseconds <= BigInt.parse('9223372036854775807')
      ? milliseconds.toInt()
      : null;
}

String cueStartText(int milliseconds) {
  final fraction = (milliseconds % 1000)
      .toString()
      .padLeft(3, '0')
      .replaceFirst(RegExp(r'0+$'), '');
  return '${milliseconds ~/ 1000}${fraction.isEmpty ? '' : '.$fraction'}';
}

class CueEditor extends StatefulWidget {
  const CueEditor({super.key, required this.cue, required this.save});
  final Json cue;
  final Future<void> Function(int startMs, int durationMs) save;
  @override
  State<CueEditor> createState() => _CueEditorState();
}

class _CueEditorState extends State<CueEditor> {
  final form = GlobalKey<FormState>();
  late final start = TextEditingController(
    text: cueStartText(widget.cue['start_time_ms'] as int),
  );
  late final duration = TextEditingController(
    text: '${widget.cue['duration_ms']}',
  );
  bool saving = false;
  String? error;
  @override
  void dispose() {
    start.dispose();
    duration.dispose();
    super.dispose();
  }

  bool get dirty =>
      start.text != cueStartText(widget.cue['start_time_ms'] as int) ||
      duration.text != '${widget.cue['duration_ms']}';
  Future<void> close() async {
    if (saving) return;
    if (dirty) {
      final discard = await showDialog<bool>(
        context: context,
        builder: (dialog) => AlertDialog(
          title: const Text('Unsaved cue'),
          content: const Text('Discard cue edits?'),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog, false),
              child: const Text('Keep editing'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: const Text('Discard'),
            ),
          ],
        ),
      );
      if (discard != true) return;
    }
    if (mounted) Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) => PopScope(
    canPop: false,
    onPopInvokedWithResult: (didPop, _) {
      if (!didPop) unawaited(close());
    },
    child: AlertDialog(
      title: Row(
        children: [
          Expanded(
            child: Text(
              '${widget.cue['name']}',
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          IconButton(
            tooltip: 'Close',
            onPressed: saving ? null : () => unawaited(close()),
            icon: const Icon(Icons.close),
          ),
        ],
      ),
      content: SizedBox(
        width: 420,
        child: SingleChildScrollView(
          child: Form(
            key: form,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                TextFormField(
                  controller: start,
                  keyboardType: const TextInputType.numberWithOptions(
                    decimal: true,
                  ),
                  enabled: !saving,
                  decoration: const InputDecoration(
                    labelText: 'Start (seconds)',
                  ),
                  validator: (value) {
                    return cueSecondsToMs(value ?? '') == null
                        ? 'Enter a non-negative time in seconds'
                        : null;
                  },
                ),
                const SizedBox(height: 16),
                TextFormField(
                  controller: duration,
                  keyboardType: TextInputType.number,
                  enabled: !saving && widget.cue['resizable'] == true,
                  decoration: const InputDecoration(labelText: 'Duration (ms)'),
                  validator: (value) {
                    final number = int.tryParse(value ?? '');
                    return widget.cue['resizable'] == true &&
                            (number == null || number < 1 || number > 86400000)
                        ? 'Enter a duration from 1 ms to 24 hours'
                        : null;
                  },
                ),
                if (error != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 16),
                    child: Text(
                      error!,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ),
                if (saving)
                  const Padding(
                    padding: EdgeInsets.only(top: 16),
                    child: LinearProgressIndicator(),
                  ),
              ],
            ),
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: saving ? null : () => unawaited(close()),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: saving
              ? null
              : () async {
                  if (!form.currentState!.validate()) return;
                  final startMs = cueSecondsToMs(start.text)!;
                  final durationMs = int.parse(duration.text);
                  if (startMs == widget.cue['start_time_ms'] &&
                      durationMs == widget.cue['duration_ms']) {
                    Navigator.pop(context);
                    return;
                  }
                  setState(() {
                    saving = true;
                    error = null;
                  });
                  try {
                    await widget.save(startMs, durationMs);
                    if (context.mounted) Navigator.pop(context);
                  } catch (e) {
                    if (mounted) {
                      setState(() {
                        saving = false;
                        error = '$e';
                      });
                    }
                  }
                },
          child: Text(saving ? 'Saving…' : 'Save'),
        ),
      ],
    ),
  );
}
