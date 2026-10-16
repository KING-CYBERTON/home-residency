import 'package:flutter/material.dart';
import 'package:geolocator/geolocator.dart';
import 'package:homend_core/homend_core.dart';
import 'package:url_launcher/url_launcher.dart';

import '../offline_queue.dart';

/// One job: where, what to do, and check-in / check-out for each visit, which work offline.
class JobScreen extends StatefulWidget {
  const JobScreen({super.key, required this.assignmentId});

  final String assignmentId;

  @override
  State<JobScreen> createState() => _JobScreenState();
}

class _JobScreenState extends State<JobScreen> {
  final _view = GlobalKey<AsyncViewState<Job>>();

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Job')),
    body: AsyncView<Job>(
      key: _view,
      load: () => context.api.job(widget.assignmentId),
      builder: (context, job, reload) => _JobBody(job: job, reload: reload),
    ),
  );
}

class _JobBody extends StatelessWidget {
  const _JobBody({required this.job, required this.reload});

  final Job job;
  final Future<void> Function() reload;

  @override
  Widget build(BuildContext context) {
    final a = job.address;
    final visits = [...job.visits]..sort((x, y) => x.scheduledStart.compareTo(y.scheduledStart));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        PageTitle(
          job.service.name,
          subtitle: [job.requestReference, ?job.clientDisplayName].join(' · '),
          trailing: Column(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              if (job.status != null) StatusBadge(job.status!),
              const SizedBox(height: 4),
              Text(kes(job.providerPayMinor), style: const TextStyle(fontWeight: FontWeight.w700)),
            ],
          ),
        ),
        if (a != null) ...[
          const SectionTitle('Where'),
          Panel(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(a.line.isEmpty ? a.label : a.line, style: const TextStyle(fontWeight: FontWeight.w600)),
                if (a.landmark != null) Text('Near ${a.landmark}'),
                if (a.accessDetails != null) ...[const SizedBox(height: 6), Text('Access: ${a.accessDetails}')],
                if (a.lat != null && a.lng != null)
                  TextButton.icon(
                    style: TextButton.styleFrom(padding: EdgeInsets.zero),
                    icon: const Icon(Icons.map_outlined),
                    label: const Text('Open in maps'),
                    onPressed: () => launchUrl(
                      Uri.parse('https://www.google.com/maps/search/?api=1&query=${a.lat},${a.lng}'),
                      mode: LaunchMode.externalApplication,
                    ),
                  ),
                const SizedBox(height: 6),
                const Text(
                  'Keep these details private. Calls to the client go through the office.',
                  style: TextStyle(color: Brand.slate500, fontSize: 13),
                ),
              ],
            ),
          ),
        ],
        if (job.service.scopeItems.isNotEmpty) ...[
          const SectionTitle('What to do'),
          Panel(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [for (final s in job.service.scopeItems) Text('•  $s')],
            ),
          ),
        ],
        const SectionTitle('Visits'),
        ...spaced(visits.map((v) => _VisitCard(visit: v, onChanged: reload))),
        const SizedBox(height: 12),
        const Text(
          'No signal at the house? Check in anyway: it is saved on your phone with the time and sent when you are back online.',
          style: TextStyle(color: Brand.slate500, fontSize: 13),
        ),
        if (job.canVary) ...[
          const SizedBox(height: 16),
          OutlinedButton(
            onPressed: () => showModalBottomSheet(
              context: context,
              isScrollControlled: true,
              builder: (_) => _VariationSheet(job: job),
            ),
            child: const Text('The client wants more work done'),
          ),
        ],
      ],
    );
  }
}

class _VisitCard extends StatefulWidget {
  const _VisitCard({required this.visit, required this.onChanged});

  final Visit visit;
  final Future<void> Function() onChanged;

  @override
  State<_VisitCard> createState() => _VisitCardState();
}

class _VisitCardState extends State<_VisitCard> {
  String _method = 'code';
  final _code = TextEditingController();
  final _notes = TextEditingController();

  Future<void> _checkIn() async {
    final queue = QueueScope.of(context);
    Json payload;
    if (_method == 'code') {
      final code = _code.text.trim();
      if (!RegExp(r'^[0-9]{4}$').hasMatch(code)) {
        showMessage(context, 'Ask the client for the 4-digit door code.', error: true);
        return;
      }
      payload = {'method': 'code', 'code': code};
    } else {
      final pos = await _position(required: true);
      if (pos == null) return;
      payload = {'method': 'geo', 'lat': pos.latitude, 'lng': pos.longitude};
    }
    await queue.add(widget.visit.id, 'check_in', payload);
    _code.clear();
    await widget.onChanged();
  }

  Future<void> _checkOut() async {
    final queue = QueueScope.of(context);
    final pos = await _position(required: false);
    await queue.add(widget.visit.id, 'check_out', {
      'notes': _notes.text.trim().isEmpty ? null : _notes.text.trim(),
      'lat': pos?.latitude,
      'lng': pos?.longitude,
    });
    _notes.clear();
    await widget.onChanged();
  }

