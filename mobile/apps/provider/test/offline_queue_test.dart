import 'dart:async';
import 'dart:convert';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:homend_core/homend_core.dart';
import 'package:homend_provider/offline_queue.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences_platform_interface/in_memory_shared_preferences_async.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_async_platform_interface.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late bool online;
  late List<Map<String, dynamic>> sent;
  late String Function(Map<String, dynamic> event) verdict;
  late StreamController<List<ConnectivityResult>> network;

  OfflineQueue queue() {
    final api = HomendApi(
      ApiClient(
        baseUrl: 'https://x/api/v1',
        httpClient: MockClient((r) async {
          if (!online) throw http.ClientException('no signal');
          expect(r.url.path, '/api/v1/provider/sync');
          final events = ((jsonDecode(r.body) as Map)['events'] as List).cast<Map<String, dynamic>>();
          sent.addAll(events);
          return http.Response(
            jsonEncode({
              'results': [
                for (final e in events)
                  {
                    'client_event_id': e['client_event_id'],
                    'status': verdict(e),
                    if (verdict(e) == 'rejected') 'error': {'code': 'rule_violation', 'message': 'Wrong door code'},
                  },
              ],
            }),
            200,
          );
        }),
      ),
    );
    return OfflineQueue(api, connectivity: network.stream);
  }

  setUp(() {
    SharedPreferencesAsyncPlatform.instance = InMemorySharedPreferencesAsync.empty();
    online = true;
    sent = [];
    verdict = (_) => 'applied';
    network = StreamController.broadcast();
  });

  test('a check-in made online is sent at once and leaves the queue', () async {
    final q = queue();
    await q.start();
    await q.add('v1', 'check_in', {'method': 'code', 'code': '4821', 'lat': null});
    expect(sent, hasLength(1));
    expect(sent.single['event_type'], 'check_in');
    expect(sent.single['visit_id'], 'v1');
    expect(sent.single['payload'], {'method': 'code', 'code': '4821'});
    expect(sent.single['device_recorded_at'], endsWith('Z'));
    expect(q.events, isEmpty);
  });

  test('without signal the event waits on the phone, survives a restart, and is sent when back online', () async {
    online = false;
    final first = queue();
    await first.start();
    final at = DateTime.utc(2026, 10, 16, 7, 58);
    await first.add('v1', 'check_in', {'method': 'geo', 'lat': -1.29, 'lng': 36.78}, at: at);
    expect(first.waitingCount, 1);
    first.dispose();

    // The app is closed and opened again, still offline.
    final second = queue();
    await second.start();
    expect(second.waitingCount, 1);
    expect(sent, isEmpty);

    online = true;
    network.add([ConnectivityResult.mobile]);
    await pumpEventQueue();
    expect(sent, hasLength(1));
    expect(sent.single['device_recorded_at'], '2026-10-16T07:58:00.000Z', reason: 'the phone time is kept');
    expect(second.events, isEmpty);
  });

  test('a refused event stays with the server message until dismissed; replays count as sent', () async {
    final q = queue();
    await q.start();
    verdict = (e) => e['payload']['code'] == '0000' ? 'rejected' : 'duplicate';
    await q.add('v1', 'check_in', {'method': 'code', 'code': '0000'});
    await q.add('v2', 'check_out', {'notes': 'Done'});
    expect(q.events, hasLength(1));
    final bad = q.events.single;
    expect(bad.rejectedMessage, 'Wrong door code');
    expect(q.waitingCount, 0);

    await q.flush();
    expect(sent, hasLength(2), reason: 'refused events are not resent');

    await q.dismiss(bad.clientEventId);
    expect(q.events, isEmpty);
  });

  test('clear removes everything from the phone', () async {
    online = false;
    final q = queue();
    await q.start();
    await q.add('v1', 'check_in', {'method': 'code', 'code': '1234'});
    await q.clear();
    expect(q.events, isEmpty);
    final again = queue();
    await again.start();
    expect(again.events, isEmpty);
  });
}
