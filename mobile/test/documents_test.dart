import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/documents.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('Actual donor extension rule is case-insensitive, with no false suffix match', () {
    expect(preferenceFileAllowed('firmware.BIN', ['bin']), true);
    expect(preferenceFileAllowed('firmware.bin.exe', ['bin']), false);
    expect(preferenceFileAllowed('firmwarebin', ['bin']), false);
  });
  test(
    'Native picker cancel and bounded .bin selection do not apply firmware',
    () async {
      final calls = <MethodCall>[];
      Object? reply;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AndroidDocuments.channel, (call) async {
            calls.add(call);
            return reply;
          });
      addTearDown(
        () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(AndroidDocuments.channel, null),
      );
      final picker = AndroidDocuments();
      expect(await picker.pick(), isNull);
      reply = {'name': 'firmware.BIN', 'bytes': Uint8List(32768)};
      expect((await picker.pick())!.bytes.length, 32768);
      reply = {'name': 'firmware.hex', 'bytes': Uint8List(32768)};
      await expectLater(picker.pick(), throwsFormatException);
      reply = {'name': 'firmware.bin', 'bytes': Uint8List(32769)};
      await expectLater(picker.pick(maximum: 32768), throwsFormatException);
      expect(calls.every((call) => call.method == 'pick'), true);
    },
  );
  test('Changed owner while choosing export abandons token; never writes or deletes', () async {
    var owned = true;
    final methods = <String>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AndroidDocuments.channel, (call) async {
          methods.add(call.method);
          if (call.method == 'create') {
            owned = false;
            return 'one-use-token';
          }
          return null;
        });
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AndroidDocuments.channel, null),
    );
    await expectLater(
      AndroidDocuments().export(
        'avr-flash.bin',
        Uint8List(32768),
        stillOwned: () => owned,
      ),
      throwsFormatException,
    );
    expect(methods, ['create', 'abandon']);
  });
  test(
    'Export writes exact bytes once to only the newly-created token',
    () async {
      final calls = <MethodCall>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AndroidDocuments.channel, (call) async {
            calls.add(call);
            return call.method == 'create' ? 'one-use-token' : null;
          });
      addTearDown(
        () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(AndroidDocuments.channel, null),
      );
      expect(
        await AndroidDocuments().export(
          'avr-eeprom.bin',
          Uint8List(1024),
          stillOwned: () => true,
        ),
        true,
      );
      expect(calls.map((c) => c.method), ['create', 'write']);
      expect(calls.last.arguments['token'], 'one-use-token');
      expect((calls.last.arguments['bytes'] as Uint8List).length, 1024);
    },
  );
}
