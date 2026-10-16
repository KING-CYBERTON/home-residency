import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

/// The provider's score: ratings, complaints, supervisor spot checks and reliability.
class AppraisalScreen extends StatelessWidget {
  const AppraisalScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('My score')),
    body: AsyncView<Appraisal>(
      load: () => context.api.appraisal(),
      builder: (context, a, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Panel(
            child: Column(
              children: [
                const Text('Your score', style: TextStyle(color: Brand.slate500)),
                Text(
                  a.totalScore?.toStringAsFixed(0) ?? '–',
                  style: const TextStyle(fontSize: 48, fontWeight: FontWeight.w700, color: Brand.c700),
                ),
                if (a.periodEnd != null)
                  Text('Up to ${date(a.periodEnd!)}', style: const TextStyle(color: Brand.slate500)),
              ],
            ),
          ),
          const SectionTitle('What makes it up'),
          Panel(
            child: Column(
              children: [
                for (final (label, v) in [
                  ('Client ratings', a.rating),
                  ('Complaints', a.complaints),
                  ('Spot checks', a.spotChecks),
                  ('Turning up on time', a.reliability),
                ])
                  KeyValue(label, v?.toStringAsFixed(0) ?? '–'),
              ],
            ),
          ),
          if (a.history.isNotEmpty) ...[
            const SectionTitle('Past scores'),
            Panel(child: Column(children: [for (final (d, s) in a.history) KeyValue(date(d), s.toStringAsFixed(0))])),
          ],
          const SizedBox(height: 16),
          const Text(
            'A higher score means more job offers. Arrive on time, follow the checklist and keep clients informed through the office.',
            style: TextStyle(color: Brand.slate500),
          ),
        ],
      ),
    ),
  );
}
