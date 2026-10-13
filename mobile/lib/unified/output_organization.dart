import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'transport.dart' show Json, objects, Rejected;

/// The host reorders one key atomically within its advertised control kind.
List<Json> orderedOutputs(List<Json> controls) {
  final kinds = <String>[];
  for (final control in controls) {
    if (!kinds.contains(control['kind'])) kinds.add('${control['kind']}');
  }
  final sorted = List<Json>.of(controls);
  sorted.sort((a, b) {
    final kind = kinds
        .indexOf('${a['kind']}')
        .compareTo(kinds.indexOf('${b['kind']}'));
    if (kind != 0) return kind;
    final order = (a['order'] is int ? a['order'] as int : controls.indexOf(a))
        .compareTo(b['order'] is int ? b['order'] as int : controls.indexOf(b));
    return order != 0 ? order : '${a['key']}'.compareTo('${b['key']}');
  });
  return sorted;
}

class OutputDrag {
  const OutputDrag(this.target, this.key, this.kind, this.group);
  final String target, key, kind, group;
}

Json outputMove(List<Json> controls, OutputDrag drag, Json destination) {
  final peers = objects(controls).where((c) => c['kind'] == drag.kind).toList();
  if (destination['kind'] != drag.kind ||
      peers.any((c) => c['order'] is! int) ||
      !peers.any((c) => c['key'] == drag.key && c['hidden'] != true) ||
      !peers.any(
        (c) => c['key'] == destination['key'] && c['hidden'] != true,
      )) {
    throw const Rejected('Channel layout changed. Start the drag again.');
  }
  peers.sort((a, b) {
    final order = (a['order'] as int).compareTo(b['order'] as int);
    return order != 0 ? order : '${a['key']}'.compareTo('${b['key']}');
  });
  final source = peers.firstWhere((c) => c['key'] == drag.key);
  final group = '${destination['group'] ?? ''}';
  if (source['control'] == 'seat-internal' && group != drag.group) {
    throw const Rejected(
      'Raw seat wiring cannot move into an operator folder.',
    );
  }
  return {
    'order': peers.indexWhere((c) => c['key'] == destination['key']),
    if (group != drag.group) 'group': group,
  };
}

class OutputDragHandle extends StatefulWidget {
  const OutputDragHandle({
    super.key,
    required this.drag,
    required this.name,
    required this.enabled,
    required this.icon,
    this.color,
    this.tile = true,
  });
  final OutputDrag drag;
  final String name;
  final bool enabled;
  final IconData icon;
  final Color? color;
  final bool tile;
  @override
  State<OutputDragHandle> createState() => _OutputDragHandleState();
}

class _OutputDragHandleState extends State<OutputDragHandle> {
  String get name => widget.name;
  bool get enabled => widget.enabled;
  bool get tile => widget.tile;
  Color? get color => widget.color;
  IconData get icon => widget.icon;
  OutputDrag get drag => widget.drag;
  EdgeDraggingAutoScroller? scroller;
  void scrollDrag(DragUpdateDetails details) {
    if (!enabled) {
      scroller?.stopAutoScroll();
      return;
    }
    final scrollable = Scrollable.maybeOf(context);
    if (scrollable == null) return;
    scroller ??= EdgeDraggingAutoScroller(scrollable, velocityScalar: 12);
    scroller!.startAutoScrollIfNecessary(
      Rect.fromCenter(center: details.globalPosition, width: 44, height: 44),
    );
  }

  @override
  void didUpdateWidget(covariant OutputDragHandle oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!enabled) scroller?.stopAutoScroll();
  }

  @override
  void dispose() {
    scroller?.stopAutoScroll();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final handle = Tooltip(
      message: 'Reorder $name',
      child: Semantics(
        label: 'Drag to reorder $name',
        enabled: enabled,
        child: SizedBox(
          width: 44,
          height: 44,
          child: Stack(
            alignment: Alignment.center,
            children: [
              Container(
                width: 32,
                height: 32,
                decoration: BoxDecoration(
                  color: tile
                      ? (color ?? Theme.of(context).colorScheme.primary)
                            .withValues(alpha: .10)
                      : Colors.transparent,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Icon(
                  icon,
                  size: 20,
                  color: color ?? Theme.of(context).colorScheme.primary,
                ),
              ),
              if (enabled)
                Positioned(
                  right: 0,
                  bottom: 1,
                  child: Icon(
                    Icons.drag_indicator,
                    size: 12,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
            ],
          ),
        ),
      ),
    );
    return LongPressDraggable<OutputDrag>(
      data: drag,
      maxSimultaneousDrags: enabled ? 1 : 0,
      delay: const Duration(milliseconds: 200),
      dragAnchorStrategy: pointerDragAnchorStrategy,
      onDragUpdate: scrollDrag,
      onDragEnd: (_) => scroller?.stopAutoScroll(),
      onDraggableCanceled: (_, _) => scroller?.stopAutoScroll(),
      feedback: Material(
        elevation: 4,
        borderRadius: BorderRadius.circular(10),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.drag_indicator, size: 20),
              const SizedBox(width: 8),
              ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 240),
                child: Text(name, maxLines: 1, overflow: TextOverflow.ellipsis),
              ),
            ],
          ),
        ),
      ),
      child: handle,
    );
  }
}

