/// Build-time settings, passed with `--dart-define`.
///
/// `flutter run --dart-define=API_BASE_URL=https://api.example.co.ke/api/v1`
abstract final class AppConfig {
  /// Defaults to a backend on the developer's machine as seen from the Android emulator.
  static const apiBaseUrl = String.fromEnvironment('API_BASE_URL', defaultValue: 'http://10.0.2.2:8080/api/v1');
}
