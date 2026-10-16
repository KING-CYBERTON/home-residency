import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';

class PayScreen extends StatelessWidget {
  const PayScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: clientBar(context, title: 'Payments'),
    body: AsyncView<ApiPage<Invoice>>(
      load: () => context.api.invoices(limit: 50),
      builder: (context, page, _) {
        final due = page.data.where((i) => i.isPayable).toList();
        final rest = page.data.where((i) => !i.isPayable).toList();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (page.data.isEmpty) const EmptyState('No invoices yet. You get one after each job.'),
            if (due.isNotEmpty) ...[
              const SectionTitle('To pay', color: Brand.amber800),
              ...spaced(due.map((i) => _InvoiceRow(i))),
            ],
            if (rest.isNotEmpty) ...[const SectionTitle('History'), ...spaced(rest.map((i) => _InvoiceRow(i)))],
          ],
        );
      },
    ),
  );
}

class _InvoiceRow extends StatelessWidget {
  const _InvoiceRow(this.i);

  final Invoice i;

  @override
  Widget build(BuildContext context) => Panel(
    onTap: () => context.go('/pay/${i.id}'),
    child: Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('Invoice ${i.reference}', style: const TextStyle(fontWeight: FontWeight.w600)),
              Text(
                [kes(i.isPayable ? i.dueMinor : i.totalMinor), if (i.issuedAt != null) day(i.issuedAt!)].join(' · '),
                style: const TextStyle(color: Brand.slate600),
              ),
            ],
          ),
        ),
        StatusBadge(i.status),
      ],
    ),
  );
}
