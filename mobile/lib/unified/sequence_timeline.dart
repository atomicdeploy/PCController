import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'transport.dart' show Json;

double stepStartMs(Json step) =>
    boundedStepNumber(step['at_us'], 0, 4294967295) / 1000;
double stepLengthMs(Json step) =>
    boundedStepNumber(step['duration_ms'], 0, 4294967);
double boundedStepNumber(Object? value, double min, double max) =>
    value is num && value.isFinite ? value.toDouble().clamp(min, max) : min;
double timelineExtent(List<Json> steps) => steps.fold<double>(
  1000,
  (end, step) => math.max(
    end,
    stepStartMs(step) +
        stepLengthMs(step) +
        (boundedStepNumber(step['repeat_count'], 1, 64) - 1) *
            boundedStepNumber(step['repeat_interval_ms'], 0, 4294967),
  ),
);

/// Native authoring only. Parent retains descriptors and commits a single timing
/// edit at release. A changed descriptor invalidates a drag instead of retargeting it.
class SequenceTimeline extends StatefulWidget {
  const SequenceTimeline({
    super.key,
    required this.steps,
    required this.selected,
    required this.select,
    required this.edit,
    this.enabled = true,
  });
  final List<Json> steps;
  final int selected;
  final ValueChanged<int> select;
  final void Function(int index, int startUs, int? durationMs) edit;
  final bool enabled;
  @override
  State<SequenceTimeline> createState() => _SequenceTimelineState();
}

class _SequenceTimelineState extends State<SequenceTimeline> {
  final scroll = ScrollController();
  double zoom = 1;
  bool snap = true;
  int? dragging;
  int initialStepCount = 0;
  String? fingerprint;
  double delta = 0, originalStart = 0, originalLength = 0;
  double? draftStart, draftLength;
  bool resizing = false;
  @override
  void dispose() {
    scroll.dispose();
    super.dispose();
  }

  void begin(int index, bool resize) {
    if (!widget.enabled) return;
    dragging = index;
    initialStepCount = widget.steps.length;
    fingerprint = jsonEncode(widget.steps[index]);
    originalStart = stepStartMs(widget.steps[index]);
    originalLength = stepLengthMs(widget.steps[index]);
    delta = 0;
    resizing = resize;
    widget.select(index);
    setState(() {
      draftStart = originalStart;
      draftLength = originalLength;
    });
  }

  void update(DragUpdateDetails details, double pxPerMs) {
    if (dragging == null || !widget.enabled) return;
    delta += details.delta.dx / pxPerMs;
    double quantize(double value) => snap ? (value / 10).round() * 10.0 : value;
    setState(() {
      if (resizing) {
        draftLength = quantize(originalLength + delta).clamp(1, 4294967);
      } else {
        draftStart = quantize(originalStart + delta).clamp(0, 4294967.295);
      }
    });
  }

  void finish() {
    final index = dragging, start = draftStart, length = draftLength;
    if (widget.enabled &&
        index != null &&
        index < widget.steps.length &&
        initialStepCount == widget.steps.length &&
        start != null &&
        length != null &&
        fingerprint == jsonEncode(widget.steps[index])) {
      widget.edit(
        index,
        (start * 1000).round().clamp(0, 4294967295),
        resizing ? length.round() : null,
      );
    }
    cancel();
  }

