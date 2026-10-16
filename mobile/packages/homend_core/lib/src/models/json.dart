/// Small helpers for reading the API's snake_case JSON without code generation.
///
/// Enum-like fields stay as strings so that a value the backend adds later does not
/// crash an installed app; [labelOf] turns them into words for the screen.
typedef Json = Map<String, dynamic>;

extension JsonRead on Json {
  String str(String key) => (this[key] ?? '').toString();
  String? strOrNull(String key) {
    final v = this[key];
    if (v == null) return null;
    final s = v.toString();
    return s.isEmpty ? null : s;
  }

  int intOr(String key, [int fallback = 0]) {
    final v = this[key];
    if (v is int) return v;
    if (v is num) return v.toInt();
    if (v is String) return int.tryParse(v) ?? fallback;
    return fallback;
  }

  int? intOrNull(String key) {
    final v = this[key];
    if (v is num) return v.toInt();
    if (v is String) return int.tryParse(v);
    return null;
  }

  double? numOrNull(String key) {
    final v = this[key];
    if (v is num) return v.toDouble();
    if (v is String) return double.tryParse(v);
    return null;
  }

  bool flag(String key) => this[key] == true;

  DateTime? date(String key) {
    final v = this[key];
    if (v is! String || v.isEmpty) return null;
    return DateTime.tryParse(v)?.toLocal();
  }

  Json? obj(String key) {
    final v = this[key];
    return v is Map ? v.cast<String, dynamic>() : null;
  }

  T? model<T>(String key, T Function(Json) fromJson) {
    final o = obj(key);
    return o == null ? null : fromJson(o);
  }

  List<T> list<T>(String key, T Function(Json) fromJson) {
    final v = this[key];
    if (v is! List) return const [];
    return v.whereType<Map>().map((e) => fromJson(e.cast<String, dynamic>())).toList();
  }

  List<String> strings(String key) {
    final v = this[key];
    if (v is! List) return const [];
    return v.map((e) => e.toString()).toList();
  }
}

/// Removes nulls so PATCH and create bodies only carry what the user set.
Json compact(Json m) => {
  for (final e in m.entries)
    if (e.value != null) e.key: e.value,
};

/// `2026-10-16` for `format: date` fields.
String isoDate(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

/// RFC 3339 UTC for `format: date-time` fields.
String isoDateTime(DateTime d) => d.toUtc().toIso8601String();

/// One page of a cursor-paginated list: `{data, meta: {next_cursor, limit}}`.
class ApiPage<T> {
  const ApiPage(this.data, this.nextCursor);

  factory ApiPage.fromJson(Json j, T Function(Json) item) =>
      ApiPage(j.list('data', item), j.obj('meta')?.strOrNull('next_cursor'));

  final List<T> data;
  final String? nextCursor;
}
