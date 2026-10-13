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
  DetectedEndpoint? verified;
  String? failure;
  bool edited = false, checking = false;
  int revision = 0;
  String get product => widget.controller ? 'pccontroller' : 'pealayer';
  String get name => widget.controller ? 'PCController' : 'Pealayer';
  EndpointAddress parse() => EndpointAddress.parse(
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
          TextField(
            key: const ValueKey('peer-address'),
            controller: address,
            autofocus: true,
            textDirection: TextDirection.ltr,
            keyboardType: TextInputType.url,
            textInputAction: TextInputAction.done,
            autocorrect: false,
            enableSuggestions: false,
            inputFormatters: [AddressCharacterFormatter()],
            onChanged: changed,
            onSubmitted: (_) => detect(connect: true),
            decoration: InputDecoration(
              labelText: 'Hostname or URL',
              hintText: widget.controller
                  ? '192.168.1.20:8787'
                  : 'pealayer.local:8080',
              prefixIcon: const Icon(Icons.link),
              enabledBorder: edited && invalid != null
                  ? OutlineInputBorder(
                      borderRadius: BorderRadius.circular(10),
                      borderSide: BorderSide(color: colors.error),
                    )
                  : null,
              focusedBorder: edited && invalid != null
                  ? OutlineInputBorder(
                      borderRadius: BorderRadius.circular(10),
                      borderSide: BorderSide(color: colors.error, width: 2),
                    )
                  : null,
              suffixIcon: address.text.isEmpty
                  ? null
                  : IconButton(
                      tooltip: 'Clear address',
                      onPressed: () {
                        address.clear();
                        changed('');
                      },
                      icon: const Icon(Icons.clear),
                    ),
            ),
          ),
          const SizedBox(height: 12),
          SizedBox(
            height: 2,
            child: checking
                ? const LinearProgressIndicator(minHeight: 2)
                : null,
          ),
          const SizedBox(height: 12),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: colors.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(10),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  verified != null
                      ? Icons.check_circle_outline
                      : Icons.dns_outlined,
                  size: 18,
                  color: verified != null
                      ? colors.primary
                      : colors.onSurfaceVariant,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      if (verified != null)
                        Text(
                          '$name verified',
                          style: Theme.of(context).textTheme.labelMedium,
                        ),
                      SelectableText(
                        normalized?.origin.toString() ?? name,
                        textDirection: TextDirection.ltr,
                        style: Theme.of(context).textTheme.bodyMedium
                            ?.copyWith(fontFamily: 'DroidSansMono'),
                      ),
                    ],
                  ),
                ),
              ],
            ),
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
