import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../api/api_client.dart';
import '../api/homend_api.dart';
import '../models/identity.dart';

/// Where the session token lives between launches.
abstract class TokenStore {
  Future<String?> read();
  Future<void> write(String token);
  Future<void> clear();
}

/// Keychain on iOS, encrypted shared preferences on Android.
class SecureTokenStore implements TokenStore {
  SecureTokenStore(this.key);

  final String key;
  static const _storage = FlutterSecureStorage();

  @override
  Future<String?> read() => _storage.read(key: key);
  @override
  Future<void> write(String token) => _storage.write(key: key, value: token);
  @override
  Future<void> clear() => _storage.delete(key: key);
}

class MemoryTokenStore implements TokenStore {
  String? token;
  @override
  Future<String?> read() async => token;
  @override
  Future<void> write(String t) async => token = t;
  @override
  Future<void> clear() async => token = null;
}

enum SessionState { loading, signedOut, signedIn }

/// Thrown when someone signs in to the wrong app, e.g. a provider number in the client app.
class WrongAppException implements Exception {
  const WrongAppException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Holds the signed-in user for one app. [userKind] is `client` or `provider`; the other kind is
/// signed straight back out with a pointer to the right app, as the web portals do.
class SessionController extends ChangeNotifier {
  SessionController({required this.api, required this.store, required this.userKind}) {
    api.client.onUnauthorized = _expired;
  }

  final HomendApi api;
  final TokenStore store;
  final String userKind;

  SessionState _state = SessionState.loading;
  Me? _me;

  SessionState get state => _state;
  Me? get me => _me;
  bool get isSignedIn => _state == SessionState.signedIn;

  /// Called on launch: reuse a saved token if the server still accepts it.
  Future<void> restore() async {
    final token = await store.read();
    if (token == null) {
      _set(SessionState.signedOut, null);
      return;
    }
    api.client.token = token;
    try {
      final me = await api.me();
      if (me.kind != userKind) throw WrongAppException(_wrongApp(me.kind));
      _set(SessionState.signedIn, me);
    } on ApiException catch (e) {
      if (e.isOffline) {
        // Keep the token: the provider app must open without signal. Screens show their own offline state.
        _set(SessionState.signedIn, _me);
      } else {
        await _clear();
      }
    } on WrongAppException {
      await _clear();
    }
  }

  /// Completes an OTP sign-in or registration that [HomendApi.verifyOtp] returned.
  Future<void> signedIn(Session session) async {
    if (session.user.kind != userKind) {
      api.client.token = session.token;
      try {
        await api.logout();
      } catch (_) {}
      api.client.token = null;
      throw WrongAppException(_wrongApp(session.user.kind));
    }
    api.client.token = session.token;
    await store.write(session.token);
    _set(SessionState.signedIn, session.user);
  }

  Future<Me> refreshMe() async {
    final me = await api.me();
    _me = me;
    notifyListeners();
    return me;
  }

  void replaceMe(Me me) {
    _me = me;
    notifyListeners();
  }

  Future<void> signOut() async {
    try {
      await api.logout();
    } catch (_) {
      // Signing out works offline too; the server session expires on its own.
    }
    await _clear();
  }

  void _expired() {
    if (_state == SessionState.signedIn) _clear();
  }

  Future<void> _clear() async {
    api.client.token = null;
    await store.clear();
    _set(SessionState.signedOut, null);
  }

  void _set(SessionState s, Me? me) {
    _state = s;
    _me = me;
    notifyListeners();
  }

  String _wrongApp(String kind) => switch (kind) {
    'provider' => 'This number is registered as a service provider. Please use the Homend Provider app.',
    'client' => 'This number is registered as a household. Please use the Homend app for households.',
    _ => 'Staff accounts sign in to the management console, not this app.',
  };
}
