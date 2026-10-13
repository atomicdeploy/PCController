import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/unified/controller_dialog.dart';
import 'package:pccontroller_mobile/unified/endpoint_address.dart';
import 'package:pccontroller_mobile/unified/endpoint_detector.dart';
import 'package:pccontroller_mobile/unified/endpoint_dialog.dart';

class ControlledDetector extends EndpointDetector {
  ControlledDetector()
    : super(client: MockClient((_) async => http.Response('', 503)));
  final result = Completer<DetectedEndpoint>();
  int checks = 0;
  bool closed = false;
  EndpointAddress? input;
  @override
  Future<DetectedEndpoint> detect(
    EndpointAddress address,
    String product, {
    Iterable<String> advertised = const [],
    bool Function()? canceled,
  }) {
    checks++;
    input = address;
    return result.future;
  }

  @override
  void close() {
    closed = true;
    super.close();
  }
}

Future<void> openDialog(
  WidgetTester tester, {
  ControlledDetector? detector,
  void Function(String?)? returned,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => TextButton(
            onPressed: () async {
              final result = await showDialog<String>(
                context: context,
                builder: (_) => EndpointDialog(
                  controller: true,
                  detectorFactory: detector == null ? null : () => detector,
                ),
              );
              returned?.call(result);
            },
            child: const Text('Open'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('Open'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'editing during detection rejects its late result and keeps the new target',
    (tester) async {
      final detector = ControlledDetector();
      String? selected;
      await openDialog(
        tester,
        detector: detector,
        returned: (value) => selected = value,
      );
      final field = find.byKey(const ValueKey('peer-address'));
      await tester.enterText(field, 'old-host:8787');
      await tester.pump();
      await tester.tap(find.text('Detect'));
      await tester.pump();
      expect(detector.checks, 1);
      await tester.enterText(field, 'new-host:8787');
      await tester.pump();
      detector.result.complete(
        DetectedEndpoint(Uri.parse('http://old-host:8787'), 'pccontroller'),
      );
      await tester.pumpAndSettle();
      expect(selected, null);
      expect(tester.widget<TextField>(field).controller!.text, 'new-host:8787');
      expect(find.byType(SelectableText), findsNothing);
      expect(find.text('PCController verified'), findsNothing);
      expect(detector.closed, true);
      await tester.tap(find.byTooltip('Close'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'address field validates live, normalizes localized digits and keeps errors inside modal',
    (tester) async {
      await openDialog(tester);
      final field = find.byKey(const ValueKey('peer-address'));
      expect(
        (tester.widget<TextField>(field)).textDirection,
        TextDirection.ltr,
      );
      await tester.enterText(field, '۱۹۲.۱۶۸.۱.۲۰:۶۵۵۳۶');
      await tester.pump();
      expect(
        (tester.widget<TextField>(field)).controller!.text,
        '192.168.1.20:65536',
      );
      expect(
        find.text('Enter a port from 1 to 65535 after the colon.'),
        findsOneWidget,
      );
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, 'Connect'))
            .onPressed,
        null,
      );
      expect(find.byType(SnackBar), findsNothing);
      await tester.enterText(field, 'HOST.LOCAL:8787');
      await tester.pump();
      expect(
        tester.widget<TextField>(field).controller!.text,
        'HOST.LOCAL:8787',
      );
      expect(find.byType(TextField), findsOneWidget);
      expect(find.byType(SelectableText), findsNothing);
      expect(
        find.text('Enter a port from 1 to 65535 after the colon.'),
        findsNothing,
      );
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, 'Connect'))
            .onPressed,
        isNotNull,
      );
      await tester.tap(find.byTooltip('Close'));
      await tester.pumpAndSettle();
      expect(find.byType(EndpointDialog), findsNothing);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'verified connection returns only after the read-only check completes',
    (tester) async {
      final detector = ControlledDetector();
      String? selected;
      await openDialog(
        tester,
        detector: detector,
        returned: (value) => selected = value,
      );
      await tester.enterText(
        find.byKey(const ValueKey('peer-address')),
        'workshop',
      );
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pump();
      expect(detector.checks, 1);
      expect(detector.input!.origin, Uri.parse('http://workshop:8787'));
      expect(detector.input!.explicitPort, false);
      expect(detector.input!.explicitScheme, false);
      expect(
        tester
            .widget<TextField>(find.byKey(const ValueKey('peer-address')))
            .controller!
            .text,
        'http://workshop:8787',
      );
      expect(selected, null);
      expect(find.byType(LinearProgressIndicator), findsOneWidget);
      detector.result.complete(
        DetectedEndpoint(Uri.parse('http://workshop:9080'), 'pccontroller'),
      );
      await tester.pumpAndSettle();
      expect(selected, 'http://workshop:9080');
      expect(detector.closed, true);
      expect(find.byType(EndpointDialog), findsNothing);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'single normalized endpoint retains short-host detection intent on retry',
    (tester) async {
      final detectors = [ControlledDetector(), ControlledDetector()];
      var attempt = 0;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: EndpointDialog(
              controller: true,
              detectorFactory: () => detectors[attempt++],
            ),
          ),
        ),
      );
      await tester.enterText(
        find.byKey(const ValueKey('peer-address')),
        'WORKSHOP',
      );
      await tester.pump();
      await tester.tap(find.text('Detect'));
      await tester.pump();
      detectors[0].result.completeError(
        const FormatException('No verified PCController endpoint found.'),
      );
      await tester.pumpAndSettle();
      expect(find.byType(TextField), findsOneWidget);
      expect(find.byType(SelectableText), findsNothing);
      expect(
        tester
            .widget<TextField>(find.byKey(const ValueKey('peer-address')))
            .controller!
            .text,
        'http://workshop:8787',
      );
      await tester.tap(find.text('Detect'));
      await tester.pump();
      expect(detectors[1].input!.explicitPort, false);
      expect(detectors[1].input!.explicitScheme, false);
      detectors[1].result.complete(
        DetectedEndpoint(Uri.parse('http://workshop:9080'), 'pccontroller'),
      );
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<TextField>(find.byKey(const ValueKey('peer-address')))
            .controller!
            .text,
        'http://workshop:9080',
      );
      expect(find.byTooltip('Endpoint verified'), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('peer-address')),
        'wss://other.example:9443/ws',
      );
      await tester.pump();
      expect(find.byTooltip('Endpoint verified'), findsNothing);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'cancel during endpoint detection cannot return a late connection',
    (tester) async {
      final detector = ControlledDetector();
      final results = <String?>[];
      await openDialog(tester, detector: detector, returned: results.add);
      await tester.enterText(
        find.byKey(const ValueKey('peer-address')),
        'workshop:8787',
      );
      await tester.pump();
      await tester.tap(find.text('Detect'));
      await tester.pump();
      expect(detector.checks, 1);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      detector.result.complete(
        DetectedEndpoint(Uri.parse('http://workshop:8787'), 'pccontroller'),
      );
      await tester.pump();
      expect(results, [null]);
      expect(detector.closed, true);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final mode in ['phone', 'tablet-rtl', 'keyboard']) {
    testWidgets('shared modal remains usable in $mode geometry', (
      tester,
    ) async {
      tester.view.physicalSize = mode == 'tablet-rtl'
          ? const Size(1280, 800)
          : const Size(320, 480);
      tester.view.devicePixelRatio = 1;
      if (mode == 'keyboard') {
        tester.view.viewInsets = const FakeViewPadding(bottom: 280);
      }
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetViewInsets);
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData(
            brightness: mode == 'tablet-rtl'
                ? Brightness.dark
                : Brightness.light,
          ),
          home: Directionality(
            textDirection: mode == 'tablet-rtl'
                ? TextDirection.rtl
                : TextDirection.ltr,
            child: const Scaffold(
              body: ControllerDialog(
                title: 'Connect PCController',
                icon: Icons.memory_outlined,
                actions: [
                  TextButton(onPressed: null, child: Text('Cancel')),
                  OutlinedButton(onPressed: null, child: Text('Detect')),
                  FilledButton(onPressed: null, child: Text('Connect')),
                ],
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [TextField(), SizedBox(height: 72)],
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byTooltip('Close'), findsOneWidget);
      expect(find.text('Connect'), findsOneWidget);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    });
  }
}
