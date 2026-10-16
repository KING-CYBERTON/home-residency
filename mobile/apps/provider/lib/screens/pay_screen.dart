import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';

class _Pay {
  _Pay(this.summary, this.earnings, this.payouts, this.profile);
  final EarningsSummary summary;
  final List<Earning> earnings;
  final List<Payout> payouts;
  final ProviderProfile? profile;
}

/// What the provider has earned, what is on hold, and M-Pesa payouts.
class PayScreen extends StatefulWidget {
  const PayScreen({super.key});

  @override
  State<PayScreen> createState() => _PayScreenState();
}

class _PayScreenState extends State<PayScreen> {
  bool _showPayouts = false;

  Future<_Pay> _load() async {
    final api = context.api;
    final r = await Future.wait([
      api.earningsSummary(),
      api.earnings(limit: 50),
      api.payouts(limit: 25),
      api.providerProfile().then<ProviderProfile?>((p) => p, onError: (_) => null),
    ]);
    return _Pay(
      r[0] as EarningsSummary,
      (r[1] as ApiPage<Earning>).data,
      (r[2] as ApiPage<Payout>).data,
      r[3] as ProviderProfile?,
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: providerBar(context, title: 'Pay'),
    body: AsyncView<_Pay>(
      load: _load,
      builder: (context, p, _) {
        final s = p.summary;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Panel(
              color: Brand.c700,
              borderColor: Brand.c700,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'To be paid${s.nextPayoutDate == null ? '' : ' on ${day(s.nextPayoutDate!)}'}',
                    style: const TextStyle(color: Brand.c100),
                  ),
                  Text(
                    kes(s.payableMinor),
                    style: const TextStyle(color: Colors.white, fontSize: 28, fontWeight: FontWeight.w700),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    p.profile?.payoutMsisdn == null
                        ? 'Add your M-Pesa number to get paid.'
                        : 'Paid weekly to M-Pesa ${displayPhone(p.profile!.payoutMsisdn)}',
                    style: const TextStyle(color: Brand.c100),
                  ),
                ],
              ),
            ),
            if (p.profile?.payoutMsisdn == null) ...[
              const SizedBox(height: 8),
              TextButton(onPressed: () => context.go('/more/profile'), child: const Text('Add my M-Pesa number')),
            ],
            const SizedBox(height: 12),
            Panel(
              child: Column(
                children: [
                  KeyValue('Waiting for client confirmation', kes(s.pendingMinor)),
                  KeyValue('On hold', kes(s.heldMinor)),
                  KeyValue('Paid this month', kes(s.paidThisMonthMinor)),
                ],
              ),
            ),
            const SizedBox(height: 16),
            SegmentedButton<bool>(
              segments: const [
                ButtonSegment(value: false, label: Text('Earnings')),
                ButtonSegment(value: true, label: Text('Payouts')),
              ],
              selected: {_showPayouts},
              onSelectionChanged: (v) => setState(() => _showPayouts = v.first),
            ),
            const SizedBox(height: 12),
            if (!_showPayouts) ...[
              if (p.earnings.isEmpty) const EmptyState('No earnings yet. You earn when a client confirms the work.'),
              ...spaced(p.earnings.map(_earning)),
            ] else ...[
              if (p.payouts.isEmpty) const EmptyState('No payouts yet.'),
              ...spaced(p.payouts.map(_payout)),
            ],
          ],
        );
      },
    ),
  );

  Widget _earning(Earning e) => Panel(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: Text(e.requestReference ?? 'Job', style: const TextStyle(fontWeight: FontWeight.w600)),
            ),
            Text(kes(e.netMinor), style: const TextStyle(fontWeight: FontWeight.w700)),
          ],
        ),
        Row(
          children: [
            Expanded(
              child: Text(day(e.earnedAt), style: const TextStyle(color: Brand.slate500)),
            ),
            StatusBadge(e.status),
          ],
        ),
        if (e.commissionMinor > 0 || e.withholdingMinor > 0)
          Text(
            '${kes(e.grossMinor)} less ${kes(e.commissionMinor)} fee${e.withholdingMinor > 0 ? ' and ${kes(e.withholdingMinor)} tax' : ''}',
            style: const TextStyle(color: Brand.slate500, fontSize: 13),
          ),
        if (e.holdReason != null) Text('On hold: ${e.holdReason}', style: const TextStyle(color: Brand.amber800)),
      ],
    ),
  );

  Widget _payout(Payout p) => Panel(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: Text(kes(p.amountMinor), style: const TextStyle(fontWeight: FontWeight.w700)),
            ),
            StatusBadge(p.status),
          ],
        ),
        if (p.paidAt != null) Text('Sent ${when(p.paidAt!)}', style: const TextStyle(color: Brand.slate500)),
        if (p.mpesaReceiptNumber != null)
          Text('M-Pesa ${p.mpesaReceiptNumber}', style: const TextStyle(color: Brand.slate500)),
        if (p.failureReason != null) Text(p.failureReason!, style: const TextStyle(color: Brand.red700)),
      ],
    ),
  );
}
