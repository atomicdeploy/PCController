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

/// One activity workspace; branding and connected products never choose pages.
class ActivityStrip extends StatelessWidget {
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
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final items = List.generate(activityIds.length, (index) {
      final active = selected == index;
      return Padding(
        padding: EdgeInsets.symmetric(
          horizontal: vertical ? 8 : 2,
          vertical: vertical ? 3 : 0,
        ),
        child: Semantics(
          selected: active,
          child: Tooltip(
            message: activityNames[index],
            child: Material(
              color: active
                  ? colors.primary.withValues(alpha: .14)
                  : Colors.transparent,
              borderRadius: BorderRadius.circular(10),
              child: InkWell(
                borderRadius: BorderRadius.circular(10),
                onTap: () => changed(index),
                child: Container(
                  constraints: const BoxConstraints(
                    minHeight: 48,
                    minWidth: 48,
                  ),
                  padding: const EdgeInsets.symmetric(horizontal: 12),
                  decoration: BoxDecoration(
                    border: active
                        ? Border(
                            bottom: BorderSide(color: colors.primary, width: 2),
                          )
                        : null,
                  ),
                  child: vertical
                      ? Row(
                          children: [
                            Icon(
                              activityIcons[index],
                              size: 21,
                              color: active
                                  ? colors.primary
                                  : colors.onSurfaceVariant,
                            ),
                            const SizedBox(width: 12),
                            Expanded(
                              child: Text(
                                activityNames[index],
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(
                                  color: active
                                      ? colors.primary
                                      : colors.onSurfaceVariant,
                                  fontWeight: active
                                      ? FontWeight.w700
                                      : FontWeight.w500,
                                ),
                              ),
                            ),
                          ],
                        )
                      : Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(
                              activityIcons[index],
                              size: 20,
                              color: active
                                  ? colors.primary
                                  : colors.onSurfaceVariant,
                            ),
                            const SizedBox(width: 7),
                            Text(
                              activityNames[index],
                              style: TextStyle(
                                color: active
                                    ? colors.primary
                                    : colors.onSurfaceVariant,
                                fontWeight: active
                                    ? FontWeight.w700
                                    : FontWeight.w500,
                              ),
                            ),
                          ],
                        ),
                ),
              ),
            ),
          ),
        ),
      );
    });
    return vertical
        ? SizedBox(width: 164, child: Column(children: items))
        : SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(children: items),
          );
  }
}
