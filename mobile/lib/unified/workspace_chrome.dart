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
  });
  final int selected;
  final ValueChanged<int> changed;
  final bool vertical;
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
    final items = List.generate(activityIds.length, (index) {
      final active = widget.selected == index;
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
              color: active ? colors.primaryContainer : Colors.transparent,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(12),
                side: BorderSide(
                  color: active
                      ? colors.primary.withValues(alpha: .16)
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
                      Icon(
                        activityIcons[index],
                        size: 20,
                        color: active
                            ? colors.onPrimaryContainer
                            : colors.onSurfaceVariant,
                      ),
                      const SizedBox(width: 9),
                      if (widget.vertical)
                        Expanded(
                          child: Text(
                            activityNames[index],
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              color: active
                                  ? colors.onPrimaryContainer
                                  : colors.onSurfaceVariant,
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
                            color: active
                                ? colors.onPrimaryContainer
                                : colors.onSurfaceVariant,
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
                          color: colors.onPrimaryContainer,
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
    });
    return Container(
      padding: const EdgeInsets.symmetric(vertical: 6),
      decoration: widget.vertical
          ? null
          : BoxDecoration(
              color: colors.surfaceContainerLow,
              borderRadius: BorderRadius.circular(16),
              border: Border.all(
                color: colors.outlineVariant.withValues(alpha: .7),
              ),
            ),
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
