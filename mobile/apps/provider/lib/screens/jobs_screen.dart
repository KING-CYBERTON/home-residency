import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';
import 'home_screen.dart';

class JobsScreen extends StatefulWidget {
  const JobsScreen({super.key});

  @override
  State<JobsScreen> createState() => _JobsScreenState();
}

class _JobsScreenState extends State<JobsScreen> {
  bool _past = false;
  final _jobs = <Job>[];
  String? _cursor;

  static const _live = {'awaiting_provider', 'confirmed', 'in_progress'};

  Future<List<Job>> _load({bool more = false}) async {
    final page = await context.api.jobs(cursor: more ? _cursor : null, limit: 50);
    if (!more) _jobs.clear();
    _jobs.addAll(page.data);
    _cursor = page.nextCursor;
    return _jobs;
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: providerBar(context, title: 'My jobs'),
    body: AsyncView<List<Job>>(
      load: _load,
      builder: (context, jobs, reload) {
        final shown = jobs.where((j) => _live.contains(j.status) != _past).toList();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const PendingSyncBanner(),
            SegmentedButton<bool>(
              segments: const [
                ButtonSegment(value: false, label: Text('Current')),
                ButtonSegment(value: true, label: Text('Past')),
              ],
              selected: {_past},
              onSelectionChanged: (s) => setState(() => _past = s.first),
            ),
            const SizedBox(height: 16),
            if (shown.isEmpty)
              EmptyState(_past ? 'No past jobs yet.' : 'No current jobs. Accept an offer to see it here.'),
            ...spaced(shown.map((j) => _JobCard(j))),
            if (_cursor != null)
              TextButton(
                onPressed: () async {
                  await _load(more: true);
                  setState(() {});
                },
                child: const Text('Show more'),
              ),
          ],
        );
      },
    ),
  );
}

class _JobCard extends StatelessWidget {
  const _JobCard(this.job);

  final Job job;

  @override
  Widget build(BuildContext context) {
    final next = job.nextVisit;
    return Panel(
      onTap: () => context.go('/jobs/${job.assignmentId}'),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(job.service.name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 16)),
              ),
              if (job.status != null) StatusBadge(job.status!),
            ],
          ),
          Text([job.requestReference, ?job.address?.estate].join(' · '), style: const TextStyle(color: Brand.slate500)),
          if (next != null)
            Padding(padding: const EdgeInsets.only(top: 6), child: Text('Next visit ${when(next.scheduledStart)}')),
        ],
      ),
    );
  }
}