  /// Location for a geo check-in. Without permission or GPS it explains what to do instead.
  Future<Position?> _position({required bool required}) async {
    try {
      if (!await Geolocator.isLocationServiceEnabled()) {
        if (required && mounted) showMessage(context, 'Turn on location, or use the door code.', error: true);
        return null;
      }
      var perm = await Geolocator.checkPermission();
      if (perm == LocationPermission.denied) perm = await Geolocator.requestPermission();
      if (perm == LocationPermission.denied || perm == LocationPermission.deniedForever) {
        if (required && mounted) {
          showMessage(context, 'Location is off for this app. Use the door code instead.', error: true);
        }
        return null;
      }
      return await Geolocator.getCurrentPosition(
        locationSettings: const LocationSettings(accuracy: LocationAccuracy.high, timeLimit: Duration(seconds: 20)),
      );
    } catch (_) {
      if (required && mounted) {
        showMessage(context, 'Could not get your location. Use the door code instead.', error: true);
      }
      return null;
    }
  }

  @override
  Widget build(BuildContext context) {
    final v = widget.visit;
    final queued = QueueScope.of(context).forVisit(v.id);
    final waiting = queued.where((e) => e.isWaiting).toList();
    final rejected = queued.where((e) => !e.isWaiting).toList();

    // What the provider should see now, counting events still on the phone.
    var status = v.status;
    if (waiting.any((e) => e.eventType == 'check_out')) {
      status = 'completed';
    } else if (waiting.any((e) => e.eventType == 'check_in') && status == 'scheduled') {
      status = 'checked_in';
    }

    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '${when(v.scheduledStart)} to ${clock(v.scheduledEnd)}',
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
              ),
              StatusBadge(status),
            ],
          ),
          if (v.checkedInAt != null)
            Text('Checked in ${clock(v.checkedInAt!)}', style: const TextStyle(color: Brand.slate600)),
          if (v.checkedOutAt != null)
            Text('Checked out ${clock(v.checkedOutAt!)}', style: const TextStyle(color: Brand.slate600)),
          for (final e in waiting)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                '${e.eventType == 'check_in' ? 'Check-in' : 'Check-out'} at ${clock(e.deviceRecordedAt.toLocal())} saved on this phone, waiting to send.',
                style: const TextStyle(color: Brand.amber800),
              ),
            ),
          for (final e in rejected)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Notice(
                e.rejectedMessage!,
                title: '${e.eventType == 'check_in' ? 'Check-in' : 'Check-out'} not accepted',
                warning: true,
                onTap: () => QueueScope.of(context).dismiss(e.clientEventId),
              ),
            ),
          if (status == 'scheduled') ...[
            const SizedBox(height: 12),
            const Text('How to check in', style: TextStyle(fontWeight: FontWeight.w600)),
            RadioGroup<String>(
              groupValue: _method,
              onChanged: (m) => setState(() => _method = m ?? 'code'),
              child: const Column(
                children: [
                  RadioListTile(
                    value: 'code',
                    title: Text('Door code from the client'),
                    contentPadding: EdgeInsets.zero,
                  ),
                  RadioListTile(value: 'geo', title: Text('My location'), contentPadding: EdgeInsets.zero),
                ],
              ),
            ),
            if (_method == 'code') ...[
              TextField(
                controller: _code,
                keyboardType: TextInputType.number,
                maxLength: 4,
                decoration: const InputDecoration(labelText: '4-digit code', counterText: ''),
              ),
              const SizedBox(height: 12),
            ],
            BusyButton(label: 'Check in', onPressed: _checkIn),
          ],
          if (status == 'checked_in') ...[
            const SizedBox(height: 12),
            TextField(
              controller: _notes,
              minLines: 2,
              maxLines: 4,
              maxLength: 2000,
              decoration: const InputDecoration(labelText: 'Notes for the office (optional)'),
            ),
            const SizedBox(height: 8),
            BusyButton(label: 'Work done, check out', onPressed: _checkOut),
          ],
        ],
      ),
    );
  }
}

class _VariationSheet extends StatefulWidget {
  const _VariationSheet({required this.job});

  final Job job;

  @override
  State<_VariationSheet> createState() => _VariationSheetState();
}

class _VariationSheetState extends State<_VariationSheet> {
  final _desc = TextEditingController();
  final _qty = TextEditingController();

  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + MediaQuery.viewInsetsOf(context).bottom),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text('More work', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
        const SizedBox(height: 8),
        const Text(
          'Do not agree extra work or money with the client directly. Send it here and we confirm it with them first.',
          style: TextStyle(color: Brand.slate600),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _desc,
          minLines: 2,
          maxLines: 4,
          decoration: const InputDecoration(labelText: 'What extra work'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _qty,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          decoration: const InputDecoration(labelText: 'How many (optional)'),
        ),
        const SizedBox(height: 16),
        BusyButton(
          label: 'Send to the office',
          onPressed: () async {
            if (_desc.text.trim().isEmpty) {
              showMessage(context, 'Say what extra work the client wants.', error: true);
              return;
            }
            await context.api.proposeVariation(
              widget.job.assignmentId,
              description: _desc.text.trim(),
              quantity: double.tryParse(_qty.text.trim()),
            );
            if (!context.mounted) return;
            final messenger = ScaffoldMessenger.of(context);
            Navigator.pop(context);
            messenger.showSnackBar(const SnackBar(content: Text('Sent. The office will confirm with the client.')));
          },
        ),
      ],
    ),
  );
}
