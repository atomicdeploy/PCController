import 'package:flutter/material.dart';

/// PU850-inspired header and rounded sheet, adapted to keyboard, phone/tablet
/// constraints and existing contextual themes rather than fixed native pixels.
class ControllerDialog extends StatelessWidget {
  const ControllerDialog({
    super.key,
    required this.title,
    required this.icon,
    required this.child,
    this.actions = const [],
    this.onClose,
  });
  final String title;
  final IconData icon;
  final Widget child;
  final List<Widget> actions;
  final VoidCallback? onClose;
  @override
  Widget build(BuildContext context) {
    final media = MediaQuery.of(context),
        colors = Theme.of(context).colorScheme;
    final compact = media.size.width < 600;
    return Dialog(
      alignment: compact ? Alignment.bottomCenter : Alignment.center,
      insetPadding: EdgeInsets.symmetric(
        horizontal: compact ? 12 : 28,
        vertical: 16,
      ),
      backgroundColor: colors.surfaceContainerLow,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: BorderSide(color: colors.outlineVariant),
      ),
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: 560,
          maxHeight:
              (media.size.height -
                      media.viewInsets.bottom -
                      media.padding.vertical -
                      32)
                  .clamp(120, 900)
                  .toDouble(),
        ),
        child: LayoutBuilder(
          builder: (context, bounds) {
            final short = bounds.maxHeight < 260;
            final content = Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Padding(
                  padding: const EdgeInsetsDirectional.fromSTEB(18, 8, 8, 8),
                  child: Row(
                    children: [
                      Icon(icon, color: colors.primary, size: 24),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Text(
                          title,
                          style: Theme.of(context).textTheme.titleMedium
                              ?.copyWith(fontWeight: FontWeight.w700),
                        ),
                      ),
                      IconButton(
                        tooltip: 'Close',
                        style: IconButton.styleFrom(
                          minimumSize: const Size(44, 44),
                        ),
                        onPressed: onClose ?? () => Navigator.of(context).pop(),
                        icon: const Icon(Icons.close),
                      ),
                    ],
                  ),
                ),
                Divider(height: 1, color: colors.outlineVariant),
                if (short)
                  Padding(padding: const EdgeInsets.all(20), child: child)
                else
                  Flexible(
                    child: SingleChildScrollView(
                      padding: const EdgeInsets.all(20),
                      child: child,
                    ),
                  ),
                if (actions.isNotEmpty) ...[
                  Divider(height: 1, color: colors.outlineVariant),
                  Padding(
                    padding: const EdgeInsets.all(12),
                    child: Align(
                      alignment: AlignmentDirectional.centerEnd,
                      child: Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        alignment: WrapAlignment.end,
                        children: actions,
                      ),
                    ),
                  ),
                ],
              ],
            );
            return short ? SingleChildScrollView(child: content) : content;
          },
        ),
      ),
    );
  }
}
