import 'package:flutter/material.dart';

import '../api/api_client.dart';
import '../api/homend_api.dart';
import '../session/session.dart';
import 'format.dart';
import 'theme.dart';

/// Gives every screen the API and the session: `context.api`, `context.session`.
class HomendScope extends InheritedNotifier<SessionController> {
  const HomendScope({super.key, required SessionController session, required super.child}) : super(notifier: session);

  static SessionController of(BuildContext context, {bool listen = true}) {
    final scope = listen
        ? context.dependOnInheritedWidgetOfExactType<HomendScope>()
        : context.getInheritedWidgetOfExactType<HomendScope>();
    assert(scope != null, 'No HomendScope above this widget');
    return scope!.notifier!;
  }
}

extension HomendContext on BuildContext {
  SessionController get session => HomendScope.of(this, listen: false);
  HomendApi get api => session.api;
}

/// Turns any error into a sentence for the user.
String messageOf(Object error) {
  if (error is ApiException) return error.message;
  if (error is WrongAppException) return error.message;
  return 'Something went wrong. Please try again.';
}

void showMessage(BuildContext context, String text, {bool error = false}) {
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(
      SnackBar(
        content: Text(text),
        backgroundColor: error ? Brand.red700 : Brand.slate900,
        behavior: SnackBarBehavior.floating,
      ),
    );
}

Future<bool> confirm(
  BuildContext context,
  String question, {
  String yes = 'Yes',
  String no = 'Not now',
  bool danger = false,
}) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (c) => AlertDialog(
      content: Text(question),
      actions: [
        TextButton(onPressed: () => Navigator.pop(c, false), child: Text(no)),
        TextButton(
          onPressed: () => Navigator.pop(c, true),
          style: danger ? TextButton.styleFrom(foregroundColor: Brand.red700) : null,
          child: Text(yes),
        ),
      ],
    ),
  );
  return ok ?? false;
}

/// Loads data, shows a spinner, an error with Retry, then the content. Pull down to refresh.
class AsyncView<T> extends StatefulWidget {
  const AsyncView({super.key, required this.load, required this.builder, this.scrollable = true});

  final Future<T> Function() load;
  final Widget Function(BuildContext context, T data, Future<void> Function() reload) builder;

  /// When true the content is wrapped in a scrolling list with pull-to-refresh.
  final bool scrollable;

  @override
  State<AsyncView<T>> createState() => AsyncViewState<T>();
}

class AsyncViewState<T> extends State<AsyncView<T>> {
  T? _data;
  Object? _error;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    reload();
  }

  Future<void> reload() async {
    setState(() {
      _loading = _data == null;
      _error = null;
    });
    try {
      final d = await widget.load();
      if (!mounted) return;
      setState(() {
        _data = d;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e;
        _loading = false;
      });
      if (_data != null) showMessage(context, messageOf(e), error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    final data = _data;
    if (data == null) {
      return ErrorPanel(message: messageOf(_error ?? 'error'), onRetry: reload);
    }
    final content = widget.builder(context, data, reload);
    if (!widget.scrollable) return content;
    return RefreshIndicator(
      onRefresh: reload,
      child: ListView(padding: const EdgeInsets.fromLTRB(16, 16, 16, 32), children: [content]),
    );
  }
}

class ErrorPanel extends StatelessWidget {
  const ErrorPanel({super.key, required this.message, this.onRetry});

  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.cloud_off_outlined, size: 40, color: Brand.slate500),
          const SizedBox(height: 12),
          Text(message, textAlign: TextAlign.center),
          if (onRetry != null) ...[
            const SizedBox(height: 16),
            OutlinedButton(onPressed: onRetry, child: const Text('Try again')),
          ],
        ],
      ),
    ),
  );
}

/// A button that runs an async action, shows a spinner meanwhile and reports errors.
class BusyButton extends StatefulWidget {
  const BusyButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.outlined = false,
    this.danger = false,
  });

  final String label;
  final Future<void> Function()? onPressed;
  final bool outlined;
  final bool danger;

  @override
  State<BusyButton> createState() => _BusyButtonState();
}

class _BusyButtonState extends State<BusyButton> {
  bool _busy = false;

  Future<void> _run() async {
    setState(() => _busy = true);
    try {
      await widget.onPressed!();
    } catch (e) {
      if (mounted) showMessage(context, messageOf(e), error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final onPressed = widget.onPressed == null || _busy ? null : _run;
    final child = _busy
        ? const SizedBox(height: 20, width: 20, child: CircularProgressIndicator(strokeWidth: 2))
        : Text(widget.label);
    if (widget.outlined) {
      return OutlinedButton(
        onPressed: onPressed,
        style: widget.danger ? OutlinedButton.styleFrom(foregroundColor: Brand.red700) : null,
        child: child,
      );
    }
    return FilledButton(
      onPressed: onPressed,
      style: widget.danger ? FilledButton.styleFrom(backgroundColor: Brand.red700) : null,
      child: child,
    );
  }
}

/// The pill next to titles: "Received", "To pay", "Not verified".
class StatusBadge extends StatelessWidget {
  const StatusBadge(this.code, {super.key, this.text});

  final String code;
  final String? text;

  @override
  Widget build(BuildContext context) {
    final (bg, fg) = switch (code) {
      'issued' ||
      'partially_paid' ||
      'awaiting_client_choice' ||
      'awaiting_rating' ||
      'offered' ||
      'pending' ||
      'awaiting_provider_account' ||
      'incomplete' ||
      'held' ||
      'draft' => (const Color(0xFFFEF3C7), Brand.amber800),
      'cancelled' ||
      'failed' ||
      'rejected' ||
      'missed' ||
      'suspended' ||
      'disqualified' ||
      'unfilled' ||
      'expired' ||
      'void' ||
      'declined' => (Brand.red50, Brand.red700),
      'closed' ||
      'paid' ||
      'succeeded' ||
      'verified' ||
      'satisfied' ||
      'client_confirmed' ||
      'active' ||
      'approved' ||
      'passed' ||
      'accepted' ||
      'decided' => (const Color(0xFFDCFCE7), const Color(0xFF166534)),
      _ => (Brand.c50, Brand.c800),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
      decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(999)),
      child: Text(
        text ?? labelOf(code),
        style: TextStyle(color: fg, fontSize: 12, fontWeight: FontWeight.w600),
      ),
    );
  }
}

/// Small grey uppercase heading above a group of cards.
class SectionTitle extends StatelessWidget {
  const SectionTitle(this.text, {super.key, this.color = Brand.slate500});

  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 24, bottom: 8),
    child: Text(
      text.toUpperCase(),
      style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, letterSpacing: 0.6, color: color),
    ),
  );
}

