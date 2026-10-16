import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

class ComplaintsScreen extends StatelessWidget {
  const ComplaintsScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Complaints')),
    floatingActionButton: FloatingActionButton.extended(
      onPressed: () => context.go('/account/complaints/new'),
      icon: const Icon(Icons.report_outlined),
      label: const Text('Report a problem'),
    ),
    body: AsyncView<ApiPage<Complaint>>(
      load: () => context.api.clientComplaints(limit: 50),
      builder: (context, page, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (page.data.isEmpty) const EmptyState('No complaints. If something goes wrong, tell us here.'),
          ...spaced(
            page.data.map(
              (c) => Panel(
                onTap: () => context.go('/account/complaints/${c.id}'),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            c.summary,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(fontWeight: FontWeight.w600),
                          ),
                          Text('${c.reference} · ${day(c.createdAt)}', style: const TextStyle(color: Brand.slate500)),
                        ],
                      ),
                    ),
                    StatusBadge(c.status),
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

class ComplaintScreen extends StatelessWidget {
  const ComplaintScreen({super.key, required this.id});

  final String id;

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Complaint')),
    body: AsyncView<Complaint>(
      load: () => context.api.clientComplaint(id),
      builder: (context, c, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PageTitle(
            c.reference,
            subtitle: '${labelOf(c.severity)} · ${when(c.createdAt)}',
            trailing: StatusBadge(c.status),
          ),
          const SizedBox(height: 16),
          Panel(child: Text(c.summary)),
          if (c.outcomeSummary != null) ...[
            const SectionTitle('Outcome'),
            Panel(child: Text(c.outcomeSummary!)),
          ] else ...[
            const SizedBox(height: 16),
            const Notice('We look into every complaint, hear the provider, and tell you the outcome here and by SMS.'),
          ],
          if (c.requestId != null)
            TextButton(onPressed: () => context.go('/bookings/${c.requestId}'), child: const Text('See the booking')),
        ],
      ),
    ),
  );
}

/// Raise a complaint. Safety and loss complaints suspend the provider straight away.
class NewComplaintScreen extends StatefulWidget {
  const NewComplaintScreen({super.key, this.requestId});

  final String? requestId;

  @override
  State<NewComplaintScreen> createState() => _NewComplaintScreenState();
}

class _NewComplaintScreenState extends State<NewComplaintScreen> {
  String _severity = 'quality';
  final _summary = TextEditingController();
  final _details = TextEditingController();
  final _evidence = <Document>[];

  static const _options = {
    'quality': 'The work was not done well',
    'conduct': 'The provider behaved badly',
    'safeguarding': 'Someone was put at risk',
    'loss': 'Something was lost, stolen or damaged',
  };

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Report a problem')),
    body: ListView(
      padding: const EdgeInsets.all(16),
      children: [
        const Text('What happened?', style: TextStyle(fontWeight: FontWeight.w600)),
        RadioGroup<String>(
          groupValue: _severity,
          onChanged: (v) => setState(() => _severity = v ?? 'quality'),
          child: Column(
            children: [
              for (final e in _options.entries)
                RadioListTile<String>(value: e.key, title: Text(e.value), contentPadding: EdgeInsets.zero),
            ],
          ),
        ),
        if (_severity == 'safeguarding')
          const Padding(
            padding: EdgeInsets.only(bottom: 12),
            child: Notice('If anyone is in danger now, call 999 or 112 first.', warning: true),
          ),
        TextField(
          controller: _summary,
          maxLength: 200,
          decoration: const InputDecoration(labelText: 'In one line'),
        ),
        const SizedBox(height: 8),
        TextField(
          controller: _details,
          minLines: 4,
          maxLines: 8,
          maxLength: 4000,
          decoration: const InputDecoration(labelText: 'Details (optional)', helperText: 'Kept private and encrypted.'),
        ),
        const SizedBox(height: 8),
        for (final d in _evidence)
          ListTile(leading: const Icon(Icons.attach_file), title: Text(d.originalFilename ?? 'Photo')),
        BusyButton(
          label: 'Add a photo or document',
          outlined: true,
          onPressed: () async {
            final doc = await pickAndUploadDocument(context, documentType: 'other');
            if (doc != null && mounted) setState(() => _evidence.add(doc));
          },
        ),
        const SizedBox(height: 16),
        BusyButton(
          label: 'Send to Homend',
          onPressed: () async {
            if (_summary.text.trim().length < 5) {
              showMessage(context, 'Say in one line what went wrong.', error: true);
              return;
            }
            final c = await context.api.raiseComplaint(
              ComplaintInput(
                severity: _severity,
                summary: _summary.text.trim(),
                details: _details.text,
                requestId: widget.requestId,
                evidenceDocumentIds: _evidence.map((d) => d.id).toList(),
              ),
            );
            if (!context.mounted) return;
            showMessage(context, 'Thank you. Your reference is ${c.reference}.');
            context.go('/account/complaints/${c.id}');
          },
        ),
      ],
    ),
  );
}
