import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../pakar_reference.dart' show studioDark;

const paletteSeeds = {
  'digitalogic': Color(0xff7166ce),
  'pealayer': Color(0xff719c42),
  'pakar': Color(0xff68a7ff),
  'amber': Color(0xffb77416),
  'custom': Color(0xff7166ce),
};
const paletteNames = {
  'digitalogic': 'Digitalogic',
  'pealayer': 'Pealayer',
  'pakar': 'PakarController',
  'amber': 'Amber',
  'custom': 'Custom',
};

class AppPreferences extends ChangeNotifier {
  void refresh() => notifyListeners();
  ThemeMode theme = ThemeMode.system;
  String layout = 'auto';
  bool compact = true;
  bool autoConnect = true;
  bool sharePackages = false;
  String palette = 'digitalogic',
      branding = 'pccontroller',
      relayLayout = 'compact';
  Color customColor = const Color(0xff7166ce);
  String get title => branding == 'pealayer' ? 'Pealayer' : 'PCController';
  ColorScheme scheme(Brightness brightness) {
    if (palette == 'pakar' && brightness == Brightness.dark) {
      return studioDark.copyWith(
        surfaceContainerLowest: const Color(0xff080a0e),
        surfaceContainerLow: studioDark.surface,
        surfaceContainer: const Color(0xff19202c),
        outlineVariant: studioDark.outline,
        onSurfaceVariant: const Color(0xff98a4b5),
      );
    }
    return ColorScheme.fromSeed(
      seedColor: palette == 'custom' ? customColor : paletteSeeds[palette]!,
      brightness: brightness,
    );
  }

  bool mirrorBuzzer = false;
  double mirrorVolume = .35;
  Set<String> launchers = {'pccontroller'};
  static const native = MethodChannel(
    'ir.digitalogic.pccontroller/preferences',
  );

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    theme = ThemeMode.values.firstWhere(
      (v) => v.name == prefs.getString('appearance.theme'),
      orElse: () => ThemeMode.system,
    );
    final savedLayout = prefs.getString('appearance.layout');
    if (['auto', 'phone', 'tablet'].contains(savedLayout)) {
      layout = savedLayout!;
    }
    compact = prefs.getBool('appearance.compact') ?? true;
    autoConnect = prefs.getBool('connections.auto') ?? true;
    sharePackages = prefs.getBool('upgrades.sharing') ?? false;
    final savedRelayLayout = prefs.getString('controls.relay-layout');
    if (['compact', 'explicit'].contains(savedRelayLayout)) {
      relayLayout = savedRelayLayout!;
    }
    final savedPalette = prefs.getString('appearance.palette');
    if (paletteSeeds.containsKey(savedPalette)) palette = savedPalette!;
    customColor = Color(prefs.getInt('appearance.color') ?? 0xff7166ce);
    final savedBrand = prefs.getString('appearance.branding');
    if (['pccontroller', 'pealayer'].contains(savedBrand)) {
      branding = savedBrand!;
    }
    mirrorBuzzer = prefs.getBool('mirror.buzzer') ?? false;
    mirrorVolume = (prefs.getDouble('mirror.volume') ?? .35).clamp(0, 1);
    // Android owns the actual launcher state, including after an app update.
    try {
      final actual = await native.invokeListMethod<String>('launchers');
      if (actual != null && actual.isNotEmpty) launchers = actual.toSet();
    } on PlatformException {
      // Optional OS preferences must not block saved peer restoration.
    } on MissingPluginException {
      // Renderer/non-Android surfaces have no Android launcher adapter.
    }
    notifyListeners();
    try {
      await updateTask();
    } on PlatformException {
      /* Android presentation must not block peer restore. */
    }
  }

  Future<void> save() async {
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('appearance.theme', theme.name);
    await prefs.setString('appearance.layout', layout);
    await prefs.setBool('appearance.compact', compact);
    await prefs.setBool('connections.auto', autoConnect);
    await prefs.setBool('upgrades.sharing', sharePackages);
    await prefs.setString('controls.relay-layout', relayLayout);
    await prefs.setString('appearance.palette', palette);
    await prefs.setInt('appearance.color', customColor.toARGB32());
    await prefs.setString('appearance.branding', branding);
    await prefs.setBool('mirror.buzzer', mirrorBuzzer);
    await prefs.setDouble('mirror.volume', mirrorVolume);
  }

  Future<void> updateTask() async {
    try {
      final icon = await rootBundle.load('assets/branding/$branding.png');
      await native.invokeMethod<void>('setTask', {
        'title': title,
        'icon': icon.buffer.asUint8List(icon.offsetInBytes, icon.lengthInBytes),
        'color': (palette == 'custom' ? customColor : paletteSeeds[palette]!)
            .toARGB32(),
      });
    } on MissingPluginException {
      // Renderer/non-Android surface.
    }
  }

  Future<void> setBranding(String value) async {
    if (!['pccontroller', 'pealayer'].contains(value)) return;
    final previous = branding;
    branding = value;
    try {
      await updateTask();
    } catch (_) {
      branding = previous;
      rethrow;
    }
    await save();
  }

  Future<void> setLaunchers(Set<String> values) async {
    if (values.isEmpty) return;
    await native.invokeMethod<void>('setLaunchers', {
      'values': values.toList(),
    });
    launchers = Set.of(values);
    notifyListeners();
  }
}

