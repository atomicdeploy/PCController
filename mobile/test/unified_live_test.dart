import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  final origin = Platform.environment['PC_MOBILE_SMOKE_ORIGIN'];
  test('Opt-in read-only current-host snapshot/catalog/WebSocket acceptance', () async {
    final session = PeerSession(controller: true);
    try {
      await session.connect(origin!, '');
      final deadline = DateTime.now().add(const Duration(seconds: 10));
      while (!session.subscribed && DateTime.now().isBefore(deadline)) {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      }
      expect(session.error, null);
      expect(session.subscribed, true);
      expect(session.identity, isNotEmpty);
      expect(session.state['connected'], isA<bool>());
      expect(session.peripherals['controls'], isA<List>());
      // All calls above are reads and subscription; no output/programming RPC.
    } finally {
      session.dispose();
    }
  }, skip: origin == null);
}
