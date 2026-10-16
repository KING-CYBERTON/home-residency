import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../widgets.dart';

class _Detail {
  _Detail(this.r, this.shortlist, this.variations, this.invoice);
  final ServiceRequest r;
  final List<ShortlistEntry> shortlist;
  final List<Variation> variations;
  final Invoice? invoice;
}

/// One booking: choose a provider, door codes, confirm work, approve changes, rate, cancel.
class RequestScreen extends StatefulWidget {
  const RequestScreen({super.key, required this.id, this.justBooked = false});

  final String id;
  final bool justBooked;

  @override
  State<RequestScreen> createState() => _RequestScreenState();
}

class _RequestScreenState extends State<RequestScreen> {
  final _view = GlobalKey<AsyncViewState<_Detail>>();

  Future<_Detail> _load() async {
    final api = context.api;
    final r = await api.request(widget.id);
    final results = await Future.wait([
      r.status == 'awaiting_client_choice' ? api.shortlist(r.id) : Future.value(<ShortlistEntry>[]),
      api.requestVariations(r.id).catchError((_) => <Variation>[]),
      r.invoiceId == null
          ? Future.value(null)
          : api.invoice(r.invoiceId!).then<Invoice?>((i) => i, onError: (_) => null),
    ]);
    return _Detail(r, results[0] as List<ShortlistEntry>, results[1] as List<Variation>, results[2] as Invoice?);
  }

  Future<void> _reload() async => _view.currentState?.reload();

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Booking')),
    body: AsyncView<_Detail>(key: _view, load: _load, builder: (context, d, _) => _body(context, d)),
  );

  Widget _body(BuildContext context, _Detail d) {
    final r = d.r;
    final a = r.address;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (widget.justBooked) ...[
          Notice('Booked. Your reference is ${r.reference}. We are finding the right provider for you.'),
          const SizedBox(height: 16),
        ],
        PageTitle(
          r.service.name,
          subtitle: '${r.reference} · booked ${when(r.submittedAt)}',
          trailing: StatusBadge(r.status),
        ),
        const SizedBox(height: 16),
        Panel(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(child: _kv('Arrival', when(r.requestedStart))),
                  Expanded(child: _kv('Total', kes(r.totalMinor))),
                ],
              ),
              if (a != null) ...[
                const SizedBox(height: 12),
                _kv('Address', [a.label, a.estate, a.houseNumber].whereType<String>().join(', ')),
              ],
              if (r.recurrenceRule != null) ...[const SizedBox(height: 12), _kv('Repeats', r.recurrenceRule!)],
              if (r.clientNotes != null) ...[const SizedBox(height: 12), _kv('Your notes', r.clientNotes!)],
              if (r.cancelReason != null) ...[const SizedBox(height: 12), _kv('Cancelled', r.cancelReason!)],
            ],
          ),
        ),
        if (d.shortlist.isNotEmpty) ...[
          const SectionTitle('Choose your provider'),
          const Text(
            'All of them are vetted and trained. Pick the one you prefer.',
            style: TextStyle(color: Brand.slate600),
          ),
          const SizedBox(height: 8),
          ...spaced(
            d.shortlist.map(
              (e) => Panel(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    ProviderCard(e.provider),
                    const SizedBox(height: 12),
                    BusyButton(
                      label: 'Choose ${e.provider.displayName}',
                      onPressed: () async {
                        await context.api.chooseFromShortlist(r.id, e.id);
                        if (!context.mounted) return;
                        showMessage(context, 'Thank you. We are confirming with ${e.provider.displayName}.');
                        await _reload();
                      },
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
        if (r.provider != null) ...[
          const SectionTitle('Your provider'),
          Panel(child: ProviderCard(r.provider!)),
          const SizedBox(height: 8),
          Row(
            children: [
              const Expanded(
                child: Text('For your safety, contact goes through Homend.', style: TextStyle(color: Brand.slate600)),
              ),
              TextButton(
                onPressed: () => context.go('/account/messages?request=${r.id}'),
                child: const Text('Message us'),
              ),
            ],
          ),
        ],
        if (r.visits.isNotEmpty) ...[
          const SectionTitle('Visits'),
          ...spaced(r.visits.map((v) => _VisitCard(visit: v, onChanged: _reload))),
        ],
        if (d.variations.isNotEmpty) ...[
          const SectionTitle('Changes to the job'),
          ...spaced(d.variations.map((v) => _VariationCard(v: v, onChanged: _reload))),
        ],
        if (r.status == 'awaiting_rating') ...[
          const SectionTitle('How did it go?'),
          _RatingForm(request: r, onDone: _reload),
        ],
        if (d.invoice case final inv?) ...[
          const SizedBox(height: 16),
          Panel(
            onTap: () => context.go('/pay/${inv.id}'),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Invoice ${inv.reference}', style: const TextStyle(fontWeight: FontWeight.w600)),
                      Text(kes(inv.totalMinor), style: const TextStyle(color: Brand.slate600)),
                    ],
                  ),
                ),
                StatusBadge(inv.status),
              ],
            ),
          ),
        ],
        const SizedBox(height: 24),
        const Divider(),
        TextButton(
          style: TextButton.styleFrom(foregroundColor: Brand.red700, alignment: Alignment.centerLeft),
          onPressed: () => context.go('/account/complaints/new?request=${r.id}'),
          child: const Text('Report a problem with this booking'),
        ),
        if (r.canCancel)
          BusyButton(
            label: 'Cancel this booking',
            outlined: true,
            onPressed: () async {
              final reason = await showDialog<String>(context: context, builder: (_) => const _CancelDialog());
              if (reason == null || !context.mounted) return;
              await context.api.cancelRequest(r.id, reason);
              if (!context.mounted) return;
              showMessage(context, 'Booking cancelled.');
              await _reload();
            },
          ),
      ],
    );
  }

  Widget _kv(String k, String v) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(k, style: const TextStyle(color: Brand.slate500, fontSize: 13)),
      Text(v, style: const TextStyle(fontWeight: FontWeight.w500)),
    ],
  );
}

