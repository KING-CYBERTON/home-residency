import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

/// Invoice lines and M-Pesa payment by STK push: the phone shows a prompt, the client enters
/// their PIN, and this screen follows the payment until it succeeds or fails.
class InvoiceScreen extends StatefulWidget {
  const InvoiceScreen({super.key, required this.id});

  final String id;

  @override
  State<InvoiceScreen> createState() => _InvoiceScreenState();
}

class _InvoiceScreenState extends State<InvoiceScreen> {
  final _view = GlobalKey<AsyncViewState<Invoice>>();
  Payment? _payment;
  Timer? _poll;
  int _polls = 0;
  String _idempotencyKey = ApiClient.newIdempotencyKey();

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _pay(Invoice inv) async {
    final phone = await showDialog<String>(
      context: context,
      builder: (_) => _PhoneDialog(initial: displayPhone(context.session.me?.phone), amount: inv.dueMinor),
    );
    if (phone == null || !mounted) return;
    final p = await context.api.payInvoice(inv.id, phone: phone, idempotencyKey: _idempotencyKey);
    setState(() {
      _payment = p;
      _polls = 0;
    });
    _follow();
  }

  /// Checks every 3 seconds for up to 2 minutes; the M-Pesa callback usually lands within 30.
  void _follow() {
    _poll?.cancel();
    _poll = Timer.periodic(const Duration(seconds: 3), (t) async {
      final p = _payment;
      if (p == null || !mounted) return t.cancel();
      _polls++;
      try {
        final now = await context.api.payment(p.id);
        if (!mounted) return;
        setState(() => _payment = now);
        if (now.isFinal) {
          t.cancel();
          _idempotencyKey = ApiClient.newIdempotencyKey();
          if (now.status == 'succeeded') await _view.currentState?.reload();
        }
      } catch (_) {}
      if (_polls >= 40) t.cancel();
    });
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Invoice')),
    body: AsyncView<Invoice>(
      key: _view,
      load: () => context.api.invoice(widget.id),
      builder: (context, inv, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PageTitle(
            'Invoice ${inv.reference}',
            subtitle: [
              if (inv.issuedAt != null) 'Issued ${when(inv.issuedAt!)}',
              if (inv.dueAt != null) 'due ${when(inv.dueAt!)}',
            ].join(' · '),
            trailing: StatusBadge(inv.status),
          ),
          const SizedBox(height: 16),
          Panel(
            child: Column(
              children: [
                for (final l in inv.lines) KeyValue(l.description, kes(l.amountMinor)),
                const Divider(height: 24),
                KeyValue('Total', kes(inv.totalMinor), bold: true),
                if (inv.vatMinor > 0) KeyValue('Includes VAT', kes(inv.vatMinor)),
                if (inv.paidMinor > 0 && inv.paidMinor < inv.totalMinor) ...[
                  KeyValue('Paid', kes(inv.paidMinor)),
                  KeyValue('Left to pay', kes(inv.dueMinor), bold: true),
                ],
              ],
            ),
          ),
          const SizedBox(height: 16),
          if (_payment case final p?) _PaymentStatus(payment: p, onRetry: () => _pay(inv)),
          if (inv.isPayable && (_payment == null || _payment!.isFinal && _payment!.status != 'succeeded')) ...[
            const SizedBox(height: 8),
            BusyButton(label: 'Pay ${kes(inv.dueMinor)} with M-Pesa', onPressed: () => _pay(inv)),
          ],
          if (inv.requestId != null)
            TextButton(onPressed: () => context.go('/bookings/${inv.requestId}'), child: const Text('See the booking')),
        ],
      ),
    ),
  );
}

class _PaymentStatus extends StatelessWidget {
  const _PaymentStatus({required this.payment, required this.onRetry});

  final Payment payment;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => switch (payment.status) {
    'succeeded' => Notice(
      'We received ${kes(payment.amountMinor)}${payment.mpesaReceiptNumber == null ? '' : ' (M-Pesa ${payment.mpesaReceiptNumber})'}. Thank you.',
      title: 'Paid',
    ),
    'failed' || 'cancelled' || 'reversed' => Notice(
      payment.failureReason ?? 'The payment did not go through. You can try again.',
      title: 'Payment not completed',
      warning: true,
    ),
    _ => Notice(
      'Approve the M-Pesa prompt for ${kes(payment.amountMinor)} by entering your PIN. This page updates by itself.',
      title: 'Check your phone',
    ),
  };
}

class _PhoneDialog extends StatefulWidget {
  const _PhoneDialog({required this.initial, required this.amount});

  final String initial;
  final int amount;

  @override
  State<_PhoneDialog> createState() => _PhoneDialogState();
}

class _PhoneDialogState extends State<_PhoneDialog> {
  late final _phone = TextEditingController(text: widget.initial);
  String? _error;

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('Pay ${kes(widget.amount)}'),
    content: TextField(
      controller: _phone,
      keyboardType: TextInputType.phone,
      decoration: InputDecoration(labelText: 'M-Pesa number', errorText: _error),
    ),
    actions: [
      TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
      TextButton(
        onPressed: () {
          final p = normalizeKenyanPhone(_phone.text);
          if (p == null) {
            setState(() => _error = 'Enter a Safaricom number, e.g. 0712 345 678');
            return;
          }
          Navigator.pop(context, p);
        },
        child: const Text('Send prompt'),
      ),
    ],
  );
}
