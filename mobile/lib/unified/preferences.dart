import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

class AppPreferences extends ChangeNotifier {
  void refresh() => notifyListeners();
  ThemeMode theme = ThemeMode.system;
  String layout = 'auto';
  bool compact = false;
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
    compact = prefs.getBool('appearance.compact') ?? false;
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
  }

  Future<void> save() async {
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('appearance.theme', theme.name);
    await prefs.setString('appearance.layout', layout);
    await prefs.setBool('appearance.compact', compact);
    await prefs.setBool('mirror.buzzer', mirrorBuzzer);
    await prefs.setDouble('mirror.volume', mirrorVolume);
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
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _section(context, 'Appearance', Icons.palette_outlined, [
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
                          content: Text('Launcher change failed. Try again.'),
                        ),
                      );
                    }
                  }
                },
              ),
            const Text('Select one icon or keep both mini-app launchers.'),
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
          const SizedBox(height: 16),
          automation,
        ],
      ),
    ),
  );
  Widget _section(
    BuildContext context,
    String title,
    IconData icon,
    List<Widget> children,
  ) => Padding(
    padding: const EdgeInsets.only(bottom: 16),
    child: Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
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