class PageTitle extends StatelessWidget {
  const PageTitle(this.title, {super.key, this.subtitle, this.trailing});

  final String title;
  final String? subtitle;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Expanded(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w600, color: Brand.slate900),
            ),
            if (subtitle != null)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(subtitle!, style: const TextStyle(color: Brand.slate500)),
              ),
          ],
        ),
      ),
      if (trailing != null) Padding(padding: const EdgeInsets.only(left: 8, top: 4), child: trailing),
    ],
  );
}

/// A white rounded card with padding.
class Panel extends StatelessWidget {
  const Panel({
    super.key,
    required this.child,
    this.onTap,
    this.color,
    this.borderColor,
    this.padding = const EdgeInsets.all(16),
  });

  final Widget child;
  final VoidCallback? onTap;
  final Color? color;
  final Color? borderColor;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) => Card(
    color: color,
    shape: borderColor == null
        ? null
        : RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(12),
            side: BorderSide(color: borderColor!),
          ),
    clipBehavior: Clip.antiAlias,
    child: InkWell(
      onTap: onTap,
      child: Padding(padding: padding, child: child),
    ),
  );
}

/// Teal (info) or amber (needs you) message box, like the portal's banners.
class Notice extends StatelessWidget {
  const Notice(this.text, {super.key, this.title, this.warning = false, this.onTap});

  final String text;
  final String? title;
  final bool warning;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) => Panel(
    color: warning ? Brand.amber50 : Brand.c50,
    borderColor: warning ? Brand.amber200 : Brand.c200,
    onTap: onTap,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (title != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 4),
            child: Text(
              title!,
              style: TextStyle(fontWeight: FontWeight.w600, color: warning ? Brand.amber800 : Brand.c900),
            ),
          ),
        Text(text, style: TextStyle(color: warning ? Brand.amber800 : Brand.c800)),
      ],
    ),
  );
}

class KeyValue extends StatelessWidget {
  const KeyValue(this.label, this.value, {super.key, this.bold = false});

  final String label;
  final String value;
  final bool bold;

  @override
  Widget build(BuildContext context) {
    final style = TextStyle(fontWeight: bold ? FontWeight.w700 : FontWeight.w400, fontSize: bold ? 17 : 15);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: [
          Expanded(
            child: Text(label, style: style.copyWith(color: bold ? Brand.slate900 : Brand.slate600)),
          ),
          Text(value, style: style),
        ],
      ),
    );
  }
}

class EmptyState extends StatelessWidget {
  const EmptyState(this.text, {super.key});

  final String text;

  @override
  Widget build(BuildContext context) => Panel(
    child: Text(
      text,
      textAlign: TextAlign.center,
      style: const TextStyle(color: Brand.slate500),
    ),
  );
}

/// A tappable row in a grouped list (Account and More screens).
class MenuGroup extends StatelessWidget {
  const MenuGroup({super.key, required this.items});

  final List<MenuItem> items;

  @override
  Widget build(BuildContext context) => Card(
    clipBehavior: Clip.antiAlias,
    child: Column(
      children: [
        for (var i = 0; i < items.length; i++) ...[
          if (i > 0) const Divider(),
          ListTile(
            title: Text(items[i].label),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (items[i].value != null) Text(items[i].value!, style: const TextStyle(color: Brand.slate500)),
                const Icon(Icons.chevron_right, color: Brand.slate300),
              ],
            ),
            onTap: items[i].onTap,
          ),
        ],
      ],
    ),
  );
}

class MenuItem {
  const MenuItem(this.label, this.onTap, {this.value});

  final String label;
  final VoidCallback onTap;
  final String? value;
}

/// The Homend mark used in app bars.
class BrandTitle extends StatelessWidget {
  const BrandTitle({super.key, this.suffix});

  final String? suffix;

  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      Container(
        width: 32,
        height: 32,
        decoration: BoxDecoration(color: Brand.c700, borderRadius: BorderRadius.circular(8)),
        child: const Icon(Icons.home_rounded, color: Colors.white, size: 22),
      ),
      const SizedBox(width: 10),
      const Text(
        'Homend',
        style: TextStyle(fontWeight: FontWeight.w700, color: Brand.c800),
      ),
      if (suffix != null)
        Text(
          ' $suffix',
          style: const TextStyle(fontWeight: FontWeight.w400, color: Brand.slate500),
        ),
    ],
  );
}

/// Vertical spacing between stacked cards.
List<Widget> spaced(Iterable<Widget> children, [double gap = 12]) {
  final out = <Widget>[];
  for (final c in children) {
    if (out.isNotEmpty) out.add(SizedBox(height: gap));
    out.add(c);
  }
  return out;
}
