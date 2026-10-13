import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

/// Horizontal activity navigation participates in the normal gesture arena;
/// child sliders, timeline editors and reorder handles retain first refusal.
class ActivitySwipe extends StatefulWidget {
  const ActivitySwipe({
    super.key,
    required this.selected,
    required this.activities,
    required this.changed,
    required this.child,
    this.enabled = true,
  });
  final int selected;
  final List<int> activities;
  final ValueChanged<int> changed;
  final Widget child;
  final bool enabled;

  @override
  State<ActivitySwipe> createState() => _ActivitySwipeState();
}

class _ActivitySwipeState extends State<ActivitySwipe> {
  double distance = 0;
  int? started;
  List<int> available = [];
  TextDirection? direction;

  void start(DragStartDetails details) {
    distance = 0;
    started = widget.selected;
    available = List.of(widget.activities);
    direction = Directionality.of(context);
  }

  void cancel() {
    distance = 0;
    started = null;
    available = [];
    direction = null;
  }

  void finish(DragEndDetails details, double width) {
    final current = available.indexOf(widget.selected);
    final velocity = details.primaryVelocity ?? 0;
    final deliberate =
        distance.abs() >= (width * .16).clamp(48, 96) ||
        distance.abs() >= 24 &&
            velocity.abs() >= 650 &&
            velocity * distance > 0;
    final valid =
        widget.enabled &&
        started == widget.selected &&
        listEquals(available, widget.activities) &&
        direction == Directionality.of(context) &&
        current >= 0;
    final forward = direction == TextDirection.rtl
        ? distance > 0
        : distance < 0;
    final next = current + (forward ? 1 : -1);
    final target = valid && deliberate && next >= 0 && next < available.length
        ? available[next]
        : null;
    cancel();
    if (target != null) widget.changed(target);
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, bounds) => Listener(
      // Flutter ends an accepted drag on pointer cancellation too. Retire it
      // before the recognizer can interpret that cancellation as navigation.
      onPointerCancel: (_) => cancel(),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onHorizontalDragStart: widget.enabled ? start : null,
        onHorizontalDragUpdate: widget.enabled
            ? (details) => distance += details.delta.dx
            : null,
        onHorizontalDragEnd: widget.enabled
            ? (details) => finish(details, bounds.maxWidth)
            : null,
        onHorizontalDragCancel: widget.enabled ? cancel : null,
        child: widget.child,
      ),
    ),
  );
}
