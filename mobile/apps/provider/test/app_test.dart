import 'dart:async';
import 'dart:convert';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:homend_core/homend_core.dart';
import 'package:homend_provider/app.dart';
import 'package:homend_provider/offline_queue.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences_platform_interface/in_memory_shared_preferences_async.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_async_platform_interface.dart';

/// A tiny stand-in for the backend with one active provider, one offer and one job today.
class FakeBackend {
  final calls = <String>[];
  final syncBodies = <Map<String, dynamic>>[];
  String providerStatus = 'active';

  static final today = DateTime.now().toUtc();
  static final service = {
    'id': 's1',
    'category_id': 'c1',
    'slug': 'deep-clean',
    'name': 'Deep clean',
    'pricing_unit': 'per_hour',
    'scope_items': ['Kitchen', 'Bathrooms'],
    'is_active': true,
  };
  static final job = {
    'assignment_id': 'as1',
    'request_reference': 'REQ-2026-000001',
    'status': 'confirmed',
    'service': service,
    'client_display_name': 'Wanjiru K.',
    'address': {
      'id': 'a1',
      'label': 'Home',
      'estate': 'Kilimani Gardens',
      'house_number': 'B4',
      'access_details': 'Gate code 1234',
      'is_default': true,
    },
    'visits': [
      {
        'id': 'v1',
        'request_id': 'r1',
        'status': 'scheduled',
        'scheduled_start': today.add(const Duration(minutes: 1)).toIso8601String(),
        'scheduled_end': today.add(const Duration(hours: 4)).toIso8601String(),
      },
    ],
    'provider_pay_minor': 180000,
  };

  http.Client get client => MockClient((r) async {
    final path = r.url.path.replaceFirst('/api/v1', '');
    calls.add('${r.method} $path');
    Object? body;
    switch ('${r.method} $path') {
      case 'GET /me':
        body = {
          'id': 'u1',
          'kind': 'provider',
          'status': 'active',
          'full_name': 'Grace Wambui',
          'phone': '+254712345678',
          'provider_status': providerStatus,
        };
      case 'GET /provider/complaints':
        body = [];
      case 'GET /provider/offers':
        body = [
          {
            'id': 'o1',
            'status': 'offered',
            'offer_expires_at': today.add(const Duration(hours: 2)).toIso8601String(),
            'request_reference': 'REQ-2026-000002',
            'service': service,
            'estate': 'Lavington',
            'requested_start': today.add(const Duration(days: 1)).toIso8601String(),
            'provider_pay_minor': 150000,
          },
        ];
      case 'GET /provider/jobs':
        body = {
          'data': [job],
          'meta': {'next_cursor': null, 'limit': 50},
        };
      case 'GET /provider/jobs/as1':
        body = job;
      case 'GET /provider/earnings/summary':
        body = {'pending_minor': 0, 'payable_minor': 360000, 'held_minor': 0, 'paid_this_month_minor': 0};
      case 'GET /provider/applications':
        body = [];
      case 'POST /provider/sync':
        final b = jsonDecode(r.body) as Map<String, dynamic>;
        syncBodies.add(b);
        body = {
          'results': [
            for (final e in b['events'] as List) {'client_event_id': e['client_event_id'], 'status': 'applied'},
          ],
        };
      default:
        return http.Response(jsonEncode({'code': 'not_found', 'message': 'Not here'}), 404);
    }
    return http.Response(jsonEncode(body), 200, headers: {'content-type': 'application/json'});
  });
}

Future<(SessionController, OfflineQueue)> boot(FakeBackend backend, {String? token = 'tok'}) async {
  SharedPreferencesAsyncPlatform.instance = InMemorySharedPreferencesAsync.empty();
  final api = HomendApi(ApiClient(baseUrl: 'https://api.test/api/v1', httpClient: backend.client));
  final session = SessionController(api: api, store: MemoryTokenStore()..token = token, userKind: 'provider');
  final queue = OfflineQueue(api, connectivity: const Stream<List<ConnectivityResult>>.empty());
  await session.restore();
  await queue.start();
  return (session, queue);
}

/// A typical Android phone: 1080 × 2400 at 2.625 density.
void phone(WidgetTester tester) {
  tester.view.physicalSize = const Size(1080, 2400);
  tester.view.devicePixelRatio = 2.625;
  addTearDown(tester.view.reset);
}

void main() {
  testWidgets('signed out, the app opens on sign-in with a way to apply', (tester) async {
    final (session, queue) =
        await tester.runAsync(() => boot(FakeBackend(), token: null)) as (SessionController, OfflineQueue);
    await tester.pumpWidget(ProviderApp(session: session, queue: queue));
    await tester.pumpAndSettle();
    expect(find.text('Sign in to your work account'), findsOneWidget);
    expect(find.text('New? Apply to work with us'), findsOneWidget);

    await tester.tap(find.text('New? Apply to work with us'));
    await tester.pumpAndSettle();
    expect(find.text('Work with Homend'), findsOneWidget);
    expect(find.textContaining('background checks'), findsOneWidget);
  });

  testWidgets('an active provider sees offers and today, then checks in with the door code', (tester) async {
    phone(tester);
    final backend = FakeBackend();
    final (session, queue) = await tester.runAsync(() => boot(backend)) as (SessionController, OfflineQueue);
    await tester.pumpWidget(ProviderApp(session: session, queue: queue));
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
    await tester.pumpAndSettle();

    expect(find.text('Hello, Grace'), findsOneWidget);
    expect(find.text('1 new job offer'), findsOneWidget);
    expect(find.text('KES 3,600'), findsOneWidget);

    await tester.tap(find.text('Deep clean · Kilimani Gardens'));
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
    await tester.pumpAndSettle();
    expect(find.text('Access: Gate code 1234'), findsOneWidget);
    expect(find.text('Check in'), findsOneWidget);

    await tester.enterText(find.widgetWithText(TextField, '4-digit code'), '4821');
    await tester.ensureVisible(find.text('Check in'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Check in'));
    for (var i = 0; i < 5; i++) {
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
      await tester.pump();
    }
    expect(backend.syncBodies, hasLength(1));
    final event = (backend.syncBodies.single['events'] as List).single as Map;
    expect(event['visit_id'], 'v1');
    expect(event['event_type'], 'check_in');
    expect(event['payload'], {'method': 'code', 'code': '4821'});
  });

  testWidgets('an applicant is asked to start an application', (tester) async {
    final backend = FakeBackend()..providerStatus = 'applicant';
    final (session, queue) = await tester.runAsync(() => boot(backend)) as (SessionController, OfflineQueue);
    await tester.pumpWidget(ProviderApp(session: session, queue: queue));
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
    await tester.pumpAndSettle();
    expect(find.text('Start my application'), findsOneWidget);
  });
}