  void cancel() {
    if (mounted) {
      setState(() {
        dragging = null;
        fingerprint = null;
        draftStart = draftLength = null;
      });
    }
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, bounds) {
      const labelWidth = 86.0;
      final viewport = math.max(120.0, bounds.maxWidth - labelWidth);
      final extent = timelineExtent(widget.steps);
      final pxPerMs = viewport / (extent + 200) * zoom;
      final canvasWidth = math.max(viewport, (extent + 200) * pxPerMs);
      final colors = Theme.of(context).colorScheme;
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            crossAxisAlignment: WrapCrossAlignment.center,
            spacing: 4,
            children: [
              Text('Timeline', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(width: 8),
              IconButton(
                tooltip: 'Zoom out',
                onPressed: zoom > 1
                    ? () => setState(() => zoom = math.max(1, zoom / 1.5))
                    : null,
                icon: const Icon(Icons.zoom_out),
              ),
              IconButton(
                tooltip: 'Zoom in',
                onPressed: zoom < 64
                    ? () => setState(() => zoom = math.min(64, zoom * 1.5))
                    : null,
                icon: const Icon(Icons.zoom_in),
              ),
              TextButton(
                onPressed: () {
                  cancel();
                  setState(() => zoom = 1);
                  if (scroll.hasClients) scroll.jumpTo(0);
                },
                child: const Text('Fit'),
              ),
              FilterChip(
                label: const Text('Snap 10 ms'),
                selected: snap,
                onSelected: (v) => setState(() => snap = v),
              ),
            ],
          ),
          const SizedBox(height: 8),
          ClipRRect(
            borderRadius: BorderRadius.circular(8),
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: colors.surfaceContainer,
                border: Border.all(color: colors.outlineVariant),
              ),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 320),
                child: SingleChildScrollView(
                  child: Directionality(
                    textDirection: TextDirection.ltr,
                    child: Scrollbar(
                      controller: scroll,
                      thumbVisibility: zoom > 1,
                      child: SingleChildScrollView(
                        controller: scroll,
                        scrollDirection: Axis.horizontal,
                        child: SizedBox(
                          width: labelWidth + canvasWidth,
                          child: Column(
                            children: [
                              Row(
                                children: [
                                  const SizedBox(width: labelWidth),
                                  SizedBox(
                                    width: canvasWidth,
                                    height: 32,
                                    child: CustomPaint(
                                      painter: TimelineRuler(
                                        pxPerMs,
                                        colors.outlineVariant,
                                        Theme.of(context).textTheme.labelSmall!
                                            .copyWith(
                                              color: colors.onSurfaceVariant,
                                            ),
                                      ),
                                    ),
                                  ),
                                ],
                              ),
                              if (widget.steps.isEmpty)
                                const Padding(
                                  padding: EdgeInsets.all(16),
                                  child: Text(
                                    'Add a step to start your sequence',
                                  ),
                                ),
                              for (
                                var index = 0;
                                index < widget.steps.length;
                                index++
                              )
                                row(
                                  context,
                                  index,
                                  labelWidth,
                                  canvasWidth,
                                  pxPerMs,
                                ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      );
    },
  );

  Widget row(
    BuildContext context,
    int index,
    double labelWidth,
    double width,
    double scale,
  ) {
    final step = widget.steps[index], colors = Theme.of(context).colorScheme;
    final active = index == widget.selected;
    final start = dragging == index ? draftStart! : stepStartMs(step);
    final length = dragging == index ? draftLength! : stepLengthMs(step);
    final cueWidth = math.max(44.0, length * scale);
    final repeats = boundedStepNumber(step['repeat_count'], 1, 64).toInt();
    final interval = boundedStepNumber(step['repeat_interval_ms'], 0, 4294967);
    final color = switch (step['kind']) {
      'buzzer' => const Color(0xff9c82dc),
      'display' => const Color(0xff63aea5),
      'rgb' => const Color(0xffdf9d69),
      _ => colors.primary,
    };
    return Container(
      height: 52,
      decoration: BoxDecoration(
        color: active ? colors.primary.withValues(alpha: .06) : null,
        border: Border(
          top: BorderSide(color: colors.outlineVariant.withValues(alpha: .5)),
        ),
      ),
      child: Row(
        children: [
          SizedBox(
            width: labelWidth,
            child: InkWell(
              onTap: () => widget.select(index),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                child: Text(
                  '${index + 1} · ${step['kind']}',
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.labelMedium
                      ?.copyWith(fontWeight: active ? FontWeight.w700 : null),
                ),
              ),
            ),
          ),
          SizedBox(
            width: width,
            child: Stack(
              clipBehavior: Clip.hardEdge,
              children: [
                for (var repeat = 1; repeat < repeats; repeat++)
                  Positioned(
                    left: (start + repeat * interval) * scale,
                    top: 10,
                    width: cueWidth,
                    height: 32,
                    child: DecoratedBox(
                      decoration: BoxDecoration(
                        color: color.withValues(alpha: .18),
                        borderRadius: BorderRadius.circular(5),
                      ),
                    ),
                  ),
                Positioned(
                  left: start * scale,
                  top: 4,
                  width: cueWidth,
                  height: 44,
                  child: Semantics(
                    selected: active,
                    label:
                        'Step ${index + 1}, ${step['kind']}, ${(start / 1000).toStringAsFixed(2)} seconds',
                    child: GestureDetector(
                      key: ValueKey('timeline-step-$index'),
                      behavior: HitTestBehavior.opaque,
                      onTap: () => widget.select(index),
                      onHorizontalDragStart: widget.enabled
                          ? (_) => begin(index, false)
                          : null,
                      onHorizontalDragUpdate: widget.enabled
                          ? (d) => update(d, scale)
                          : null,
                      onHorizontalDragEnd: widget.enabled
                          ? (_) => finish()
                          : null,
                      onHorizontalDragCancel: cancel,
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 6),
                        child: Container(
                          decoration: BoxDecoration(
                            color: color.withValues(alpha: active ? .9 : .65),
                            borderRadius: BorderRadius.circular(5),
                            border: active
                                ? Border.all(
                                    color: colors.onSurface,
                                    width: 1.5,
                                  )
                                : null,
                          ),
                          alignment: Alignment.centerLeft,
                          padding: const EdgeInsets.symmetric(horizontal: 6),
                          child: Text(
                            '${(start / 1000).toStringAsFixed(2)}s',
                            maxLines: 1,
                            overflow: TextOverflow.clip,
                            style: Theme.of(context).textTheme.labelSmall
                                ?.copyWith(color: Colors.white),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
                if (active && step['duration_ms'] is num)
                  Positioned(
                    left: start * scale + cueWidth - 6,
                    top: 4,
                    width: 44,
                    height: 44,
                    child: Tooltip(
                      message: 'Resize step duration',
                      child: GestureDetector(
                        key: ValueKey('timeline-resize-$index'),
                        behavior: HitTestBehavior.opaque,
                        onHorizontalDragStart: widget.enabled
                            ? (_) => begin(index, true)
                            : null,
                        onHorizontalDragUpdate: widget.enabled
                            ? (d) => update(d, scale)
                            : null,
                        onHorizontalDragEnd: widget.enabled
                            ? (_) => finish()
                            : null,
                        onHorizontalDragCancel: cancel,
                        child: Align(
                          alignment: Alignment.centerLeft,
                          child: Icon(
                            Icons.drag_indicator,
                            size: 18,
                            color: colors.onSurface,
                          ),
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class TimelineRuler extends CustomPainter {
  TimelineRuler(this.scale, this.color, this.style);
  final double scale;
  final Color color;
  final TextStyle style;
  @override
  void paint(Canvas canvas, Size size) {
    final raw = 100 / scale;
    final power = math.pow(10, (math.log(raw) / math.ln10).floor()).toDouble();
    final tick =
        (raw / power <= 2
            ? 2
            : raw / power <= 5
            ? 5
            : 10) *
        power;
    for (var ms = 0.0; ms * scale < size.width; ms += tick) {
      final x = ms * scale;
      canvas.drawLine(Offset(x, 25), Offset(x, 32), Paint()..color = color);
      final text = TextPainter(
        text: TextSpan(
          text: '${(ms / 1000).toStringAsFixed(ms < 1000 ? 2 : 1)}s',
          style: style,
        ),
        textDirection: TextDirection.ltr,
      )..layout();
      text.paint(canvas, Offset(x + 4, 3));
    }
  }

  @override
  bool shouldRepaint(TimelineRuler old) =>
      old.scale != scale || old.color != color || old.style != style;
}
