import 'package:flutter/material.dart';

import '../icons.dart';
import 'transport.dart' show Json;
import 'controller_dialog.dart';

const channelIcons = <String, IconData>{
  'armchair': Icons.airline_seat_recline_normal,
  'lightbulb': Icons.lightbulb_outline,
  'lightning': PakarIcons.lightning,
  'power': Icons.power_settings_new,
  'fan': Icons.air,
  'speaker': Icons.speaker_outlined,
  'monitor': Icons.monitor,
  'cpu': PakarIcons.cpu,
  'thermometer': Icons.thermostat,
  'gauge': PakarIcons.gauge,
  'music-note': PakarIcons.musicNote,
  'keyboard': PakarIcons.keyboard,
  'radio': PakarIcons.radio,
};
IconData channelIcon(Json control) =>
    channelIcons[control['icon']] ??
    (['seat', 'side', 'motion'].contains(control['kind'])
        ? Icons.airline_seat_recline_normal
        : control['kind'] == 'relay'
        ? Icons.power
        : Icons.lightbulb_outline);
Color? channelColor(Object? value) =>
    value is String && RegExp(r'^#[0-9a-fA-F]{6}$').hasMatch(value)
    ? Color(0xff000000 | int.parse(value.substring(1), radix: 16))
    : null;

class ChannelEditor extends StatefulWidget {
  const ChannelEditor({
    super.key,
    required this.control,
    this.folders = const [],
    this.controls = const [],
  });
  final Json control;
  final List<Json> folders, controls;
  @override
  State<ChannelEditor> createState() => _ChannelEditorState();
}

class _ChannelEditorState extends State<ChannelEditor> {
  final form = GlobalKey<FormState>();
  late final name = TextEditingController(
    text: '${widget.control['name'] ?? ''}',
  );
  late String folder = '${widget.control['group'] ?? ''}';
  String kind(Object? value) => ['seat', 'side', 'motion'].contains(value)
      ? 'motion'
      : ['mosfet', 'pwm'].contains(value)
      ? 'pwm'
      : value == 'relay'
      ? 'relay'
      : 'board';
  List<String> get folderChoices => <String>{
    '',
    folder,
    for (final f in widget.folders)
      if (f['kind'] == kind(widget.control['kind']) && f['name'] is String)
        f['name'] as String,
    for (final c in widget.controls)
      if (kind(c['kind']) == kind(widget.control['kind']) &&
          c['group'] is String)
        c['group'] as String,
  }.toList()..sort((a, b) => a.toLowerCase().compareTo(b.toLowerCase()));
  late final color = TextEditingController(
    text: '${widget.control['color'] ?? ''}',
  );
  late String icon = '${widget.control['icon'] ?? ''}';
  @override
  void dispose() {
    name.dispose();
    color.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ControllerDialog(
    title: 'Edit channel',
    icon: channelIcons[icon] ?? channelIcon(widget.control),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      FilledButton(
        onPressed: () {
          if (form.currentState!.validate()) {
            Navigator.pop(context, <String, dynamic>{
              'name': name.text.trim(),
              'group': folder,
              'icon': icon,
              'color': color.text.trim(),
            });
          }
        },
        child: const Text('Save'),
      ),
    ],
    child: SizedBox(
      width: 440,
      child: SingleChildScrollView(
        child: Form(
          key: form,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextFormField(
                controller: name,
                maxLength: 96,
                decoration: const InputDecoration(labelText: 'Name'),
              ),
              DropdownButtonFormField<String>(
                key: const ValueKey('channel-folder'),
                initialValue: folder,
                isExpanded: true,
                decoration: const InputDecoration(
                  labelText: 'Folder',
                  prefixIcon: Icon(Icons.folder_outlined),
                ),
                items: [
                  for (final value in folderChoices)
                    DropdownMenuItem(
                      value: value,
                      child: Text(value.isEmpty ? 'No folder' : value),
                    ),
                ],
                onChanged: (value) {
                  if (value != null) setState(() => folder = value);
                },
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                initialValue: icon,
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Icon'),
                items: [
                  const DropdownMenuItem(value: '', child: Text('Default')),
                  if (icon.isNotEmpty && !channelIcons.containsKey(icon))
                    DropdownMenuItem(value: icon, child: Text(icon)),
                  for (final entry in channelIcons.entries)
                    DropdownMenuItem(
                      value: entry.key,
                      child: Row(
                        children: [
                          Icon(entry.value, size: 20),
                          const SizedBox(width: 12),
                          Text(entry.key),
                        ],
                      ),
                    ),
                ],
                onChanged: (v) {
                  if (v != null) setState(() => icon = v);
                },
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: color,
                decoration: const InputDecoration(
                  labelText: 'Color',
                  hintText: '#RRGGBB',
                ),
                onChanged: (_) => setState(() {}),
                validator: (v) =>
                    v != null && v.isNotEmpty && channelColor(v) == null
                    ? 'Use #RRGGBB or leave blank for Default'
                    : null,
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final value in [
                    '#7166CE',
                    '#55BD83',
                    '#E68B85',
                    '#E0AC62',
                    '#62BCAE',
                    '#5D9BDE',
                    '#BA80D7',
                  ])
                    IconButton(
                      tooltip: value,
                      onPressed: () => setState(() => color.text = value),
                      icon: Icon(
                        color.text.toUpperCase() == value
                            ? Icons.check_circle
                            : Icons.circle,
                        color: channelColor(value),
                      ),
                      style: IconButton.styleFrom(
                        minimumSize: const Size(44, 44),
                      ),
                    ),
                ],
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
