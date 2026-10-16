import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

/// Complaints about the provider's work. They are heard before any decision (report §5.4).
class ProviderComplaintsScreen extends StatelessWidget {
  const ProviderComplaintsScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Complaints')),
    body: AsyncView<List<Complaint>>(
      load: () => context.api.providerComplaints(),
      builder: (context, list, reload) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (list.isEmpty) const EmptyState('No complaints. Keep it up.'),
          ...spaced(
            list.map(
              (c) => Panel(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(c.reference, style: const TextStyle(fontWeight: FontWeight.w600)),
                        ),
                        StatusBadge(c.status),
                      ],
                    ),
                    Text('${labelOf(c.severity)} · ${day(c.createdAt)}', style: const TextStyle(color: Brand.slate500)),
                    const SizedBox(height: 6),
                    Text(c.summary),
                    if (c.outcomeSummary != null) ...[
                      const SizedBox(height: 6),
                      Text('Outcome: ${c.outcomeSummary}', style: const TextStyle(fontWeight: FontWeight.w600)),
                    ],
                    if (c.status == 'awaiting_provider_account') ...[
                      const SizedBox(height: 12),
                      FilledButton(
                        onPressed: () async {
                          final sent = await showModalBottomSheet<bool>(
                            context: context,
                            isScrollControlled: true,
                            builder: (_) => _AccountSheet(complaint: c),
                          );
                          if (sent == true) await reload();
                        },
                        child: const Text('Give my side of the story'),
                      ),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    ),
  );
}

class _AccountSheet extends StatefulWidget {
  const _AccountSheet({required this.complaint});

  final Complaint complaint;

  @override
  State<_AccountSheet> createState() => _AccountSheetState();
}

class _AccountSheetState extends State<_AccountSheet> {
  final _text = TextEditingController();

  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + MediaQuery.viewInsetsOf(context).bottom),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text('Your side of the story', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
        const SizedBox(height: 8),
        const Text(
          'Say what happened in your own words. The office reads this before deciding anything.',
          style: TextStyle(color: Brand.slate600),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _text,
          minLines: 5,
          maxLines: 10,
          maxLength: 4000,
          decoration: const InputDecoration(labelText: 'What happened'),
        ),
        const SizedBox(height: 12),
        BusyButton(
          label: 'Send to the office',
          onPressed: () async {
            if (_text.text.trim().length < 10) {
              showMessage(context, 'Please write a little more.', error: true);
              return;
            }
            await context.api.giveAccount(widget.complaint.id, _text.text.trim());
            if (context.mounted) Navigator.pop(context, true);
          },
        ),
      ],
    ),
  );
}
