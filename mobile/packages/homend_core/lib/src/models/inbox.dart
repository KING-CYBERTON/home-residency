import 'json.dart';

class AppNotification {
  const AppNotification({
    required this.id,
    required this.eventKey,
    required this.body,
    required this.createdAt,
    this.title,
    this.data = const {},
    this.readAt,
  });

  factory AppNotification.fromJson(Json j) => AppNotification(
    id: j.str('id'),
    eventKey: j.str('event_key'),
    body: j.str('body'),
    createdAt: j.date('created_at') ?? DateTime.now(),
    title: j.strOrNull('title'),
    data: j.obj('data') ?? const {},
    readAt: j.date('read_at'),
  );

  final String id;
  final String eventKey;
  final String body;
  final DateTime createdAt;
  final String? title;
  final Json data;
  final DateTime? readAt;

  bool get isUnread => readAt == null;
}

class Conversation {
  const Conversation({
    required this.id,
    required this.createdAt,
    this.subject,
    this.requestId,
    this.complaintId,
    this.lastMessageAt,
    this.unreadCount = 0,
    this.closedAt,
  });

  factory Conversation.fromJson(Json j) => Conversation(
    id: j.str('id'),
    createdAt: j.date('created_at') ?? DateTime.now(),
    subject: j.strOrNull('subject'),
    requestId: j.strOrNull('request_id'),
    complaintId: j.strOrNull('complaint_id'),
    lastMessageAt: j.date('last_message_at'),
    unreadCount: j.intOr('unread_count'),
    closedAt: j.date('closed_at'),
  );

  final String id;
  final DateTime createdAt;
  final String? subject;
  final String? requestId;
  final String? complaintId;
  final DateTime? lastMessageAt;
  final int unreadCount;
  final DateTime? closedAt;
}

class Message {
  const Message({required this.id, required this.body, required this.createdAt, this.fromManagement = false});

  factory Message.fromJson(Json j) => Message(
    id: j.str('id'),
    body: j.str('body'),
    createdAt: j.date('created_at') ?? DateTime.now(),
    fromManagement: j.flag('from_management'),
  );

  final String id;
  final String body;
  final DateTime createdAt;
  final bool fromManagement;
}

/// Vault document metadata. The file itself is only reachable through a short-lived, logged link.
class Document {
  const Document({
    required this.id,
    required this.documentType,
    required this.status,
    required this.createdAt,
    this.originalFilename,
    this.expiresOn,
    this.rejectionReason,
  });

  factory Document.fromJson(Json j) => Document(
    id: j.str('id'),
    documentType: j.str('document_type'),
    status: j.str('status'),
    createdAt: j.date('created_at') ?? DateTime.now(),
    originalFilename: j.strOrNull('original_filename'),
    expiresOn: j.date('expires_on'),
    rejectionReason: j.strOrNull('rejection_reason'),
  );

  final String id;
  final String documentType;

  /// `pending_upload`, `uploaded`, `verified`, `rejected`, `expired` or `purged`.
  final String status;
  final DateTime createdAt;
  final String? originalFilename;
  final DateTime? expiresOn;
  final String? rejectionReason;
}

class UploadTicket {
  const UploadTicket({required this.document, required this.uploadUrl, this.uploadHeaders = const {}});

  factory UploadTicket.fromJson(Json j) => UploadTicket(
    document: Document.fromJson(j.obj('document') ?? const {}),
    uploadUrl: j.str('upload_url'),
    uploadHeaders: (j.obj('upload_headers') ?? const {}).map((k, v) => MapEntry(k, v.toString())),
  );

  final Document document;
  final String uploadUrl;
  final Map<String, String> uploadHeaders;
}