class AppSettings extends StatelessWidget {
  const AppSettings({
    super.key,
    required this.preferences,
    required this.connections,
    required this.automation,
    required this.changed,
  });
  final AppPreferences preferences;
  final Widget connections, automation;
  final VoidCallback changed;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: preferences,
    builder: (context, _) => Scaffold(
      appBar: AppBar(title: const Text('Preferences')),
      body: LayoutBuilder(
        builder: (context, bounds) => SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: Wrap(
            spacing: 14,
            runSpacing: 14,
            children: [
              for (final section in [
                _section(context, 'Appearance', Icons.palette_outlined, [
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final entry in paletteNames.entries)
                        ChoiceChip(
                          label: Text(entry.value),
                          avatar: CircleAvatar(
                            radius: 7,
                            backgroundColor: entry.key == 'custom'
                                ? preferences.customColor
                                : paletteSeeds[entry.key],
                          ),
                          selected: preferences.palette == entry.key,
                          onSelected: (_) async {
                            if (entry.key == 'custom') {
                              final value = await _customColor(context);
                              if (value == null) return;
                              preferences.customColor = value;
                            }
                            preferences.palette = entry.key;
                            await preferences.save();
                            try {
                              await preferences.updateTask();
                            } catch (_) {
                              if (context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(
                                    content: Text(
                                      'Android task appearance could not be updated.',
                                    ),
                                  ),
                                );
                              }
                            }
                          },
                        ),
                    ],
                  ),
                  const SizedBox(height: 16),
                  DropdownButtonFormField<ThemeMode>(
                    initialValue: preferences.theme,
                    decoration: const InputDecoration(labelText: 'Theme'),
                    items: [
                      for (final mode in ThemeMode.values)
                        DropdownMenuItem(
                          value: mode,
                          child: Text(switch (mode) {
                            ThemeMode.system => 'System',
                            ThemeMode.light => 'Light',
                            ThemeMode.dark => 'Dark',
                          }),
                        ),
                    ],
                    onChanged: (v) {
                      if (v != null) {
                        preferences.theme = v;
                        preferences.save();
                      }
                    },
                  ),
                  const SizedBox(height: 16),
                  DropdownButtonFormField<String>(
                    initialValue: preferences.layout,
                    decoration: const InputDecoration(labelText: 'Layout'),
                    items: const [
                      DropdownMenuItem(value: 'auto', child: Text('Automatic')),
                      DropdownMenuItem(value: 'phone', child: Text('Mobile')),
                      DropdownMenuItem(value: 'tablet', child: Text('Tablet')),
                    ],
                    onChanged: (v) {
                      if (v != null) {
                        preferences.layout = v;
                        preferences.save();
                      }
                    },
                  ),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Compact controls'),
                    value: preferences.compact,
                    onChanged: (v) {
                      preferences.compact = v;
                      preferences.save();
                    },
                  ),
                ]),
                _section(context, 'Controls', Icons.tune, [
                  DropdownButtonFormField<String>(
                    initialValue: preferences.relayLayout,
                    decoration: const InputDecoration(labelText: 'Relay cards'),
                    items: const [
                      DropdownMenuItem(
                        value: 'compact',
                        child: Text('Compact · one line'),
                      ),
                      DropdownMenuItem(
                        value: 'explicit',
                        child: Text('Explicit ON / OFF · two lines'),
                      ),
                    ],
                    onChanged: (value) {
                      if (value != null) {
                        preferences.relayLayout = value;
                        preferences.save();
                      }
                    },
                  ),
                ]),
                _section(context, 'Branding', Icons.badge_outlined, [
                  for (final brand in ['pccontroller', 'pealayer'])
                    ListTile(
                      leading: Icon(
                        preferences.branding == brand
                            ? Icons.radio_button_checked
                            : Icons.radio_button_unchecked,
                      ),
                      title: Text(
                        brand == 'pealayer' ? 'Pealayer' : 'PCController',
                      ),
                      trailing: Image.asset(
                        'assets/branding/$brand.png',
                        width: 32,
                        height: 32,
                      ),
                      onTap: () async {
                        try {
                          await preferences.setBranding(brand);
                        } catch (_) {
                          if (context.mounted) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(
                                content: Text(
                                  'Android branding change failed.',
                                ),
                              ),
                            );
                          }
                        }
                      },
                    ),
                ]),
                _section(context, 'Launcher icons', Icons.apps, [
                  for (final entry in [
                    ('pccontroller', 'PCController'),
                    ('pealayer', 'Pealayer'),
                  ])
                    CheckboxListTile(
                      contentPadding: EdgeInsets.zero,
                      secondary: Image.asset(
                        'assets/branding/${entry.$1}.png',
                        width: 36,
                        height: 36,
                      ),
                      title: Text(entry.$2),
                      value: preferences.launchers.contains(entry.$1),
                      onChanged: (v) async {
                        final next = Set<String>.of(preferences.launchers);
                        if (v == true) {
                          next.add(entry.$1);
                        } else {
                          next.remove(entry.$1);
                        }
                        if (next.isEmpty) return;
                        try {
                          await preferences.setLaunchers(next);
                        } catch (_) {
                          if (context.mounted) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(
                                content: Text(
                                  'Launcher change failed. Try again.',
                                ),
                              ),
                            );
                          }
                        }
                      },
                    ),
                ]),
                _section(context, 'Discovery', Icons.radar, [
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Auto-connect discovered peers'),
                    value: preferences.autoConnect,
                    onChanged: (v) {
                      preferences.autoConnect = v;
                      preferences.save();
                      changed();
                    },
                  ),
                ]),
                _section(context, 'Peer updates', Icons.system_update, [
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Share app package on this network'),
                    value: preferences.sharePackages,
                    onChanged: (v) {
                      preferences.sharePackages = v;
                      preferences.save();
                      changed();
                    },
                  ),
                ]),
                _section(context, 'Mirroring', Icons.speaker_outlined, [
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Mirror board beeps on this device'),
                    value: preferences.mirrorBuzzer,
                    onChanged: (v) {
                      preferences.mirrorBuzzer = v;
                      preferences.save();
                      changed();
                    },
                  ),
                  Row(
                    children: [
                      const Icon(Icons.volume_down_outlined),
                      Expanded(
                        child: Slider(
                          value: preferences.mirrorVolume,
                          label: '${(preferences.mirrorVolume * 100).round()}%',
                          onChanged: (v) {
                            preferences.mirrorVolume = v;
                            preferences.refresh();
                          },
                          onChangeEnd: (_) {
                            preferences.save();
                            changed();
                          },
                        ),
                      ),
                      Text('${(preferences.mirrorVolume * 100).round()}%'),
                    ],
                  ),
                ]),
                connections,
                automation,
              ])
                SizedBox(
                  width: bounds.maxWidth >= 900
                      ? (bounds.maxWidth - 46) / 2
                      : bounds.maxWidth - 32,
                  child: section,
                ),
            ],
          ),
        ),
      ),
    ),
  );
  Future<Color?> _customColor(BuildContext context) async {
    final text = TextEditingController(
      text: preferences.customColor
          .toARGB32()
          .toRadixString(16)
          .substring(2)
          .toUpperCase(),
    );
    final key = GlobalKey<FormState>();
    final result = await showDialog<Color>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Custom palette'),
        content: Form(
          key: key,
          child: TextFormField(
            controller: text,
            maxLength: 6,
            decoration: const InputDecoration(
              labelText: 'Color',
              prefixText: '#',
            ),
            validator: (value) =>
                RegExp(r'^[0-9a-fA-F]{6}$').hasMatch(value ?? '')
                ? null
                : 'Enter six hexadecimal digits.',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () {
              if (key.currentState!.validate()) {
                Navigator.pop(
                  context,
                  Color(0xff000000 | int.parse(text.text, radix: 16)),
                );
              }
            },
            child: const Text('Apply'),
          ),
        ],
      ),
    );
    text.dispose();
    return result;
  }

  Widget _section(
    BuildContext context,
    String title,
    IconData icon,
    List<Widget> children,
  ) => Padding(
    padding: EdgeInsets.zero,
    child: Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(icon, size: 22),
                const SizedBox(width: 10),
                Text(title, style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            ...children,
          ],
        ),
      ),
    ),
  );
}
