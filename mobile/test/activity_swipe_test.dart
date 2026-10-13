import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/activity_swipe.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:pccontroller_mobile/unified/preferences.dart';
import 'package:pccontroller_mobile/unified/workspace_chrome.dart';

void main() {
  for (final size in [const Size(360, 800), const Size(1280, 800)]) {
    for (final direction in TextDirection.values) {
      testWidgets(
        'activity swipes follow visible tabs at $size in $direction',
        (tester) async {
          SharedPreferences.setMockInitialValues({});
          tester.view.physicalSize = size;
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.resetPhysicalSize);
          addTearDown(tester.view.resetDevicePixelRatio);
          final preferences = AppPreferences();
          addTearDown(preferences.dispose);
          await tester.pumpWidget(
            MaterialApp(
              home: Directionality(
                textDirection: direction,
                child: ControllerHome(restore: false, preferences: preferences),
              ),
            ),
          );
          final dynamic home = tester.state(find.byType(ControllerHome));
          final PeerSession board = home.board;
          board.online = true;
          board.received = DateTime.now();
          board.state = {'connected': false};
          board.notifyListeners();
          await tester.pumpAndSettle();
          final dx = direction == TextDirection.ltr ? -160.0 : 160.0;
          Future<void> swipe(double delta) async {
            final bounds = tester.getRect(find.byType(ActivitySwipe));
            await tester.dragFrom(
              bounds.bottomCenter - const Offset(0, 80),
              Offset(delta, 0),
            );
            await tester.pumpAndSettle();
          }

          await swipe(dx);
          expect(
            home.section,
            3,
          ); // Channels/Panel are unavailable in host-only mode.
          expect(
            tester.widget<ActivityStrip>(find.byType(ActivityStrip)).selected,
            3,
          );
          home.displayText.text = 'Retained draft';
          await swipe(-dx);
          expect(home.section, 0);
          expect(home.displayText.text, 'Retained draft');
          await swipe(-dx); // No wrapping at the first activity.
          expect(home.section, 0);
          board.disconnect();
          await tester.pumpAndSettle();
          await swipe(dx);
          expect(home.section, 0);
          expect(find.byType(ActivityStrip), findsNothing);
          expect(tester.takeException(), null);
          await tester.pumpWidget(const SizedBox());
        },
      );
    }
  }

  testWidgets('swiping away and back restores activity scroll and drafts', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    tester.view.physicalSize = const Size(412, 650);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(const UnifiedControllerApp(restore: false));
    final dynamic home = tester.state(find.byType(ControllerHome));
    final PeerSession board = home.board;
    board.online = true;
    board.received = DateTime.now();
    board.state = {
      'connected': true,
      'have_front_panel': true,
      'front_panel': {
        'host_captured': false,
        'lcd_available': false,
        'pressed_keys': 0,
        'menu_page': 1,
      },
    };
    home.displayText.text = 'Unsaved segment draft';
    home.selectActivity(2);
    await tester.pumpAndSettle();
    Finder scroll() =>
        find.byKey(const PageStorageKey('activity-scroll-panel'));
    ScrollPosition position() => tester
        .state<ScrollableState>(
          find
              .descendant(of: scroll(), matching: find.byType(Scrollable))
              .first,
        )
        .position;
    expect(position().maxScrollExtent, greaterThan(40));
    position().jumpTo(40);
    await tester.pump();
    final viewport = tester.getRect(find.byType(ActivitySwipe));
    await tester.dragFrom(
      viewport.topRight + const Offset(-18, 30),
      const Offset(-160, 0),
    );
    await tester.pumpAndSettle();
    expect(home.section, 3);
    await tester.dragFrom(
      viewport.topLeft + const Offset(18, 30),
      const Offset(160, 0),
    );
    await tester.pumpAndSettle();
    expect(home.section, 2);
    expect(position().pixels, 40);
    expect(home.displayText.text, 'Unsaved segment draft');
    expect(tester.takeException(), null);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('slider and nested horizontal scroller keep their gestures', (
    tester,
  ) async {
    var selected = 0, value = .4;
    final controller = ScrollController();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, update) => ActivitySwipe(
              selected: selected,
              activities: const [0, 3, 4],
              changed: (next) => update(() => selected = next),
              child: Column(
                children: [
                  Slider(
                    value: value,
                    onChanged: (next) => update(() => value = next),
                  ),
                  SizedBox(
                    height: 100,
                    child: SingleChildScrollView(
                      key: const ValueKey('child-scroll'),
                      controller: controller,
                      scrollDirection: Axis.horizontal,
                      child: const SizedBox(
                        width: 1800,
                        height: 100,
                        child: ColoredBox(color: Colors.teal),
                      ),
                    ),
                  ),
                  const Expanded(child: SizedBox.expand()),
                ],
              ),
            ),
          ),
        ),
      ),
    );
    await tester.drag(find.byType(Slider), const Offset(130, 0));
    await tester.pumpAndSettle();
    expect(value, greaterThan(.4));
    expect(selected, 0);
    await tester.drag(
      find.byKey(const ValueKey('child-scroll')),
      const Offset(-150, 0),
    );
    await tester.pumpAndSettle();
    expect(controller.offset, greaterThan(0));
    expect(selected, 0);
    await tester.pumpWidget(const SizedBox());
    controller.dispose();
  });

  testWidgets(
    'short, canceled and stale-configuration gestures cannot navigate',
    (tester) async {
      var selected = 0;
      var available = <int>[0, 3, 4];
      late StateSetter update;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, state) {
                update = state;
                return ActivitySwipe(
                  selected: selected,
                  activities: available,
                  changed: (next) => update(() => selected = next),
                  child: const SizedBox.expand(),
                );
              },
            ),
          ),
        ),
      );
      final center = tester.getCenter(find.byType(ActivitySwipe));
      await tester.dragFrom(center, const Offset(-10, 0));
      await tester.pumpAndSettle();
      expect(selected, 0);
      final canceled = await tester.startGesture(center);
      await canceled.moveBy(const Offset(-140, 0));
      await canceled.cancel();
      await tester.pumpAndSettle();
      expect(selected, 0);
      final stale = await tester.startGesture(center);
      await stale.moveBy(const Offset(-140, 0));
      await tester.pump();
      update(() => available = [0, 4]);
      await tester.pump();
      await stale.up();
      await tester.pumpAndSettle();
      expect(selected, 0);
      update(() => selected = 4);
      await tester.pump();
      await tester.dragFrom(center, const Offset(-160, 0));
      await tester.pumpAndSettle();
      expect(selected, 4); // No wrapping at the final visible activity.
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
