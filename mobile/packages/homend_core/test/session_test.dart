import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:homend_core/homend_core.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

Map<String, dynamic> session(String kind) => {
  'token': 'tok-$kind',
  'expires_at': '2026-11-16T00:00:00Z',
  'user': {'id': 'u1', 'kind': kind, 'status': 'active', 'full_name': 'Wanjiru Kamau', 'phone': '+254781334473'},
};

void main() {
  test('signing in stores the token; signing out clears it', () async {
    final store = MemoryTokenStore();
    final api = HomendApi(
      ApiClient(baseUrl: 'https://x/api/v1', httpClient: MockClient((_) async => http.Response('', 204))),
    );
    final s = SessionController(api: api, store: store, userKind: 'client');
    await s.signedIn(Session.fromJson(session('client')));
    expect(s.isSignedIn, isTrue);
    expect(store.token, 'tok-client');
    expect(api.client.token, 'tok-client');
    await s.signOut();
    expect(s.state, SessionState.signedOut);
    expect(store.token, isNull);
  });

  test('a provider number in the client app is signed straight back out', () async {
    final calls = <String>[];
    final api = HomendApi(
      ApiClient(
        baseUrl: 'https://x/api/v1',
        httpClient: MockClient((r) async {
          calls.add('${r.method} ${r.url.path}');
          return http.Response('', 204);
        }),
      ),
    );
    final s = SessionController(api: api, store: MemoryTokenStore(), userKind: 'client');
    await expectLater(s.signedIn(Session.fromJson(session('provider'))), throwsA(isA<WrongAppException>()));
    expect(s.isSignedIn, isFalse);
    expect(calls, ['POST /api/v1/auth/logout']);
  });

  test('restore keeps the session when the phone is offline', () async {
    final store = MemoryTokenStore()..token = 'saved';
    final api = HomendApi(
      ApiClient(
        baseUrl: 'https://x/api/v1',
        httpClient: MockClient((_) async => throw http.ClientException('offline')),
      ),
    );
    final s = SessionController(api: api, store: store, userKind: 'provider');
    await s.restore();
    expect(s.isSignedIn, isTrue);
    expect(store.token, 'saved');
  });

  test('restore drops a token the server rejects', () async {
    final store = MemoryTokenStore()..token = 'old';
    final api = HomendApi(
      ApiClient(
        baseUrl: 'https://x/api/v1',
        httpClient: MockClient(
          (_) async => http.Response(jsonEncode({'code': 'unauthorized', 'message': 'Sign in'}), 401),
        ),
      ),
    );
    final s = SessionController(api: api, store: store, userKind: 'provider');
    await s.restore();
    expect(s.state, SessionState.signedOut);
    expect(store.token, isNull);
  });
}
