import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

Json push(
  int revision, {
  int generation = 1,
  String owner = 'primary',
  bool available = true,
}) => {
  'host_instance_id': owner,
  'connection_generation': generation,
  'revision': revision,
  'time': DateTime.now().toUtc().toIso8601String(),
  'available': available,
  'selected_channel': 0,
  'values': List.generate(16, (i) => i * 100),
  'channels': List.generate(
    16,
    (i) => {'channel': i, 'raw_value': i * 100, 'logical_percent': i * 5.0},
  ),
};

void main() {
  test('complete calibrated pushes preserve true zero and reject old owners and revisions', () {
    final session = PeerSession(controller: true)
      ..state = {
        'host_instance_id': 'primary',
        'connection_generation': 1,
        'connected': true,
      };
    addTearDown(session.dispose);
    expect(session.acceptPwmPush(push(2)), true);
    expect(session.pwmValues['channels'][0]['logical_percent'], 0);
    expect(session.pwmValues['channels'][3]['logical_percent'], 15);
    expect(session.acceptPwmPush(push(1)), false);
    expect(session.acceptPwmPush(push(3, generation: 0)), false);
    expect(session.acceptPwmPush(push(3, owner: 'another')), false);
    expect(session.acceptPwmPush({...push(3), 'channels': []}), false);
    expect(session.pwmValues['revision'], 2);
    expect(session.acceptPwmPush(push(3, available: false)), true);
    expect(session.pwmValues['revision'], 2);
    session.state['connected'] = false;
    expect(session.acceptPwmPush(push(4)), false);
  });
}
