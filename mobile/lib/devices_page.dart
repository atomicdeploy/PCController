import 'package:flutter/material.dart';

import 'device_registry.dart';
import 'discovery.dart';
import 'icons.dart';

class DevicesPage extends StatefulWidget {
  const DevicesPage({super.key, required this.select});
  final Future<void> Function(SavedDevice, String) select;
  @override
  State<DevicesPage> createState() => _DevicesPageState();
}

class _DevicesPageState extends State<DevicesPage> {
  final registry = DeviceRegistry(), discovery = DeviceDiscovery();
  List<SavedDevice> saved = [];
  List<DiscoveredDevice> found = [];
  bool busy = false;
  String message = '';
  @override
  void initState() {
    super.initState();
    reload();
    scan();
  }

  Future<void> reload() async {
    try {
      final v = await registry.load();
      if (mounted) setState(() => saved = v);
    } on Object {
      if (mounted) {
        setState(
          () => message =
              'Saved devices unavailable; existing data was not replaced.',
        );
      }
    }
  }

  Future<void> scan() async {
    if (busy) return;
    setState(() => busy = true);
    final v = await discovery.scan();
    if (!mounted) return;
    setState(() {
      found = v;
      busy = false;
      message = discovery.errors.isEmpty
          ? '${v.length} advertisements · unverified until connected'
          : '${v.length} advertisements; ${discovery.errors.keys.join(', ')} unavailable';
    });
  }

  Future<void> edit({SavedDevice? device, DiscoveredDevice? discovered}) async {
    final name = TextEditingController(
      text: device?.name ?? discovered?.name ?? 'New device',
    );
    final endpoint = TextEditingController(
      text: device?.endpoint ?? discovered?.endpoint ?? 'http://ledboard.local',
    );
    final user = TextEditingController(text: device?.user ?? 'admin');
    final password = TextEditingController();
    var location = device?.location ?? DeviceLocation.local;
    var error = '', writing = false;
    await showDialog<void>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, update) => PopScope(
          canPop: !writing,
          child: AlertDialog(
            title: Text(device == null ? 'Save device' : 'Device settings'),
            content: SizedBox(
              width: 480,
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    TextField(
                      controller: name,
                      decoration: const InputDecoration(
                        labelText: 'Display name',
                      ),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: endpoint,
                      decoration: const InputDecoration(
                        labelText: 'Hostname / HTTP(S) origin',
                      ),
                    ),
                    const SizedBox(height: 12),
                    DropdownButtonFormField<DeviceLocation>(
                      initialValue: location,
                      items: [
                        for (final l in DeviceLocation.values)
                          DropdownMenuItem(value: l, child: Text(l.name)),
                      ],
                      onChanged: writing
                          ? null
                          : (v) => update(() => location = v!),
                      decoration: const InputDecoration(
                        labelText: 'Local or remote transport',
                      ),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: user,
                      decoration: const InputDecoration(labelText: 'Username'),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: password,
                      obscureText: true,
                      decoration: InputDecoration(
                        labelText: device == null
                            ? 'Password · secure storage'
                            : 'New password · blank keeps existing',
                      ),
                    ),
                    const SizedBox(height: 12),
                    const Text(
                      'Board type, title and controls are read from the actual peer after connection. Discovery is not authorization.',
                    ),
                    if (error.isNotEmpty) Text(error),
                  ],
                ),
              ),
            ),
            actions: [
              TextButton(
                onPressed: writing ? null : () => Navigator.pop(context),
                child: const Text('Cancel'),
              ),
              FilledButton(
                onPressed: writing
                    ? null
                    : () async {
                        update(() => writing = true);
                        try {
                          final next = SavedDevice(
                            id: device?.id ?? randomIdentifier(),
                            name: name.text.trim(),
                            endpoint: endpoint.text,
                            location: location,
                            user: user.text.trim(),
                          );
                          final secret = password.text.isNotEmpty
                              ? password.text
                              : device == null
                              ? ''
                              : await registry.password(device) ?? '';
                          await registry.save(next, secret);
                          if (context.mounted) Navigator.pop(context);
                        } on Object {
                          update(() {
                            error = 'Device settings could not be saved; check origin and credentials.';
                            writing = false;
                          });
                        }
                      },
                child: const Text('Save'),
              ),
            ],
          ),
        ),
      ),
    );
    name.dispose();
    endpoint.dispose();
    user.dispose();
    password.dispose();
    await reload();
  }

  Future<void> connect(SavedDevice d) async {
    try {
      final secret = await registry.password(d);
      if (secret == null) throw StateError('Credentials unavailable');
      if (mounted) await widget.select(d, secret);
    } on Object {
      if (mounted) {
        setState(
          () => message = 'Device connection failed; no command replayed.',
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) => ListView(
    padding: const EdgeInsets.all(20),
    children: [
      Wrap(
        spacing: 12,
        children: [
          FilledButton.icon(
            onPressed: () => edit(),
            icon: const Icon(PakarIcons.link),
            label: const Text('Add local / remote device'),
          ),
          OutlinedButton.icon(
            onPressed: busy ? null : scan,
            icon: const Icon(PakarIcons.radio),
            label: const Text('Discover'),
          ),
        ],
      ),
      if (busy) const LinearProgressIndicator(),
      Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Text(message),
      ),
      Text('Saved devices', style: Theme.of(context).textTheme.titleLarge),
      for (final d in saved)
        Card(
          child: ListTile(
            leading: Icon(
              d.location == DeviceLocation.remote
                  ? PakarIcons.cloud
                  : PakarIcons.circuitry,
            ),
            title: Text(d.name),
            subtitle: Text(
              '${d.endpoint} · ${d.location.name} · type detected on connection',
            ),
            onTap: () => connect(d),
            trailing: PopupMenuButton<String>(
              onSelected: (v) async {
                if (v == 'settings') {
                  await edit(device: d);
                } else {
                  final yes = await showDialog<bool>(
                    context: context,
                    builder: (c) => AlertDialog(
                      title: const Text('Forget saved device?'),
                      content: Text(d.name),
                      actions: [
                        TextButton(
                          onPressed: () => Navigator.pop(c, false),
                          child: const Text('Cancel'),
                        ),
                        FilledButton(
                          onPressed: () => Navigator.pop(c, true),
                          child: const Text('Forget'),
                        ),
                      ],
                    ),
                  );
                  if (yes == true) {
                    await registry.remove(d);
                    await reload();
                  }
                }
              },
              itemBuilder: (_) => const [
                PopupMenuItem(value: 'settings', child: Text('Settings…')),
                PopupMenuItem(value: 'remove', child: Text('Forget device…')),
              ],
            ),
          ),
        ),
      const SizedBox(height: 20),
      Text(
        'Nearby advertisements',
        style: Theme.of(context).textTheme.titleLarge,
      ),
      for (final d in found)
        Card(
          child: ListTile(
            leading: const Icon(PakarIcons.broadcast),
            title: Text(d.name, maxLines: 2, overflow: TextOverflow.ellipsis),
            subtitle: Text('${d.endpoint} · ${d.protocol} · unverified'),
            trailing: OutlinedButton(
              onPressed: () => edit(discovered: d),
              child: const Text('Save…'),
            ),
          ),
        ),
    ],
  );
}
