import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';
import 'package:intl/date_symbol_data_local.dart';

import 'app.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await initializeDateFormatting('en_KE');

  final api = HomendApi(ApiClient(baseUrl: AppConfig.apiBaseUrl));
  final session = SessionController(api: api, store: SecureTokenStore('homend_client_session'), userKind: 'client');
  await session.restore();

  runApp(ClientApp(session: session));
}