class _VisitCard extends StatefulWidget {
  const _VisitCard({required this.visit, required this.onChanged});

  final Visit visit;
  final Future<void> Function() onChanged;

  @override
  State<_VisitCard> createState() => _VisitCardState();
}

class _VisitCardState extends State<_VisitCard> {
  CheckinCode? _code;

  @override
  Widget build(BuildContext context) {
    final v = widget.visit;
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(day(v.scheduledStart), style: const TextStyle(fontWeight: FontWeight.w600)),
                    Text(
                      '${clock(v.scheduledStart)} to ${clock(v.scheduledEnd)}',
                      style: const TextStyle(color: Brand.slate600),
                    ),
                  ],
                ),
              ),
              StatusBadge(v.status),
            ],
          ),
          if (v.checkedInAt != null)
            Text(
              'Arrived ${clock(v.checkedInAt!)}${v.checkedOutAt == null ? '' : ', left ${clock(v.checkedOutAt!)}'}',
              style: const TextStyle(color: Brand.slate600),
            ),
          if (v.providerNotes != null) Text('“${v.providerNotes}”'),
          if (v.status == 'scheduled') ...[
            const SizedBox(height: 12),
            if (_code == null)
              BusyButton(
                label: 'Show door code',
                outlined: true,
                onPressed: () async {
                  final c = await context.api.doorCode(v.id);
                  if (mounted) setState(() => _code = c);
                },
              )
            else
              Panel(
                color: Brand.c50,
                borderColor: Brand.c200,
                child: Column(
                  children: [
                    const Text('Door code', style: TextStyle(color: Brand.c800)),
                    Text(
                      _code!.code,
                      style: const TextStyle(fontSize: 36, fontWeight: FontWeight.w700, letterSpacing: 8),
                    ),
                    if (_code!.validUntil != null)
                      Text(
                        'Valid until ${when(_code!.validUntil!)}. Asking again makes a new code.',
                        textAlign: TextAlign.center,
                        style: const TextStyle(color: Brand.slate600, fontSize: 13),
                      ),
                  ],
                ),
              ),
            const SizedBox(height: 6),
            const Text(
              'Read this code to the provider when they arrive. It proves they are at your door.',
              style: TextStyle(color: Brand.slate500, fontSize: 13),
            ),
          ],
          if (v.status == 'completed') ...[
            const SizedBox(height: 12),
            BusyButton(
              label: 'Confirm the work is done',
              onPressed: () async {
                await context.api.confirmVisit(v.id);
                await widget.onChanged();
              },
            ),
          ],
        ],
      ),
    );
  }
}

class _VariationCard extends StatelessWidget {
  const _VariationCard({required this.v, required this.onChanged});

  final Variation v;
  final Future<void> Function() onChanged;

