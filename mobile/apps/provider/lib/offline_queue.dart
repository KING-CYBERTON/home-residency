import 'dart:async';
import 'dart:convert';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/widgets.dart';
import 'package:homend_core/homend_core.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:uuid/uuid.dart';

/// One check-in, check-out or note recorded on the phone (`SyncEvent`).
class QueuedEvent {
  QueuedEvent({
    required this.clientEventId,
    required this.visitId,
    required this.eventType,
    required this.payload,
    required this.deviceRecordedAt,
    this.rejectedMessage,
  });

  factory QueuedEvent.fromJson(Json j) => QueuedEvent(
    clientEventId: j.str('client_event_id'),
    visitId: j.str('visit_id'),
    eventType: j.str('event_type'),
    payload: j.obj('payload') ?? const {},
    deviceRecordedAt: DateTime.parse(j.str('device_recorded_at')),
    rejectedMessage: j.strOrNull('rejected_message'),
  );

  final String clientEventId;
  final String visitId;

  /// `check_in`, `check_out` or `note`.
  final String eventType;
  final Json payload;
  final DateTime deviceRecordedAt;

  /// Set when the server refused the event (wrong door code, visit cancelled…).
  String? rejectedMessage;

  bool get isWaiting => rejectedMessage == null;

  Json toSyncEvent() => {
    'client_event_id': clientEventId,
    'visit_id': visitId,
    'event_type': eventType,
    'payload': payload,
    'device_recorded_at': deviceRecordedAt.toUtc().toIso8601String(),
  };

  Json toJson() => {...toSyncEvent(), 'rejected_message': rejectedMessage};
}

/// Check-in and completion that work without signal, like the provider portal's service worker.
///
/// Every event is saved on the phone first with the phone's own time and a `client_event_id`,
/// then sent to `POST /provider/sync`. The server applies each event once and keeps the phone's
/// time, so a check-in made in a basement counts at the moment it happened. The queue is sent on
/// launch, when the phone comes back online, when the app returns to the foreground, and right
/// after each new event.
class OfflineQueue extends ChangeNotifier with WidgetsBindingObserver {
  OfflineQueue(this._api, {SharedPreferencesAsync? prefs, Stream<List<ConnectivityResult>>? connectivity})
    : _prefs = prefs ?? SharedPreferencesAsync(),
      _connectivityOverride = connectivity;

  static const _key = 'homend_provider_visit_events';
  static const _uuid = Uuid();

  final HomendApi _api;
  final SharedPreferencesAsync _prefs;
  final Stream<List<ConnectivityResult>>? _connectivityOverride;
  StreamSubscription<List<ConnectivityResult>>? _sub;

  List<QueuedEvent> _events = [];
  bool _sending = false;

  List<QueuedEvent> get events => List.unmodifiable(_events);
  int get waitingCount => _events.where((e) => e.isWaiting).length;
  List<QueuedEvent> forVisit(String visitId) => _events.where((e) => e.visitId == visitId).toList();

  Future<void> start() async {
    await _load();
    WidgetsBinding.instance.addObserver(this);
    _sub = (_connectivityOverride ?? Connectivity().onConnectivityChanged).listen((r) {
      if (r.any((c) => c != ConnectivityResult.none)) flush();
    });
    unawaited(flush());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) flush();
  }

  @override
  void dispose() {
    _sub?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  Future<void> _load() async {
    final raw = await _prefs.getString(_key);
    if (raw == null) return;
    try {
      _events = (jsonDecode(raw) as List).map((e) => QueuedEvent.fromJson((e as Map).cast<String, dynamic>())).toList();
    } catch (_) {
      _events = [];
    }
    notifyListeners();
  }

  Future<void> _save() => _prefs.setString(_key, jsonEncode(_events.map((e) => e.toJson()).toList()));

  /// Saves an event and tries to send it straight away.
  Future<QueuedEvent> add(String visitId, String eventType, Json payload, {DateTime? at}) async {
    final ev = QueuedEvent(
      clientEventId: _uuid.v4(),
      visitId: visitId,
      eventType: eventType,
      payload: compact(payload),
      deviceRecordedAt: (at ?? DateTime.now()).toUtc(),
    );
    _events.add(ev);
    await _save();
    notifyListeners();
    await flush();
    return ev;
  }

  /// Sends everything waiting. Safe to call often: replays come back as `duplicate`.
  Future<void> flush() async {
    if (_sending) return;
    final waiting = _events.where((e) => e.isWaiting).take(200).toList();
    if (waiting.isEmpty) return;
    _sending = true;
    try {
      final results = await _api.sync(waiting.map((e) => e.toSyncEvent()).toList());
      final byId = {for (final r in results) r.clientEventId: r};
      for (final e in waiting) {
        final r = byId[e.clientEventId];
        if (r == null) continue;
        if (r.status == 'rejected') {
          e.rejectedMessage = r.message ?? 'The office could not accept this.';
        } else {
          _events.remove(e);
        }
      }
      await _save();
      notifyListeners();
    } on ApiException catch (_) {
      // Offline or the session expired: keep everything and try again later.
    } finally {
      _sending = false;
    }
  }

  Future<void> dismiss(String clientEventId) async {
    _events.removeWhere((e) => e.clientEventId == clientEventId);
    await _save();
    notifyListeners();
  }

  /// Signing out removes everything from the phone, including unsent events.
  Future<void> clear() async {
    _events = [];
    await _prefs.remove(_key);
    notifyListeners();
  }
}

class QueueScope extends InheritedNotifier<OfflineQueue> {
  const QueueScope({super.key, required OfflineQueue queue, required super.child}) : super(notifier: queue);

  static OfflineQueue of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<QueueScope>()!.notifier!;
}
