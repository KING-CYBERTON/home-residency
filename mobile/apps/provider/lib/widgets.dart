import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

class VisitWithJob {
  const VisitWithJob(this.visit, this.job);
  final Visit visit;
  final Job job;
}

bool isSameDay(DateTime a, DateTime b) => a.year == b.year && a.month == b.month && a.day == b.day;

/// Open visits (scheduled or checked in) from today on, soonest first.
List<VisitWithJob> upcomingVisits(List<Job> jobs) {
  final now = DateTime.now();
  final startOfToday = DateTime(now.year, now.month, now.day);
  final out = [
    for (final j in jobs)
      for (final v in j.visits)
        if ((v.status == 'scheduled' || v.status == 'checked_in') && !v.scheduledEnd.isBefore(startOfToday))
          VisitWithJob(v, j),
  ]..sort((a, b) => a.visit.scheduledStart.compareTo(b.visit.scheduledStart));
  return out;
}

/// "Wed 14 Oct, 09:00 to 13:00 · Deep clean · Kilimani".
class VisitLine extends StatelessWidget {
  const VisitLine(this.v, {super.key});

  final VisitWithJob v;

  @override
  Widget build(BuildContext context) => Panel(
    onTap: () => context.go('/jobs/${v.job.assignmentId}'),
    child: Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '${when(v.visit.scheduledStart)} to ${clock(v.visit.scheduledEnd)}',
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
              Text(
                [v.job.service.name, ?v.job.address?.estate].join(' · '),
                style: const TextStyle(color: Brand.slate600),
              ),
            ],
          ),
        ),
        StatusBadge(v.visit.status),
      ],
    ),
  );
}
