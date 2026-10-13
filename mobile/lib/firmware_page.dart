import 'dart:async';

import 'package:flutter/material.dart';

import 'api.dart';
import 'documents.dart';
import 'firmware.dart';
import 'icons.dart';

class FirmwarePage extends StatefulWidget {
  const FirmwarePage({
    super.key,
    required this.api,
    required this.authorized,
    required this.busyChanged,
    this.documents,
  });
  final PakarApi api;
  final bool Function(String) authorized;
  final void Function(bool) busyChanged;
  final AndroidDocuments? documents;
  @override
  State<FirmwarePage> createState() => _FirmwarePageState();
}

class _FirmwarePageState extends State<FirmwarePage> {
  late final controller = FirmwareController(
    widget.api,
    authorized: (feature) => widget.authorized(feature),
  );
  late final documents = widget.documents ?? AndroidDocuments();
  final url = TextEditingController(),
      hash = TextEditingController(),
      bearer = TextEditingController();
  RemoteJob? job;
  Timer? timer;
  String message = '', staged = '';
  bool working = false,
      executing = false,
      loading = false,
      uncertain = false,
      downgrade = false;
  int? targets;
  int readEpoch = 0;
  @override
  void initState() {
    super.initState();
    unawaited(refresh());
    timer = Timer.periodic(const Duration(seconds: 3), (_) => refresh());
  }

  @override
  void dispose() {
    timer?.cancel();
    ++readEpoch;
    controller.dispose();
    url.dispose();
    hash.dispose();
    bearer.dispose();
    super.dispose();
  }

  Future<void> refresh({bool explicit = false}) async {
    if (!mounted || working || loading) return;
    final epoch = ++readEpoch;
    loading = true;
    try {
      final observed = await controller.status();
      if (!mounted || epoch != readEpoch) return;
      setState(() {
        job = observed;
        if (explicit) {
          uncertain = false;
          message = 'Observed current job. Prior commands were not replayed.';
        }
      });
    } catch (error) {
      if (mounted && epoch == readEpoch) {
        setState(() {
          job = null;
          message = '$error';
        });
      }
    } finally {
      loading = false;
    }
  }