class OutputDropCard extends StatelessWidget {
  const OutputDropCard({
    super.key,
    required this.control,
    required this.target,
    required this.enabled,
    required this.dropped,
    required this.child,
  });
  final Json control;
  final String target;
  final bool enabled;
  final ValueChanged<OutputDrag> dropped;
  final Widget child;
  @override
  Widget build(BuildContext context) => DragTarget<OutputDrag>(
    onWillAcceptWithDetails: (details) =>
        enabled &&
        details.data.target == target &&
        details.data.kind == control['kind'] &&
        details.data.key != control['key'],
    onAcceptWithDetails: (details) => dropped(details.data),
    builder: (context, candidates, rejected) => DecoratedBox(
      decoration: BoxDecoration(
        border: Border.all(
          color: candidates.isEmpty
              ? Colors.transparent
              : Theme.of(context).colorScheme.primary,
          width: 2,
        ),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Padding(padding: const EdgeInsets.all(2), child: child),
    ),
  );
}

class InlineOutputName extends StatefulWidget {
  const InlineOutputName({
    super.key,
    required this.name,
    required this.enabled,
    required this.style,
    required this.commit,
  });
  final String name;
  final bool enabled;
  final TextStyle? style;
  final Future<void> Function(String) commit;
  @override
  State<InlineOutputName> createState() => InlineOutputNameState();
}

class InlineOutputNameState extends State<InlineOutputName> {
  final text = TextEditingController();
  final focus = FocusNode();
  bool editing = false, pending = false;
  String? error;
  late Future<void> Function(String) commit;
  void report(String message) {
    setState(() => error = message);
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));
  }

  void start() {
    if (!widget.enabled || pending) return;
    commit =
        widget.commit; // Pin owner/revision at edit start, not each rebuild.
    text.text = widget.name;
    text.selection = TextSelection(
      baseOffset: 0,
      extentOffset: text.text.length,
    );
    setState(() {
      editing = true;
      error = null;
    });
    focus.requestFocus();
  }

  void cancel() {
    if (!pending) {
      setState(() {
        editing = false;
        error = null;
      });
    }
  }

  Future<void> save() async {
    if (pending) return;
    final name = text.text.trim();
    if (name.isEmpty ||
        name.runes.length > 64 ||
        name.runes.any((r) => r < 32 || r == 127)) {
      report('Use 1–64 printable characters.');
      return;
    }
    if (name == widget.name) {
      cancel();
      return;
    }
    setState(() {
      pending = true;
      error = null;
    });
    try {
      await commit(name);
      if (mounted) setState(() => editing = false);
    } catch (failure) {
      if (mounted) report('$failure');
    } finally {
      if (mounted) setState(() => pending = false);
    }
  }

  @override
  void dispose() {
    text.dispose();
    focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => !editing
      ? Tooltip(
          message: widget.enabled ? 'Rename ${widget.name}' : '',
          child: InkWell(
            onTap: widget.enabled ? start : null,
            child: ConstrainedBox(
              constraints: const BoxConstraints(minHeight: 44),
              child: Align(
                alignment: AlignmentDirectional.centerStart,
                child: Text(
                  widget.name,
                  style: widget.style,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  strutStyle: StrutStyle.fromTextStyle(
                    widget.style ?? const TextStyle(),
                    height: 1,
                    forceStrutHeight: true,
                  ),
                  textHeightBehavior: const TextHeightBehavior(
                    applyHeightToFirstAscent: false,
                    applyHeightToLastDescent: false,
                  ),
                ),
              ),
            ),
          ),
        )
      : CallbackShortcuts(
          bindings: {const SingleActivator(LogicalKeyboardKey.escape): cancel},
          child: Tooltip(
            message: error ?? '',
            child: SizedBox(
              height: 44,
              child: TextField(
                controller: text,
                focusNode: focus,
                autofocus: true,
                enabled: !pending,
                style: widget.style,
                maxLines: 1,
                strutStyle: StrutStyle.fromTextStyle(
                  widget.style ?? const TextStyle(),
                  height: 1,
                  forceStrutHeight: true,
                ),
                textAlignVertical: TextAlignVertical.center,
                textInputAction: TextInputAction.done,
                decoration: InputDecoration(
                  isDense: true,
                  contentPadding: EdgeInsets.zero,
                  border: InputBorder.none,
                  suffixIconConstraints: const BoxConstraints.tightFor(
                    width: 44,
                    height: 44,
                  ),
                  suffixIcon: pending
                      ? const Center(
                          child: SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                        )
                      : IconButton(
                          tooltip: 'Save channel name',
                          onPressed: save,
                          icon: const Icon(Icons.check, size: 18),
                          style: IconButton.styleFrom(
                            minimumSize: const Size(44, 44),
                          ),
                        ),
                ),
                onSubmitted: (_) => save(),
              ),
            ),
          ),
        );
}
