import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

/// Household subscription plans: start one (paid by M-Pesa) or cancel at the end of the period.
class PlanScreen extends StatelessWidget {
  const PlanScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Household plan')),
    body: AsyncView<(Subscription?, List<SubscriptionPlan>)>(
      load: () async {
        final api = context.api;
        final r = await Future.wait([api.subscription(), api.subscriptionPlans()]);
        return (r[0] as Subscription?, r[1] as List<SubscriptionPlan>);
      },
      builder: (context, d, reload) {
        final sub = d.$1;
        final active = sub != null && sub.status == 'active';
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (active) ...[
              Panel(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(sub.plan.name, style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
                        ),
                        StatusBadge(sub.status),
                      ],
                    ),
                    Text('${kes(sub.plan.priceMinor)} ${labelOf(sub.plan.billingPeriod).toLowerCase()}'),
                    if (sub.currentPeriodEnd != null)
                      Text(
                        sub.cancelAtPeriodEnd
                            ? 'Ends on ${date(sub.currentPeriodEnd!)}.'
                            : 'Renews on ${date(sub.currentPeriodEnd!)}.',
                        style: const TextStyle(color: Brand.slate600),
                      ),
                    if (!sub.cancelAtPeriodEnd) ...[
                      const SizedBox(height: 12),
                      BusyButton(
                        label: 'Cancel at the end of this period',
                        outlined: true,
                        onPressed: () async {
                          if (!await confirm(
                            context,
                            'Cancel your plan? It stays active until the end of this period.',
                            yes: 'Cancel plan',
                            no: 'Keep it',
                            danger: true,
                          )) {
                            return;
                          }
                          if (!context.mounted) return;
                          await context.api.cancelSubscription();
                          await reload();
                        },
                      ),
                    ],
                  ],
                ),
              ),
              const SectionTitle('Other plans'),
            ] else
              const Padding(
                padding: EdgeInsets.only(bottom: 16),
                child: Text(
                  'A plan gives your household regular visits at a set monthly price.',
                  style: TextStyle(color: Brand.slate600),
                ),
              ),
            if (d.$2.isEmpty) const EmptyState('No plans on offer right now.'),
            ...spaced(
              d.$2
                  .where((p) => !active || p.id != sub.plan.id)
                  .map(
                    (p) => Panel(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(p.name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 16)),
                              ),
                              Text(kes(p.priceMinor), style: const TextStyle(fontWeight: FontWeight.w700)),
                            ],
                          ),
                          Text(labelOf(p.billingPeriod), style: const TextStyle(color: Brand.slate500)),
                          if (p.description != null)
                            Padding(padding: const EdgeInsets.only(top: 6), child: Text(p.description!)),
                          if (!active) ...[
                            const SizedBox(height: 12),
                            BusyButton(
                              label: 'Choose ${p.name}',
                              onPressed: () async {
                                final phone = context.session.me?.phone;
                                await context.api.subscribe(
                                  p.id,
                                  payPhone: phone,
                                  idempotencyKey: ApiClient.newIdempotencyKey(),
                                );
                                if (!context.mounted) return;
                                showMessage(context, 'Plan started. Approve the M-Pesa prompt on your phone to pay.');
                                await reload();
                              },
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
            ),
          ],
        );
      },
    ),
  );
}