  @override
  Widget build(BuildContext context) {
    final delta = v.clientDeltaMinor;
    return Panel(
      color: v.status == 'pending' ? Brand.amber50 : null,
      borderColor: v.status == 'pending' ? Brand.amber200 : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(child: Text(v.description)),
              StatusBadge(v.status),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            '${delta > 0
                ? 'Adds ${kes(delta)}'
                : delta < 0
                ? 'Saves ${kes(-delta)}'
                : 'No change to the price'}'
            ' · asked by ${v.raisedBy == 'provider' ? 'the provider' : 'Homend'}',
            style: const TextStyle(color: Brand.slate600, fontSize: 13),
          ),
          if (v.status == 'pending') ...[
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: BusyButton(
                    label: 'Decline',
                    outlined: true,
                    onPressed: () async {
                      await context.api.rejectVariation(v.id, 'Declined by the client');
                      await onChanged();
                    },
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: BusyButton(
                    label: 'Approve',
                    onPressed: () async {
                      await context.api.approveVariation(v.id);
                      await onChanged();
                    },
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _RatingForm extends StatefulWidget {
  const _RatingForm({required this.request, required this.onDone});

  final ServiceRequest request;
  final Future<void> Function() onDone;

  @override
  State<_RatingForm> createState() => _RatingFormState();
}

class _RatingFormState extends State<_RatingForm> {
  int _stars = 0;
  final Map<String, int?> _sub = {'punctuality': null, 'quality': null, 'conduct': null};
  final _comment = TextEditingController();

  @override
  Widget build(BuildContext context) => Panel(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text(
          'Your rating closes the job and helps us keep standards high.',
          style: TextStyle(color: Brand.slate600),
        ),
        const SizedBox(height: 12),
        Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            for (var n = 1; n <= 5; n++)
              IconButton(
                tooltip: '$n star${n > 1 ? 's' : ''}',
                iconSize: 40,
                icon: Icon(
                  n <= _stars ? Icons.star_rounded : Icons.star_outline_rounded,
                  color: n <= _stars ? Brand.accent : Brand.slate300,
                ),
                onPressed: () => setState(() => _stars = n),
              ),
          ],
        ),
        const Text(
          '1 is poor, 5 is excellent.',
          textAlign: TextAlign.center,
          style: TextStyle(color: Brand.slate500, fontSize: 13),
        ),
        const SizedBox(height: 12),
        for (final k in _sub.keys)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Row(
              children: [
                Expanded(child: Text(labelOf(k))),
                DropdownButton<int?>(
                  value: _sub[k],
                  hint: const Text('–'),
                  items: [
                    const DropdownMenuItem(value: null, child: Text('–')),
                    for (var n = 1; n <= 5; n++) DropdownMenuItem(value: n, child: Text('$n')),
                  ],
                  onChanged: (v) => setState(() => _sub[k] = v),
                ),
              ],
            ),
          ),
        TextField(
          controller: _comment,
          minLines: 2,
          maxLines: 4,
          maxLength: 1000,
          decoration: const InputDecoration(hintText: 'Anything to add? (optional)'),
        ),
        const SizedBox(height: 8),
        BusyButton(
          label: 'Submit rating',
          onPressed: _stars == 0
              ? null
              : () async {
                  final done =
                      widget.request.visits
                          .where((v) => v.status == 'client_confirmed' || v.status == 'completed')
                          .toList()
                        ..sort((a, b) => b.scheduledStart.compareTo(a.scheduledStart));
                  await context.api.rate(
                    widget.request.id,
                    RatingInput(
                      stars: _stars,
                      visitId: done.firstOrNull?.id,
                      punctuality: _sub['punctuality'],
                      quality: _sub['quality'],
                      conduct: _sub['conduct'],
                      comment: _comment.text,
                    ),
                  );
                  if (!context.mounted) return;
                  showMessage(context, 'Thank you for your rating.');
                  await widget.onDone();
                },
        ),
      ],
    ),
  );
}

class _CancelDialog extends StatefulWidget {
  const _CancelDialog();

  @override
  State<_CancelDialog> createState() => _CancelDialogState();
}

class _CancelDialogState extends State<_CancelDialog> {
  String? _reason;
  static const _reasons = ['Plans changed', 'Booked by mistake', 'Found someone else', 'Price too high', 'Other'];

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Why are you cancelling?'),
    content: RadioGroup<String>(
      groupValue: _reason,
      onChanged: (v) => setState(() => _reason = v),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final r in _reasons) RadioListTile<String>(value: r, title: Text(r), contentPadding: EdgeInsets.zero),
          const Text(
            'Cancellation fees, if any, follow the published terms and appear on your invoice.',
            style: TextStyle(color: Brand.slate500, fontSize: 13),
          ),
        ],
      ),
    ),
    actions: [
      TextButton(onPressed: () => Navigator.pop(context), child: const Text('Keep booking')),
      TextButton(
        style: TextButton.styleFrom(foregroundColor: Brand.red700),
        onPressed: _reason == null ? null : () => Navigator.pop(context, _reason),
        child: const Text('Cancel booking'),
      ),
    ],
  );
}
