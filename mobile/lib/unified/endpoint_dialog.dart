import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'controller_dialog.dart';
import 'endpoint_address.dart';
import 'endpoint_detector.dart';

class AddressCharacterFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(
    TextEditingValue oldValue,
    TextEditingValue newValue,
  ) => newValue.copyWith(text: normalizeAddressCharacters(newValue.text));
}

class EndpointDialog extends StatefulWidget {
  const EndpointDialog({
    super.key,
    required this.controller,
    this.initialValue = '',
    this.advertised = const [],
    this.detectorFactory,
  });
  final bool controller;
  final String initialValue;
  final List<String> advertised;
  final EndpointDetector Function()? detectorFactory;
  @override
  State<EndpointDialog> createState() => _EndpointDialogState();
}

class _EndpointDialogState extends State<EndpointDialog> {
  late final address = TextEditingController(text: widget.initialValue);
  EndpointDetector? activeDetector;
  EndpointAddress? inputIntent;
  String? canonicalInput;
  DetectedEndpoint? verified;
  String? failure;
  bool edited = false, checking = false;
  int revision = 0;
  String get product => widget.controller ? 'pccontroller' : 'pealayer';
  String get name => widget.controller ? 'PCController' : 'Pealayer';
  EndpointAddress parse() =>
      address.text == canonicalInput && inputIntent != null
      ? inputIntent!
      : EndpointAddress.parse(
          address.text,
          defaultPort: widget.controller ? 8787 : 8080,
        );

  @override
  void initState() {
    super.initState();
    address.selection = TextSelection(
      baseOffset: 0,
      extentOffset: address.text.length,
    );
  }

  void changed(String value) {
    inputIntent = null;
    canonicalInput = null;
    ++revision;
    activeDetector?.close();
    activeDetector = null;
    setState(() {
      edited = true;
      checking = false;
      verified = null;
      failure = null;
    });
  }

  Future<void> detect({bool connect = false}) async {
    EndpointAddress input;
    try {
      input = parse();
    } on FormatException catch (error) {
      setState(() {
        edited = true;
        failure = error.message.toString();
      });
      return;
    }
    if (checking) return;
    if (connect && verified?.origin == input.origin) {
      Navigator.pop(context, input.origin.toString());
      return;
    }
    final epoch = ++revision;
    final detector = widget.detectorFactory?.call() ?? EndpointDetector();
    activeDetector = detector;
    setState(() {
      // Normalize the actual full endpoint field after parsing. Probe with the
      // original intent so a short hostname still permits default-port search.
      inputIntent = input;
      canonicalInput = input.origin.toString();
      address.value = TextEditingValue(
        text: input.origin.toString(),
        selection: TextSelection.collapsed(
          offset: input.origin.toString().length,
        ),
      );
      checking = true;
      failure = null;
    });
    try {
      final result = await detector.detect(
        input,
        product,
        advertised: widget.advertised,
        canceled: () => !mounted || epoch != revision,
      );
      if (!mounted || epoch != revision) return;
      if (connect) {
        Navigator.pop(context, result.origin.toString());
      } else {
        setState(() {
          inputIntent = null;
          canonicalInput = null;
          address.text = result.origin.toString();
          address.selection = TextSelection.collapsed(
            offset: address.text.length,
          );
          verified = result;
        });
      }
    } catch (error) {
      if (mounted && epoch == revision) {
        setState(
          () => failure = error is FormatException
              ? error.message.toString()
              : 'Endpoint check failed. Check the address and network.',
        );
      }
    } finally {
      detector.close();
      if (identical(activeDetector, detector)) activeDetector = null;
      if (mounted && epoch == revision) setState(() => checking = false);
    }
  }

  @override
  void dispose() {
    ++revision;
    activeDetector?.close();
    address.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    EndpointAddress? normalized;
    String? invalid;
    try {
      normalized = parse();
    } on FormatException catch (error) {
      invalid = error.message.toString();
    }
    final colors = Theme.of(context).colorScheme;
    return ControllerDialog(
      title: 'Connect $name',
      icon: widget.controller
          ? Icons.memory_outlined
          : Icons.play_circle_outline,
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
        if (failure?.startsWith('No verified ') == true &&
            normalized != null &&
            !checking)
          TextButton(
            onPressed: () =>
                Navigator.pop(context, normalized!.origin.toString()),
            child: const Text('Connect when available'),
          ),
        OutlinedButton.icon(
          onPressed: normalized != null && !checking ? () => detect() : null,
          icon: const Icon(Icons.search, size: 18),
          label: const Text('Detect'),
        ),
        FilledButton.icon(
          onPressed: normalized != null && !checking
              ? () => detect(connect: true)
              : null,
          icon: const Icon(Icons.link, size: 18),
          label: Text(checking ? 'Checking…' : 'Connect'),
        ),
      ],
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          EndpointInput(
            address: address,
            controller: widget.controller,
            invalid: edited ? invalid : null,
            verified: verified != null,
            onChanged: changed,
            onSubmitted: () => detect(connect: true),
          ),
          const SizedBox(height: 12),
          SizedBox(
            height: 2,
            child: checking
                ? const LinearProgressIndicator(minHeight: 2)
                : null,
          ),
          // A reserved error slot keeps the input/footer still during checking.
          SizedBox(
            height: 72,
            child: Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Text(
                failure ?? (edited ? invalid ?? '' : ''),
                style: Theme.of(context).textTheme.bodySmall
                    ?.copyWith(color: colors.error),
                maxLines: 4,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// A single complete endpoint editor; detection updates this same field.
class EndpointInput extends StatelessWidget {
  const EndpointInput({
    super.key,
    required this.address,
    required this.controller,
    required this.onChanged,
    required this.onSubmitted,
    this.invalid,
    this.verified = false,
  });
  final TextEditingController address;
  final bool controller, verified;
  final String? invalid;
  final ValueChanged<String> onChanged;
  final VoidCallback onSubmitted;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final border = OutlineInputBorder(
      borderRadius: BorderRadius.circular(14),
      borderSide: BorderSide(
        color: invalid != null ? colors.error : colors.outlineVariant,
      ),
    );
    return TextField(
      key: const ValueKey('peer-address'),
      controller: address,
      autofocus: true,
      textDirection: TextDirection.ltr,
      keyboardType: TextInputType.url,
      textInputAction: TextInputAction.done,
      autocorrect: false,
      enableSuggestions: false,
      inputFormatters: [AddressCharacterFormatter()],
      onChanged: onChanged,
      onSubmitted: (_) => onSubmitted(),
      decoration: InputDecoration(
        labelText: 'Endpoint',
        hintText: controller
            ? 'http://pccontroller.local:8787'
            : 'http://pealayer.local:8080',
        filled: true,
        fillColor: colors.primary.withValues(alpha: .045),
        enabledBorder: border,
        focusedBorder: border.copyWith(
          borderSide: BorderSide(
            color: invalid != null ? colors.error : colors.primary,
            width: 2,
          ),
        ),
        prefixIcon: Icon(Icons.dns_rounded, color: colors.primary),
        suffixIcon: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (verified)
              const Tooltip(
                message: 'Endpoint verified',
                child: Icon(Icons.verified_rounded, color: Color(0xff239765)),
              ),
            if (address.text.isNotEmpty)
              IconButton(
                tooltip: 'Clear address',
                onPressed: () {
                  address.clear();
                  onChanged('');
                },
                icon: const Icon(Icons.clear, size: 20),
              ),
          ],
        ),
      ),
    );
  }
}
