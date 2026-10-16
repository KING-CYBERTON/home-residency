import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';
import '../widgets.dart';

class _Home {
  _Home(this.requests, this.unpaid);
  final List<ServiceRequest> requests;
  final List<Invoice> unpaid;
}

/// Needs you, to pay, upcoming and recent: the same sections as the portal's home page.
class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  Future<_Home> _load(BuildContext context) async {
    final api = context.api;
    final r = await Future.wait([
      api.requests(limit: 50),
      api.invoices(status: 'issued', limit: 10),
      api.invoices(status: 'partially_paid', limit: 10),
    ]);
    return _Home(
      (r[0] as ApiPage<ServiceRequest>).data,
      [...(r[1] as ApiPage<Invoice>).data, ...(r[2] as ApiPage<Invoice>).data].where((i) => i.isPayable).toList(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final me = HomendScope.of(context).me;
    return Scaffold(
      appBar: clientBar(context),
      body: AsyncView<_Home>(
        load: () => _load(context),
        builder: (context, d, _) {
          final action = d.requests.where((r) => r.needsClient).toList();
          final active = d.requests.where((r) => r.isActive && !r.needsClient).toList()
            ..sort((a, b) => a.requestedStart.compareTo(b.requestedStart));
          final recent = d.requests.where((r) => !r.isActive && !r.needsClient).take(3).toList();
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                'Hello${me == null ? '' : ', ${me.firstName}'}',
                style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: 16),
              Panel(
                color: Brand.c700,
                borderColor: Brand.c700,
                onTap: () => context.go('/book'),
                child: const Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            'Book a service',
                            style: TextStyle(color: Colors.white, fontWeight: FontWeight.w600, fontSize: 17),
                          ),
                          Text('Cleaning, laundry, childcare and more', style: TextStyle(color: Brand.c100)),
                        ],
                      ),
                    ),
                    Icon(Icons.chevron_right, color: Colors.white),
                  ],
                ),
              ),
              if (action.isNotEmpty) ...[
                const SectionTitle('Needs you', color: Brand.amber800),
                ...spaced(action.map((r) => RequestCard(r))),
              ],
              if (d.unpaid.isNotEmpty) ...[
                const SectionTitle('To pay'),
                ...spaced(
                  d.unpaid.map(
                    (i) => Panel(
                      onTap: () => context.go('/pay/${i.id}'),
                      color: Brand.amber50,
                      borderColor: Brand.amber200,
                      child: Row(
                        children: [
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(kes(i.dueMinor), style: const TextStyle(fontWeight: FontWeight.w600)),
                                Text('Invoice ${i.reference}', style: const TextStyle(color: Brand.slate600)),
                              ],
                            ),
                          ),
                          FilledButton(
                            style: FilledButton.styleFrom(minimumSize: const Size(0, 40)),
                            onPressed: () => context.go('/pay/${i.id}'),
                            child: const Text('Pay with M-Pesa'),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              ],
              const SectionTitle('Upcoming and in progress'),
              if (active.isEmpty) const EmptyState('Nothing booked right now.'),
              ...spaced(active.map((r) => RequestCard(r))),
              if (recent.isNotEmpty) ...[
                const SectionTitle('Recent'),
                ...spaced(recent.map((r) => RequestCard(r))),
                TextButton(onPressed: () => context.go('/bookings'), child: const Text('All bookings')),
              ],
            ],
          );
        },
      ),
    );
  }
}
