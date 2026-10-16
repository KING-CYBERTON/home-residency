import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';
import 'package:intl/date_symbol_data_local.dart';

import 'app.dart';
import 'offline_queue.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await initializeDateFormatting('en_KE');

  final api = HomendApi(ApiClient(baseUrl: AppConfig.apiBaseUrl));
  final session = SessionController(api: api, store: SecureTokenStore('homend_provider_session'), userKind: 'provider');
  final queue = OfflineQueue(api);
  await Future.wait([session.restore(), queue.start()]);

  runApp(ProviderApp(session: session, queue: queue));
}
