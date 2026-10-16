import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:homend_core/homend_core.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

http.Response json(Object body, [int status = 200]) =>
    http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

void main() {
  test('sends the bearer token and the idempotency key, and builds query strings', () async {
    late http.Request seen;
    final c = ApiClient(
      baseUrl: 'https://api.example/api/v1',
      httpClient: MockClient((r) async {
        seen = r;
        return json({
          'id': 'p1',
          'invoice_id': 'i1',
          'status': 'initiated',
          'amount_minor': 290000,
          'method': 'mpesa_stk',
        }, 202);
      }),
    )..token = 'tok';
    final api = HomendApi(c);
    final p = await api.payInvoice('i1', phone: '+254781334473', idempotencyKey: 'key-1');
    expect(seen.url.toString(), 'https://api.example/api/v1/client/invoices/i1/pay');
    expect(seen.headers['Authorization'], 'Bearer tok');
    expect(seen.headers['Idempotency-Key'], 'key-1');
    expect(jsonDecode(seen.body), {'phone': '+254781334473'});
    expect(p.status, 'initiated');
    expect(p.isFinal, isFalse);

    await api.requests(status: 'closed', limit: 10).catchError((_) => const ApiPage<ServiceRequest>([], null));
    expect(seen.url.queryParameters, {'status': 'closed', 'limit': '10'});
  });

  test('turns the error envelope into an ApiException the user can read', () async {
    final c = ApiClient(
      baseUrl: 'https://api.example/api/v1',
      httpClient: MockClient(
        (_) async => json({
          'code': 'rule_violation',
          'message': 'Mandatory items are outstanding',
          'details': {
            'missing': ['National ID'],
          },
          'request_id': 'r1',
        }, 422),
      ),
    );
    try {
      await HomendApi(c).submitApplication('a1');
      fail('should throw');
    } on ApiException catch (e) {
      expect(e.statusCode, 422);
      expect(e.code, 'rule_violation');
      expect(e.message, 'Mandatory items are outstanding');
      expect(e.details['missing'], ['National ID']);
      expect(e.isUserFacing, isTrue);
    }
  });

  test('a 401 tells the app the session is gone', () async {
    var expired = false;
    final c = ApiClient(
      baseUrl: 'https://x/api/v1',
      httpClient: MockClient((_) async => json({'code': 'unauthorized', 'message': 'Sign in'}, 401)),
    )..onUnauthorized = () => expired = true;
    await expectLater(HomendApi(c).me(), throwsA(isA<ApiException>()));
    expect(expired, isTrue);
  });

  test('no connection becomes an offline error', () async {
    final c = ApiClient(
      baseUrl: 'https://x/api/v1',
      httpClient: MockClient((_) async => throw http.ClientException('no route')),
    );
    await expectLater(HomendApi(c).offers(), throwsA(predicate((e) => e is ApiException && e.isOffline)));
  });

  test('a 404 subscription means no plan', () async {
    final c = ApiClient(
      baseUrl: 'https://x/api/v1',
      httpClient: MockClient((_) async => json({'code': 'not_found', 'message': 'No subscription'}, 404)),
    );
    expect(await HomendApi(c).subscription(), isNull);
  });

  test('document upload: ticket, PUT to the vault URL, then complete', () async {
    final calls = <String>[];
    final c = ApiClient(
      baseUrl: 'http://localhost:8080/api/v1',
      httpClient: MockClient((r) async {
        calls.add('${r.method} ${r.url}');
        if (r.url.path.endsWith('/documents/uploads')) {
          final body = jsonDecode(r.body) as Map;
          expect(body['size_bytes'], 3);
          expect(body['mime_type'], 'image/jpeg');
          return json({
            'document': {
              'id': 'd1',
              'document_type': 'national_id',
              'status': 'pending_upload',
              'created_at': '2026-10-16T10:00:00Z',
            },
            'upload_url': 'http://localhost:8080/storage/k1?exp=1&sig=s',
            'upload_headers': {'Content-Type': 'image/jpeg'},
            'expires_at': '2026-10-16T10:15:00Z',
          }, 201);
        }
        if (r.method == 'PUT') {
          expect(r.headers.containsKey('Authorization'), isFalse, reason: 'the presigned URL is the credential');
          expect(r.bodyBytes, [1, 2, 3]);
          return http.Response('', 200);
        }
        return json({
          'id': 'd1',
          'document_type': 'national_id',
          'status': 'uploaded',
          'created_at': '2026-10-16T10:00:00Z',
        });
      }),
    )..token = 'tok';
    final doc = await HomendApi(c).uploadDocument(
      documentType: 'national_id',
      mimeType: 'image/jpeg',
      bytes: Uint8List.fromList([1, 2, 3]),
      filename: 'id.jpg',
    );
    expect(doc.status, 'uploaded');
    expect(calls, [
      'POST http://localhost:8080/api/v1/documents/uploads',
      'PUT http://localhost:8080/storage/k1?exp=1&sig=s',
      'POST http://localhost:8080/api/v1/documents/d1/complete',
    ]);
  });
}
