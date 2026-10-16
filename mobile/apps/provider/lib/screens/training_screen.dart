import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

class TrainingScreen extends StatelessWidget {
  const TrainingScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Training')),
    body: AsyncView<List<TrainingEnrolment>>(
      load: () => context.api.trainings(),
      builder: (context, list, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (list.isEmpty)
            const EmptyState(
              'No training booked yet. The office will SMS you a date once your application is approved.',
            ),
          ...spaced(
            list.map(
              (t) => Panel(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            '${t.courseName}${t.isRetraining ? ' (retraining)' : ''}',
                            style: const TextStyle(fontWeight: FontWeight.w600),
                          ),
                        ),
                        StatusBadge(t.status),
                      ],
                    ),
                    if (t.scheduledFor != null)
                      Text(when(t.scheduledFor!), style: const TextStyle(color: Brand.slate600)),
                    if (t.score != null)
                      Text(
                        'Score ${t.score!.toStringAsFixed(0)}${t.passMark == null ? '' : ' (pass mark ${t.passMark})'}',
                        style: const TextStyle(color: Brand.slate600),
                      ),
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
