import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';

import '../models/json.dart';

/// The backend's one error envelope: `{code, message, details, request_id}`.
class ApiException implements Exception {
  const ApiException(this.statusCode, this.code, this.message, {this.details = const {}, this.requestId});

  /// No response at all: no signal, DNS failure, timeout.
  const ApiException.offline()
    : statusCode = 0,
      code = 'offline',
      message = 'No connection. Check your data or Wi-Fi and try again.',
      details = const {},
      requestId = null;

  final int statusCode;
  final String code;
  final String message;
  final Json details;
  final String? requestId;

  bool get isOffline => statusCode == 0;
  bool get isUnauthorized => statusCode == 401;

  /// 422 `rule_violation` / `validation_failed` or 409 `invalid_state_transition`: the message is for the user.
  bool get isUserFacing => statusCode == 400 || statusCode == 409 || statusCode == 422 || statusCode == 429;

  @override
  String toString() => 'ApiException($statusCode $code: $message)';
}

/// Low-level JSON over HTTP for `/api/v1`, with the bearer token and the error envelope.
class ApiClient {
  ApiClient({required this.baseUrl, http.Client? httpClient, this.timeout = const Duration(seconds: 30)})
    : _http = httpClient ?? http.Client();

  /// For example `https://api.homendresidence.co.ke/api/v1`.
  final String baseUrl;
  final Duration timeout;
  final http.Client _http;

  String? token;

  /// Called when the server says the session is gone, so the app can return to sign-in.
  void Function()? onUnauthorized;

  static const _uuid = Uuid();
  static String newIdempotencyKey() => _uuid.v4();

  Uri uri(String path, [Map<String, Object?>? query]) {
    final q = <String, String>{
      for (final e in (query ?? const {}).entries)
        if (e.value != null) e.key: e.value.toString(),
    };
    final base = Uri.parse(baseUrl.endsWith('/') ? baseUrl.substring(0, baseUrl.length - 1) : baseUrl);
    return base.replace(path: '${base.path}$path', queryParameters: q.isEmpty ? null : q);
  }

  Map<String, String> _headers({bool json = false, String? idempotencyKey}) => {
    'Accept': 'application/json',
    if (json) 'Content-Type': 'application/json',
    if (token case final t?) 'Authorization': 'Bearer $t',
    'Idempotency-Key': ?idempotencyKey,
  };

  Future<dynamic> get(String path, {Map<String, Object?>? query}) =>
      _send(() => _http.get(uri(path, query), headers: _headers()));

  Future<dynamic> post(String path, {Object? body, String? idempotencyKey}) => _send(
    () => _http.post(
      uri(path),
      headers: _headers(json: body != null, idempotencyKey: idempotencyKey),
      body: body == null ? null : jsonEncode(body),
    ),
  );

  Future<dynamic> patch(String path, Object body) =>
      _send(() => _http.patch(uri(path), headers: _headers(json: true), body: jsonEncode(body)));

  Future<dynamic> put(String path, Object body) =>
      _send(() => _http.put(uri(path), headers: _headers(json: true), body: jsonEncode(body)));

  Future<dynamic> delete(String path, {Map<String, Object?>? query}) =>
      _send(() => _http.delete(uri(path, query), headers: _headers()));

  /// PUTs file bytes to a presigned vault URL. No bearer token: the URL is the credential.
  Future<void> putBytes(String url, Uint8List bytes, Map<String, String> headers) async {
    final http.Response res;
    try {
      res = await _http.put(Uri.parse(url), headers: headers, body: bytes).timeout(const Duration(minutes: 2));
    } on TimeoutException {
      throw const ApiException.offline();
    } on http.ClientException {
      throw const ApiException.offline();
    }
    if (res.statusCode >= 300) {
      throw ApiException(res.statusCode, 'upload_failed', 'The upload did not go through. Please try again.');
    }
  }

  Future<dynamic> _send(Future<http.Response> Function() call) async {
    final http.Response res;
    try {
      res = await call().timeout(timeout);
    } on TimeoutException {
      throw const ApiException.offline();
    } on http.ClientException {
      throw const ApiException.offline();
    }
    if (res.statusCode >= 200 && res.statusCode < 300) {
      if (res.statusCode == 204 || res.bodyBytes.isEmpty) return null;
      return jsonDecode(utf8.decode(res.bodyBytes));
    }
    final err = _error(res);
    if (err.isUnauthorized) onUnauthorized?.call();
    throw err;
  }

  ApiException _error(http.Response res) {
    try {
      final j = (jsonDecode(utf8.decode(res.bodyBytes)) as Map).cast<String, dynamic>();
      return ApiException(
        res.statusCode,
        j.str('code'),
        j.strOrNull('message') ?? 'Something went wrong (${res.statusCode}).',
        details: j.obj('details') ?? const {},
        requestId: j.strOrNull('request_id'),
      );
    } catch (_) {
      return ApiException(
        res.statusCode,
        'http_${res.statusCode}',
        'Something went wrong (${res.statusCode}). Please try again.',
      );
    }
  }
}