  Future<bool> confirm(String title, String detail) async =>
      await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(title),
          content: SingleChildScrollView(child: Text(detail)),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Confirm'),
            ),
          ],
        ),
      ) ??
      false;
  Future<void> run(
    String title,
    String warning,
    Future<String> Function() action, {
    bool mutation = true,
  }) async {
    if (!mounted || working || (mutation && uncertain)) return;
    ++readEpoch;
    setState(() {
      working = true;
      message = '';
    });
    widget.busyChanged(true);
    try {
      if (!await confirm(title, warning) || !mounted) return;
      setState(() => executing = true);
      final result = await action();
      if (mounted) setState(() => message = result);
    } catch (error) {
      if (mounted) {
        setState(() {
          uncertain = mutation;
          message =
              '$error No operation was replayed. Inspect with Refresh before deciding on another action.';
        });
      }
    } finally {
      if (mounted) {
        setState(() {
          working = false;
          executing = false;
        });
        widget.busyChanged(false);
      }
    }
  }

  Future<void> stageRemote() async {
    final target = targets,
        source = url.text,
        digest = hash.text,
        token = bearer.text,
        allow = downgrade;
    try {
      if (target == null) {
        throw const FormatException('Choose acknowledged board targets');
      }
      validateRemote(source, digest, target, token);
      controller.checkTargets(target);
    } catch (error) {
      setState(() => message = '$error');
      return;
    }
    await run(
      'Download and stage?',
      'Download the SHA-256-pinned manifest over HTTPS for ${target == 3
          ? 'both boards'
          : target == 1
          ? 'ESP'
          : 'AVR'}. This does NOT flash either board. ${allow ? 'Downgrades explicitly allowed.' : 'Downgrades are not allowed.'}',
      () async {
        final ack = await controller.stage(
          source,
          digest,
          target,
          token,
          allowDowngrade: allow,
        );
        if (mounted) {
          job = ack;
          bearer.clear();
          staged = '';
        }
        return 'Download accepted as job ${ack.id}; wait for the actual ready state before applying.';
      },
    );
  }

  Future<void> jobAction(bool apply) async {
    final captured = job;
    if (captured == null) return;
    await run(
      apply ? 'Apply exact staged job?' : 'Cancel exact download?',
      'Job ${captured.id} · boot ${captured.boot}\nSHA-256 ${captured.hash}\n${apply ? 'This can replace firmware, reset both boards and interrupt networking. Device-side backups, EEPROM preservation, pair verification and the durable Prog gate apply. Keep stable power. Completion requires post-reboot readback, not this acknowledgment.' : 'Cancel only this download/staged job, not an active programming/recovery sequence. No previous image is applied.'}',
      () async {
        final ack = await controller.act(captured, apply: apply);
        if (mounted) job = ack;
        return apply
            ? 'Exact job accepted for preparation. Follow observed progress; acceptance is not completion.'
            : 'Exact job cancellation confirmed.';
      },
    );
  }

  Future<void> manual(String action) async {
    final esp = action == 'update';
    final warning = switch (action) {
      'probe' => 'ISP signature probe resets/interrupts AVR. It is sent once, even though its HTTP method is GET.',
      'backup' => 'Read full AVR flash (32768 bytes, including Urboot) and EEPROM (1024 bytes) into ESP storage. This replaces the device’s previous backups. Keep stable power.',
      'stage' => 'Choose a complete 32768-byte raw AVR .bin including the intended bootloader. This only replaces the shared staging file; it does not program AVR. Filename/size are not authenticity checks.',
      'program' => 'Program the CURRENT shared AVR staging image. Its hash is not advertised by this legacy route and another client can replace it. Only continue if you independently know its provenance. Prefer the pinned remote pair workflow. This resets AVR and preserves/verifies EEPROM; keep stable power.',
      'update' => 'Choose an uncompressed ESP8266 E9 binary for this exact board/profile. Direct OTA has no pinned manifest authenticity or paired-version assurance; prefer the remote pair workflow. ESP will reboot and disconnect. Keep stable power.',
      _ => '',
    };
    await run(
      esp
          ? 'Manual ESP update?'
          : 'AVR ${action == 'stage' ? 'image import' : action}?',
      warning,
      () async {
        controller.check(esp ? 'ota' : 'isp');
        FirmwareDocument? document;
        if (action == 'stage' || esp) {
          document = await documents.pick(maximum: esp ? 2097152 : 32768);
          controller.check(esp ? 'ota' : 'isp');
          if (document == null) {
            return 'File selection cancelled; no device operation sent.';
          }
          validateFirmwareImage(esp, document.bytes);
          if (!mounted ||
              !await confirm(
                'Use ${document.name}?',
                '${document.bytes.length} bytes. ${esp ? 'Apply this ESP firmware now?' : 'Store this full AVR image without programming?'} Filename and format do not verify origin.',
              )) {
            return 'Image not accepted; no upload sent.';
          }
          controller.check(esp ? 'ota' : 'isp');
        }
        final result = await controller.manual(action, bytes: document?.bytes);
        if (mounted && action == 'stage') staged = document!.name;
        return result;
      },
    );
  }

  Future<void> export(bool eeprom) =>
      run('Export stored ${eeprom ? 'EEPROM' : 'flash'} backup?', 'Download the existing ESP-stored backup, validate its exact full size, then choose a NEW Android document. This does not take a fresh backup or verify it represents the current running AVR. No existing document is overwritten.', () async {
        final bytes = await controller.backupBytes(eeprom);
        final saved = await documents.export(
          'avr-${eeprom ? 'eeprom' : 'flash'}.bin',
          bytes,
          stillOwned: () => mounted && controller.allowed('isp'),
        );
        controller.check('isp');
        return saved
            ? '${bytes.length}-byte stored backup exported to the new document.'
            : 'Export cancelled; no document written.';
      }, mutation: false);
  Widget card(String title, List<Widget> children) => Card(
    child: Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(title, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 16),
          ...children,
        ],
      ),
    ),
  );
  Widget button(
    String text,
    IconData icon,
    VoidCallback action, {
    bool enabled = true,
  }) => OutlinedButton.icon(
    onPressed: working || uncertain || !enabled ? null : action,
    icon: Icon(icon),
    label: Text(text),
  );
  @override
  Widget build(BuildContext context) {
    final isp = controller.allowed('isp'), ota = controller.allowed('ota');
    if (!isp && !ota) return const SizedBox.shrink();
    final observed = job;
    final available = [if (ota) 1, if (isp) 2, if (ota && isp) 3];
    if (!available.contains(targets)) targets = null;
    final free =
        observed != null && !observed.busy && observed.phase != 'ready';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        card('Remote firmware · pinned manifest', [
          const Text(
            'HTTPS download → verified staging → explicit apply → post-boot pair verification. No automatic replay.',
          ),
          const SizedBox(height: 16),
          for (final entry in [
            (url, 'Manifest HTTPS URL'),
            (hash, 'Manifest SHA-256 · lowercase'),
            (bearer, 'Optional download bearer'),
          ]) ...[
            TextField(
              controller: entry.$1,
              enabled: !working && !uncertain && free,
              obscureText: entry.$1 == bearer,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(
                labelText: entry.$2,
                border: const OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 12),
          ],
          DropdownButtonFormField<int>(
            key: ValueKey(targets),
            initialValue: targets,
            isExpanded: true,
            icon: const Icon(PakarIcons.caretDown),
            decoration: const InputDecoration(
              labelText: 'Acknowledged update targets',
            ),
            items: [
              for (final t in available)
                DropdownMenuItem(
                  value: t,
                  child: Text(
                    t == 3
                        ? 'Both boards · coordinated'
                        : t == 1
                        ? 'ESP only'
                        : 'AVR only',
                  ),
                ),
            ],
            onChanged: working || uncertain || !free
                ? null
                : (value) => setState(() => targets = value),
          ),
          SwitchListTile.adaptive(
            contentPadding: EdgeInsets.zero,
            title: const Text('Explicitly allow firmware downgrade'),
            value: downgrade,
            onChanged: working || uncertain || !free
                ? null
                : (v) => setState(() => downgrade = v),
          ),
          button(
            'Download and stage',
            PakarIcons.cloud,
            stageRemote,
            enabled: free,
          ),
        ]),
        card('Observed job', [
          Row(
            children: [
              Expanded(child: Text(observed?.phase ?? 'Job state unavailable')),
              IconButton(
                tooltip: 'Refresh and inspect current state',
                onPressed: working ? null : () => refresh(explicit: true),
                icon: const Icon(PakarIcons.arrowClockwise),
              ),
            ],
          ),
          if (observed != null) ...[
            SelectableText('Job ${observed.id} · boot ${observed.boot}'),
            if (observed.id > 0) SelectableText('Manifest ${observed.hash}'),
            Text(
              'Targets: ${observed.targets == 3
                  ? 'ESP + AVR'
                  : observed.targets == 1
                  ? 'ESP'
                  : 'AVR'}',
            ),
            if (observed.expected > 0) ...[
              const SizedBox(height: 8),
              LinearProgressIndicator(
                value: observed.received / observed.expected,
              ),
              Text(
                '${observed.received} / ${observed.expected} received bytes · not flash percentage',
              ),
            ],
            if (observed.error.isNotEmpty)
              Text(
                observed.error,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            if (observed.phase == 'complete')
              const Text(
                'Device reports pair completion; physical peripheral acceptance is separate.',
              ),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                button(
                  'Apply exact job',
                  PakarIcons.lightning,
                  () => jobAction(true),
                  enabled:
                      observed.canApply &&
                      controller.allowedTargets(observed.targets),
                ),
                button(
                  'Cancel exact job',
                  PakarIcons.warning,
                  () => jobAction(false),
                  enabled:
                      observed.canCancel &&
                      controller.allowedTargets(observed.targets),
                ),
              ],
            ),
          ],
          if (uncertain)
            const Text(
              'Outcome needs inspection. Only explicit Refresh unlocks a new decision; no retry is queued.',
            ),
        ]),
        if (isp)
          card('AVR ISP · complete flash and EEPROM', [
            const Text(
              'These maintenance actions reset AVR. Exported backups include all flash/Urboot or all EEPROM, not partial application data.',
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                button(
                  'Probe ISP',
                  PakarIcons.cpu,
                  () => manual('probe'),
                  enabled: free,
                ),
                button(
                  'Create full backups',
                  PakarIcons.floppyDisk,
                  () => manual('backup'),
                  enabled: free,
                ),
                button(
                  'Export flash',
                  PakarIcons.floppyDisk,
                  () => export(false),
                  enabled: observed != null && !observed.busy,
                ),
                button(
                  'Export EEPROM',
                  PakarIcons.floppyDisk,
                  () => export(true),
                  enabled: observed != null && !observed.busy,
                ),
                button(
                  'Import full AVR image',
                  PakarIcons.cpu,
                  () => manual('stage'),
                  enabled: free,
                ),
                button(
                  'Program shared image',
                  PakarIcons.warning,
                  () => manual('program'),
                  enabled: free,
                ),
              ],
            ),
            if (staged.isNotEmpty)
              Text(
                'Last local stage: $staged · NOT a verified current shared-image hash',
              ),
          ]),
        if (ota)
          card('Manual ESP OTA', [
            const Text(
              'For a known complete ESP8266 binary only. Pinned remote coordinated updates are preferred. Reconnect and read the actual running version after reboot.',
            ),
            const SizedBox(height: 12),
            button(
              'Export running ESP firmware',
              PakarIcons.floppyDisk,
              () => run('Download running ESP firmware?', 'Read the running application image, not full flash or private settings. Choose a new Android document. This is not an authenticated release manifest.', () async {
                final bytes = await controller.espBytes();
                final saved = await documents.export(
                  'esp-firmware.bin',
                  bytes,
                  stillOwned: () => mounted && controller.allowed('ota'),
                );
                return saved
                    ? '${bytes.length} running firmware bytes exported.'
                    : 'Export cancelled.';
              }, mutation: false),
              enabled: free,
            ),
            const SizedBox(height: 12),
            button(
              'Select and update ESP',
              PakarIcons.warning,
              () => manual('update'),
              enabled: free,
            ),
          ]),
        if (executing) const LinearProgressIndicator(),
        if (message.isNotEmpty)
          Padding(
            padding: const EdgeInsets.all(12),
            child: Semantics(liveRegion: true, child: Text(message)),
          ),
      ],
    );
  }
}
