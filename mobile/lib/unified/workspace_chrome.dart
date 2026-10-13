import 'package:flutter/material.dart';

const activityIds = [
  'live',
  'channels',
  'panel',
  'studio',
  'devices',
  'firmware',
  'events',
];
const activityNames = [
  'Live',
  'Channels',
  'Panel',
  'Studio',
  'Devices',
  'Firmware',
  'Events',
];
const activityIcons = [
  Icons.monitor_heart_outlined,
  Icons.tune,
  Icons.dialpad,
  Icons.auto_awesome_outlined,
  Icons.hub_outlined,
  Icons.system_update_alt,
  Icons.history,
];

/// One activity workspace with stable targets and selection-aware scrolling.
class ActivityStrip extends StatefulWidget {
  const ActivityStrip({
    super.key,
    required this.selected,
    required this.changed,
    this.vertical = false,
    this.muted = false,
    this.activities = const [0, 1, 2, 3, 4, 5, 6],
  });
  final int selected;
  final ValueChanged<int> changed;
  final bool vertical;
  final bool muted;
  final List<int> activities;
  @override
  State<ActivityStrip> createState() => _ActivityStripState();
}

class _ActivityStripState extends State<ActivityStrip> {
  final targets = List.generate(activityIds.length, (_) => GlobalKey());
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final target = targets[widget.selected].currentContext;
      if (mounted && target != null) {
        Scrollable.ensureVisible(target, alignment: .5);
      }
    });
  }

  @override
  void didUpdateWidget(ActivityStrip oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.selected != widget.selected) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final target = targets[widget.selected].currentContext;
        if (mounted && target != null) {
          Scrollable.ensureVisible(
            target,
            duration: const Duration(milliseconds: 180),
            alignment: .5,
          );
        }
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final items = widget.activities.map((index) {
      final active = widget.selected == index;
      final dark = Theme.of(context).brightness == Brightness.dark;
      final accent = (dark
          ? const [
              Color(0xff5ce0b6),
              Color(0xffffc86a),
              Color(0xff82b6ff),
              Color(0xffcda0ff),
              Color(0xff6cdbed),
              Color(0xffffa780),
              Color(0xfff49dce),
            ]
          : const [
              Color(0xff007b60),
              Color(0xff956000),
              Color(0xff235ec0),
              Color(0xff8541c5),
              Color(0xff00788c),
              Color(0xffb64b20),
              Color(0xffb3317b),
            ])[index];
      final ink = widget.muted
          ? Color.lerp(accent, colors.onSurfaceVariant, .35)!
          : accent;
      return Padding(
        key: targets[index],
        padding: EdgeInsets.symmetric(
          horizontal: widget.vertical ? 8 : 3,
          vertical: widget.vertical ? 4 : 0,
        ),
        child: Semantics(
          selected: active,
          child: Tooltip(
            message: activityNames[index],
            child: Material(
              color: active
                  ? ink.withValues(alpha: widget.muted ? .09 : .13)
                  : Colors.transparent,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(12),
                side: BorderSide(
                  color: active
                      ? ink.withValues(alpha: .38)
                      : Colors.transparent,
                ),
              ),
              child: InkWell(
                borderRadius: BorderRadius.circular(12),
                onTap: () => widget.changed(index),
                child: Container(
                  constraints: const BoxConstraints(
                    minHeight: 48,
                    minWidth: 48,
                  ),
                  padding: EdgeInsets.symmetric(
                    horizontal: widget.vertical ? 10 : 12,
                  ),
                  child: Row(
                    mainAxisSize: widget.vertical
                        ? MainAxisSize.max
                        : MainAxisSize.min,
                    children: [
                      Container(
                        padding: const EdgeInsets.all(5),
                        decoration: BoxDecoration(
                          color: ink.withValues(alpha: active ? .18 : .09),
                          borderRadius: BorderRadius.circular(9),
                        ),
                        child: Icon(activityIcons[index], size: 20, color: ink),
                      ),
                      const SizedBox(width: 9),
                      if (widget.vertical)
                        Expanded(
                          child: Text(
                            activityNames[index],
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              color: active ? ink : colors.onSurfaceVariant,
                              fontWeight: active
                                  ? FontWeight.w700
                                  : FontWeight.w500,
                            ),
                          ),
                        )
                      else
                        Text(
                          activityNames[index],
                          style: TextStyle(
                            color: active ? ink : colors.onSurfaceVariant,
                            fontWeight: active
                                ? FontWeight.w700
                                : FontWeight.w500,
                          ),
                        ),
                      if (widget.vertical && active) ...[
                        const SizedBox(width: 4),
                        Icon(
                          Directionality.of(context) == TextDirection.rtl
                              ? Icons.chevron_left
                              : Icons.chevron_right,
                          size: 16,
                          color: ink,
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      );
    }).toList();
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: widget.vertical
          ? SizedBox(
              width: 164,
              child: SingleChildScrollView(child: Column(children: items)),
            )
          : SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(children: items),
            ),
    );
  }
}
