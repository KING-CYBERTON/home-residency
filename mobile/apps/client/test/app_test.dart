import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:homend_client/app.dart';
import 'package:homend_core/homend_core.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A tiny stand-in for the backend: one household, one booking awaiting a rating, one unpaid invoice.
class FakeBackend {
  final calls = <String>[];
  final created = <http.Request>[];

  static final soon = DateTime.now().toUtc().add(const Duration(days: 1));
  static const deepClean = {
    'id': 's1',
    'category_id': 'c1',
    'slug': 'deep-clean',
    'name': 'Deep clean',
    'pricing_unit': 'per_hour',
    'description': 'Inside cupboards, ovens and windows too.',
    'scope_items': ['Oven', 'Windows'],
    'is_active': true,
    'current_price': {
      'id': 'rc1',
      'service_id': 's1',
      'client_price_minor': 60000,
      'valid_from': '2026-01-01T00:00:00Z',
    },
  };
  static final request = {
    'id': 'r1',
    'reference': 'REQ-2026-000001',
    'status': 'submitted',
    'service': deepClean,
    'total_minor': 60000,
    'requested_start': soon.toIso8601String(),
    'submitted_at': DateTime.now().toUtc().toIso8601String(),
    'address': {'id': 'a1', 'label': 'Home', 'estate': 'Kilimani Gardens', 'house_number': 'B4', 'is_default': true},
    'visits': [],
  };
  static final rated = {...request, 'id': 'r0', 'reference': 'REQ-2026-000000', 'status': 'awaiting_rating'};

  http.Client get client => MockClient((r) async {
    final path = r.url.path.replaceFirst('/api/v1', '');
    final key = '${r.method} $path';
    calls.add(key);
    Object? body;
    var status = 200;
    switch (key) {
      case 'GET /me':
        body = {
          'id': 'u1',
          'kind': 'client',
          'status': 'active',
          'full_name': 'Wanjiru Kamau',
          'phone': '+254781334473',
          'id_verification': 'none',
        };
      case 'GET /client/requests':
        body = {
          'data': [rated],
          'meta': {'limit': 50},
        };
      case 'GET /client/invoices':
        body = {
          'data': [
            if (r.url.queryParameters['status'] == 'issued')
              {'id': 'i1', 'reference': 'INV-2026-000001', 'status': 'issued', 'total_minor': 290000, 'paid_minor': 0},
          ],
          'meta': {'limit': 10},
        };
      case 'GET /public/categories':
        body = [
          {
            'id': 'c1',
            'slug': 'cleaning',
            'name': 'Cleaning',
            'description': 'Regular and deep cleans by vetted, trained cleaners.',
            'engagement': 'one_off',
            'is_active': true,
          },
        ];
      case 'GET /public/categories/cleaning':
        body = {
          'category': {'id': 'c1', 'slug': 'cleaning', 'name': 'Cleaning', 'engagement': 'one_off', 'is_active': true},
          'services': [deepClean],
        };
      case 'GET /client/addresses':
        body = [request['address']];
      case 'POST /client/quotes':
        body = {
          'rate_card_entry_id': 'rc1',
          'quantity': 1,
          'unit_price_minor': 60000,
          'placement_fee_minor': 0,
          'service_minor': 60000,
          'rounding_minor': 0,
          'subtotal_minor': 51724,
          'vat_minor': 8276,
          'total_minor': 60000,
          'service_area_supported': true,
        };
      case 'POST /client/requests':
        created.add(r);
        body = request;
        status = 201;
      case 'GET /client/requests/r1':
        body = request;
      case 'GET /client/requests/r1/variations':
        body = [];
      default:
        body = {'code': 'not_found', 'message': 'Not here'};
        status = 404;
    }
    return http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});
  });
}

Future<SessionController> boot(FakeBackend b, {String? token = 'tok'}) async {
  final api = HomendApi(ApiClient(baseUrl: 'https://api.test/api/v1', httpClient: b.client));
  final s = SessionController(api: api, store: MemoryTokenStore()..token = token, userKind: 'client');
  await s.restore();
  return s;
}

void phone(WidgetTester tester) {
  tester.view.physicalSize = const Size(1080, 2400);
  tester.view.devicePixelRatio = 2.625;
  addTearDown(tester.view.reset);
}

Future<void> settle(WidgetTester tester) async {
  for (var i = 0; i < 4; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
    await tester.pumpAndSettle();
  }
}

void main() {
  testWidgets('signed out, the app opens on sign-in and can switch to registration', (tester) async {
    final session = await tester.runAsync(() => boot(FakeBackend(), token: null)) as SessionController;
    await tester.pumpWidget(ClientApp(session: session));
    await tester.pumpAndSettle();
    expect(find.text('Sign in'), findsWidgets);
    await tester.tap(find.text('New here? Create an account'));
    await tester.pumpAndSettle();
    expect(find.text('Create your account'), findsOneWidget);
    expect(find.textContaining('privacy notice'), findsOneWidget);
  });

  testWidgets('home shows what needs the client and what is to pay', (tester) async {
    phone(tester);
    final session = await tester.runAsync(() => boot(FakeBackend())) as SessionController;
    await tester.pumpWidget(ClientApp(session: session));
    await settle(tester);
    expect(find.text('Hello, Wanjiru'), findsOneWidget);
    expect(find.text('NEEDS YOU'), findsOneWidget);
    expect(find.text('Rate the job'), findsOneWidget);
    expect(find.text('KES 2,900'), findsOneWidget);
    expect(find.text('Pay with M-Pesa'), findsOneWidget);
  });

  testWidgets('booking a deep clean prices it and submits once with an idempotency key', (tester) async {
    phone(tester);
    final backend = FakeBackend();
    final session = await tester.runAsync(() => boot(backend)) as SessionController;
    await tester.pumpWidget(ClientApp(session: session));
    await settle(tester);

    await tester.tap(find.text('Book'));
    await settle(tester);
    expect(find.text('Deep clean'), findsOneWidget);
    expect(find.text('KES 600 / hour'), findsOneWidget);
    expect(backend.calls, contains('POST /client/quotes'));
    expect(find.text('KES 600'), findsNWidgets(2)); // service and total: the price includes VAT
    expect(find.text('Includes VAT'), findsOneWidget);

    await tester.ensureVisible(find.byType(CheckboxListTile));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(CheckboxListTile));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Book now'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Book now'));
    await settle(tester);

    expect(backend.created, hasLength(1));
    final sent = jsonDecode(backend.created.single.body) as Map;
    expect(sent['service_id'], 's1');
    expect(sent['address_id'], 'a1');
    expect(sent['accept_terms'], true);
    expect(backend.created.single.headers['Idempotency-Key'], isNotEmpty);
    expect(find.textContaining('Booked. Your reference is REQ-2026-000001'), findsOneWidget);
  });
}
