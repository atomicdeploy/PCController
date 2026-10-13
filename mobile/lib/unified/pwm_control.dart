import 'package:flutter/material.dart';

/// The thumb is an edit/setpoint. Applied state remains the peer observation.
/// A commit completes only after a fenced authoritative readback, never a retry.
class PwmSetpointControl extends StatefulWidget {
  const PwmSetpointControl({
    super.key,
    required this.enabled,
    required this.commit,
    this.observed,
    this.applyStyle,
    this.stale = false,
  });
  final bool enabled;
  final double? observed;
  final ButtonStyle? applyStyle;
  final bool stale;
  final Future<double> Function(int) commit;
  @override
  State<PwmSetpointControl> createState() => _PwmSetpointControlState();
}

class _PwmSetpointControlState extends State<PwmSetpointControl> {
  double? value;
  bool editing = false, pending = false;
  String? error;
  final input = TextEditingController();
  final inputKey = GlobalKey<FormFieldState<String>>();
  @override
  void initState() {
    super.initState();
    value = widget.observed;
  }

  @override
  void didUpdateWidget(covariant PwmSetpointControl oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!widget.enabled && !pending) editing = false;
    if (!editing && !pending) value = widget.observed;
  }

  Future<void> apply(int percent) async {
    if (pending || !widget.enabled) return;
    setState(() {
      pending = true;
      editing = false;
      value = percent.toDouble();
      error = null;
    });
    try {
      final applied = await widget.commit(percent);
      if (!applied.isFinite || applied < 0 || applied > 100) {
        throw StateError('Invalid PWM readback');
      }
      if (mounted) {
        setState(() {
          value = applied;
          pending = false;
        });
      }
    } catch (failure) {
      if (mounted) {
        setState(() {
          pending = false;
          value = widget.observed;
          error = '$failure';
        });
      }
    }
  }

  void applyInput() {
    if (inputKey.currentState?.validate() != true) return;
    apply(int.parse(input.text.trim()));
  }

  @override
  void dispose() {
    input.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      if (value != null) ...[
        Row(
          children: [
            Expanded(
              child: Text(
                '${widget.stale && !editing && !pending ? 'Last known' : 'Setpoint'} ${value!.toStringAsFixed(editing || pending ? 0 : 1)}%',
                style: Theme.of(context).textTheme.labelMedium,
              ),
            ),
            if (pending)
              const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
          ],
        ),
        Slider(
          value: value!.clamp(0, 100),
          max: 100,
          divisions: 100,
          label: '${value!.round()}%',
          onChangeStart: widget.enabled && !pending
              ? (_) => setState(() => editing = true)
              : null,
          onChanged: widget.enabled && !pending
              ? (next) => setState(() {
                  editing = true;
                  value = next;
                  error = null;
                })
              : null,
          onChangeEnd: widget.enabled && !pending
              ? (next) => apply(next.round())
              : null,
        ),
      ] else
        TextFormField(
          key: inputKey,
          controller: input,
          enabled: widget.enabled && !pending,
          keyboardType: TextInputType.number,
          decoration: InputDecoration(
            labelText: 'Setpoint (%)',
            suffixIcon: IconButton(
              tooltip: 'Apply PWM setpoint',
              onPressed: widget.enabled && !pending ? applyInput : null,
              icon: const Icon(Icons.check),
              style: widget.applyStyle,
            ),
          ),
          validator: (text) {
            final number = int.tryParse(text?.trim() ?? '');
            return number != null && number >= 0 && number <= 100
                ? null
                : 'Enter 0–100%';
          },
          onFieldSubmitted: (_) => applyInput(),
        ),
      if (error != null)
        Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(
            error!,
            style: Theme.of(context).textTheme.labelSmall
                ?.copyWith(color: Theme.of(context).colorScheme.error),
          ),
        ),
    ],
  );
}
