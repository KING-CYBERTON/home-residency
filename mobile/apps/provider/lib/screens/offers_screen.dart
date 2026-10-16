import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';

/// Job offers from the office. Only one live offer per request, so answer quickly.
class OffersScreen extends StatefulWidget {
  const OffersScreen({super.key});

  @override
  State<OffersScreen> createState() => _OffersScreenState();
}

class _OffersScreenState extends State<OffersScreen> {
  final _view = GlobalKey<AsyncViewState<List<Offer>>>();

  Future<void> _accept(Offer o) async {
    final job = await context.api.acceptOffer(o.id, idempotencyKey: ApiClient.newIdempotencyKey());
    if (!mounted) return;
    showMessage(context, 'Job accepted. The client has been told.');
    context.go('/jobs/${job.assignmentId}');
  }

  Future<void> _decline(Offer o) async {
    final reason = await showDialog<String>(context: context, builder: (_) => const _DeclineDialog());
    if (reason == null || !mounted) return;
    await context.api.declineOffer(o.id, reason: reason.isEmpty ? null : reason);
    if (!mounted) return;
    showMessage(context, 'Offer declined.');
    await _view.currentState?.reload();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: providerBar(context, title: 'Job offers'),
    body: AsyncView<List<Offer>>(
      key: _view,
      load: () async => (await context.api.offers()).where((o) => o.status == 'offered').toList(),
      builder: (context, offers, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (offers.isEmpty)
            const EmptyState('No offers right now. New offers come here and by SMS.')
          else
            ...spaced(
              offers.map((o) => _OfferCard(offer: o, onAccept: () => _accept(o), onDecline: () => _decline(o))),
            ),
        ],
      ),
    ),
  );
}

class _OfferCard extends StatelessWidget {
  const _OfferCard({required this.offer, required this.onAccept, required this.onDecline});

  final Offer offer;
  final Future<void> Function() onAccept;
  final Future<void> Function() onDecline;

  @override
  Widget build(BuildContext context) {
    final o = offer;
    final left = o.offerExpiresAt.difference(DateTime.now());
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(o.service.name, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
              ),
              Text(kes(o.providerPayMinor), style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            [o.estate, o.serviceArea?.name].whereType<String>().join(', '),
            style: const TextStyle(color: Brand.slate600),
          ),
          const SizedBox(height: 8),
          Text('${when(o.requestedStart)}${o.requestedEnd == null ? '' : ' to ${clock(o.requestedEnd!)}'}'),
          if (o.recurrenceRule != null)
            Text('Repeats: ${o.recurrenceRule}', style: const TextStyle(color: Brand.slate600)),
          if (o.service.scopeItems.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                o.service.scopeItems.join(' · '),
                style: const TextStyle(color: Brand.slate600, fontSize: 13),
              ),
            ),
          if (o.clientNotes != null) Padding(padding: const EdgeInsets.only(top: 8), child: Text('“${o.clientNotes}”')),
          const SizedBox(height: 8),
          Text(
            left.isNegative ? 'This offer has expired.' : 'Answer within ${_left(left)}.',
            style: const TextStyle(color: Brand.amber800, fontSize: 13),
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(
                child: BusyButton(label: 'Decline', outlined: true, onPressed: onDecline),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: BusyButton(label: 'Accept', onPressed: left.isNegative ? null : onAccept),
              ),
            ],
          ),
        ],
      ),
    );
  }

  String _left(Duration d) =>
      d.inHours >= 1 ? '${d.inHours} h ${d.inMinutes % 60} min' : '${d.inMinutes < 1 ? 1 : d.inMinutes} min';
}

class _DeclineDialog extends StatefulWidget {
  const _DeclineDialog();

  @override
  State<_DeclineDialog> createState() => _DeclineDialogState();
}

class _DeclineDialogState extends State<_DeclineDialog> {
  String _reason = '';
  static const _reasons = ['Too far', 'Not available then', 'Not my type of work', 'Other'];

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Decline this job?'),
    content: RadioGroup<String>(
      groupValue: _reason,
      onChanged: (v) => setState(() => _reason = v ?? ''),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final r in _reasons) RadioListTile<String>(value: r, title: Text(r), contentPadding: EdgeInsets.zero),
        ],
      ),
    ),
    actions: [
      TextButton(onPressed: () => Navigator.pop(context), child: const Text('Keep it')),
      TextButton(onPressed: () => Navigator.pop(context, _reason), child: const Text('Decline')),
    ],
  );
}
