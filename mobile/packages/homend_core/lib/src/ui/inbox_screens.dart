import 'package:flutter/material.dart';

import '../models/inbox.dart';
import '../models/json.dart';
import 'format.dart';
import 'theme.dart';
import 'widgets.dart';

/// In-app notifications, newest first. Opening the screen marks everything read.
class NotificationsScreen extends StatelessWidget {
  const NotificationsScreen({super.key, this.onOpen});

  /// Lets the app jump to the request, invoice or job a notification is about.
  final void Function(BuildContext context, AppNotification n)? onOpen;

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Notifications')),
    body: AsyncView<ApiPage<AppNotification>>(
      load: () async {
        final api = context.api;
        final page = await api.notifications(limit: 50);
        if (page.data.any((n) => n.isUnread)) {
          api.markAllNotificationsRead().ignore();
        }
        return page;
      },
      builder: (context, page, _) => page.data.isEmpty
          ? const EmptyState('Nothing new. We will let you know here and by SMS when something changes.')
          : Column(
              children: spaced(
                page.data.map(
                  (n) => Panel(
                    onTap: onOpen == null ? null : () => onOpen!(context, n),
                    color: n.isUnread ? Brand.c50 : null,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (n.title != null) Text(n.title!, style: const TextStyle(fontWeight: FontWeight.w600)),
                        Text(n.body),
                        const SizedBox(height: 6),
                        Text(when(n.createdAt), style: const TextStyle(color: Brand.slate500, fontSize: 13)),
                      ],
                    ),
                  ),
                ),
              ),
            ),
    ),
  );
}

/// Messages with the Homend office. Clients and providers never message each other.
class ConversationsScreen extends StatefulWidget {
  const ConversationsScreen({super.key, this.requestId});

  /// When set, "New message" links the conversation to this booking.
  final String? requestId;

  @override
  State<ConversationsScreen> createState() => _ConversationsScreenState();
}

class _ConversationsScreenState extends State<ConversationsScreen> {
  final _view = GlobalKey<AsyncViewState<ApiPage<Conversation>>>();

  Future<void> _new() async {
    final created = await Navigator.of(context)
        .push<Conversation>(MaterialPageRoute(builder: (_) => NewConversationScreen(requestId: widget.requestId)));
    if (created != null && mounted) {
      _view.currentState?.reload();
      _open(created);
    }
  }

  void _open(Conversation c) =>
      Navigator.of(context)
          .push(MaterialPageRoute(builder: (_) => ConversationScreen(conversation: c)))
          .then((_) => _view.currentState?.reload());

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Messages with Homend')),
    floatingActionButton: FloatingActionButton.extended(
      onPressed: _new,
      icon: const Icon(Icons.edit_outlined),
      label: const Text('New message'),
    ),
    body: AsyncView<ApiPage<Conversation>>(
      key: _view,
      load: () => context.api.conversations(limit: 50),
      builder: (context, page, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Notice(
            'For your safety, all contact goes through the Homend office. We usually reply within working hours.',
          ),
          const SizedBox(height: 16),
          if (page.data.isEmpty) const EmptyState('No messages yet.'),
          ...spaced(
            page.data.map(
              (c) => Panel(
                onTap: () => _open(c),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(c.subject ?? 'Message', style: const TextStyle(fontWeight: FontWeight.w600)),
                          Text(
                            when(c.lastMessageAt ?? c.createdAt),
                            style: const TextStyle(color: Brand.slate500, fontSize: 13),
                          ),
                        ],
                      ),
                    ),
                    if (c.unreadCount > 0) StatusBadge('pending', text: '${c.unreadCount} new'),
                    if (c.closedAt != null) const StatusBadge('closed', text: 'Closed'),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(height: 72),
        ],
      ),
    ),
  );
}

class NewConversationScreen extends StatefulWidget {
  const NewConversationScreen({super.key, this.requestId, this.complaintId, this.subject});

  final String? requestId;
  final String? complaintId;
  final String? subject;

  @override
  State<NewConversationScreen> createState() => _NewConversationScreenState();
}

class _NewConversationScreenState extends State<NewConversationScreen> {
  late final _subject = TextEditingController(text: widget.subject);
  final _body = TextEditingController();

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('New message')),
    body: ListView(
      padding: const EdgeInsets.all(16),
      children: [
        TextField(
          controller: _subject,
          decoration: const InputDecoration(labelText: 'Subject (optional)'),
        ),
        const SizedBox(height: 16),
        TextField(
          controller: _body,
          minLines: 4,
          maxLines: 8,
          maxLength: 2000,
          decoration: const InputDecoration(labelText: 'Your message'),
        ),
        const SizedBox(height: 16),
        BusyButton(
          label: 'Send',
          onPressed: () async {
            if (_body.text.trim().isEmpty) {
              showMessage(context, 'Write your message first.', error: true);
              return;
            }
            final c = await context.api.startConversation(
              message: _body.text.trim(),
              subject: _subject.text.trim().isEmpty ? null : _subject.text.trim(),
              requestId: widget.requestId,
              complaintId: widget.complaintId,
            );
            if (context.mounted) Navigator.of(context).pop(c);
          },
        ),
      ],
    ),
  );
}

class ConversationScreen extends StatefulWidget {
  const ConversationScreen({super.key, required this.conversation});

  final Conversation conversation;

  @override
  State<ConversationScreen> createState() => _ConversationScreenState();
}

class _ConversationScreenState extends State<ConversationScreen> {
  final _view = GlobalKey<AsyncViewState<ApiPage<Message>>>();
  final _body = TextEditingController();

  Future<void> _send() async {
    final text = _body.text.trim();
    if (text.isEmpty) return;
    await context.api.sendMessage(widget.conversation.id, text);
    _body.clear();
    await _view.currentState?.reload();
  }

  @override
  Widget build(BuildContext context) {
    final closed = widget.conversation.closedAt != null;
    return Scaffold(
      appBar: AppBar(title: Text(widget.conversation.subject ?? 'Message')),
      body: Column(
        children: [
          Expanded(
            child: AsyncView<ApiPage<Message>>(
              key: _view,
              load: () => context.api.messages(widget.conversation.id),
              builder: (context, page, _) {
                final msgs = [...page.data]..sort((a, b) => a.createdAt.compareTo(b.createdAt));
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: spaced(
                    msgs.map(
                      (m) => Align(
                        alignment: m.fromManagement ? Alignment.centerLeft : Alignment.centerRight,
                        child: ConstrainedBox(
                          constraints: const BoxConstraints(maxWidth: 320),
                          child: Panel(
                            color: m.fromManagement ? Colors.white : Brand.c50,
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                if (m.fromManagement)
                                  const Text(
                                    'Homend',
                                    style: TextStyle(fontWeight: FontWeight.w600, color: Brand.c800),
                                  ),
                                Text(m.body),
                                const SizedBox(height: 4),
                                Text(when(m.createdAt), style: const TextStyle(color: Brand.slate500, fontSize: 12)),
                              ],
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                );
              },
            ),
          ),
          if (!closed)
            SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
                child: Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _body,
                        minLines: 1,
                        maxLines: 4,
                        decoration: const InputDecoration(hintText: 'Write a message'),
                      ),
                    ),
                    const SizedBox(width: 8),
                    SizedBox(
                      width: 96,
                      child: BusyButton(label: 'Send', onPressed: _send),
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }
}
