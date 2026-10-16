import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';
import '../offline_queue.dart';
import '../widgets.dart';

class _Dashboard {
  _Dashboard({
    required this.me,
    this.applications = const [],
    this.contract,
    this.offers = const [],
    this.jobs = const [],
    this.earnings,
    this.openComplaints = 0,
  });

  final Me me;
  final List<Application> applications;
  final Contract? contract;
  final List<Offer> offers;
  final List<Job> jobs;
  final EarningsSummary? earnings;
  final int openComplaints;
}

/// What to do next, depending on where the provider is: applying, approved, active or suspended.
class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  Future<_Dashboard> _load(BuildContext context) async {
    final api = context.api;
    final me = await context.session.refreshMe();
    final complaints = api.providerComplaints().catchError((_) => <Complaint>[]);
    final status = me.providerStatus ?? 'applicant';
    final open = (await complaints).where((c) => c.status == 'awaiting_provider_account').length;
    switch (status) {
      case 'applicant' || 'withdrawn':
        return _Dashboard(me: me, applications: await api.applications(), openComplaints: open);
      case 'approved':
        return _Dashboard(me: me, contract: await api.contract(), openComplaints: open);
      case 'active':
        final r = await Future.wait([api.offers(), api.jobs(limit: 50), api.earningsSummary()]);
        return _Dashboard(
          me: me,
          offers: (r[0] as List<Offer>).where((o) => o.status == 'offered').toList(),
          jobs: (r[1] as ApiPage<Job>).data,
          earnings: r[2] as EarningsSummary,
          openComplaints: open,
        );
      default:
        return _Dashboard(me: me, openComplaints: open);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: providerBar(context),
    body: AsyncView<_Dashboard>(
      load: () => _load(context),
      builder: (context, d, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PendingSyncBanner(),
          Text('Hello, ${d.me.firstName}', style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w600)),
          const SizedBox(height: 16),
          if (d.openComplaints > 0) ...[
            Notice(
              'A complaint needs your side of the story. Tap to answer.',
              warning: true,
              onTap: () => context.go('/more/complaints'),
            ),
            const SizedBox(height: 16),
          ],
          ..._body(context, d),
        ],
      ),
    ),
  );

  List<Widget> _body(BuildContext context, _Dashboard d) {
    final status = d.me.providerStatus ?? 'applicant';
    switch (status) {
      case 'applicant' || 'withdrawn':
        return [
          Panel(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text('Your application', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 17)),
                const SizedBox(height: 8),
                if (d.applications.isEmpty) ...[
                  const Text('Choose the work you want to do and upload your documents.'),
                  const SizedBox(height: 16),
                  FilledButton(
                    onPressed: () => context.go('/more/applications'),
                    child: const Text('Start my application'),
                  ),
                ],
                for (final a in d.applications)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text(a.reference),
                    subtitle: a.total > 0 ? Text('${a.satisfied} of ${a.total} done') : null,
                    trailing: StatusBadge(a.status),
                    onTap: () => context.go('/more/applications/${a.id}'),
                  ),
              ],
            ),
          ),
        ];
      case 'approved':
        final signed = d.contract?.signedAt;
        return [
          Panel(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text('You are approved', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 17)),
                const SizedBox(height: 8),
                if (signed != null)
                  Text(
                    'Contract signed ${day(signed)}. Management will activate your account, then job offers will come here and by SMS.',
                  )
                else ...[
                  const Text('Read and sign your contract and code of conduct to start getting jobs.'),
                  const SizedBox(height: 16),
                  FilledButton(onPressed: () => context.go('/more/contract'), child: const Text('Sign my contract')),
                ],
                const SizedBox(height: 8),
                TextButton(onPressed: () => context.go('/more/training'), child: const Text('See my training')),
              ],
            ),
          ),
        ];
      case 'active':
        final visits = upcomingVisits(d.jobs);
        final now = DateTime.now();
        final today = visits.where((v) => isSameDay(v.visit.scheduledStart, now)).toList();
        final later = visits
            .where((v) => v.visit.scheduledStart.isAfter(DateTime(now.year, now.month, now.day + 1)))
            .take(5);
        return [
          if (d.offers.isNotEmpty) ...[
            Panel(
              color: Brand.c700,
              borderColor: Brand.c700,
              onTap: () => context.go('/offers'),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '${d.offers.length} new job offer${d.offers.length > 1 ? 's' : ''}',
                      style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w600, fontSize: 17),
                    ),
                  ),
                  const Text('Answer →', style: TextStyle(color: Colors.white)),
                ],
              ),
            ),
          ],
          const SectionTitle('Today'),
          if (today.isEmpty) const EmptyState('No visits today.'),
          ...spaced(today.map((v) => VisitLine(v))),
          if (later.isNotEmpty) ...[const SectionTitle('Coming up'), ...spaced(later.map((v) => VisitLine(v)))],
          if (d.earnings case final e?) ...[
            const SectionTitle('Pay'),
            Panel(
              onTap: () => context.go('/pay'),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'To be paid${e.nextPayoutDate == null ? '' : ' on ${day(e.nextPayoutDate!)}'}',
                    style: const TextStyle(color: Brand.slate500),
                  ),
                  Text(kes(e.payableMinor), style: const TextStyle(fontSize: 24, fontWeight: FontWeight.w700)),
                  if (e.pendingMinor > 0)
                    Text(
                      'Waiting for client confirmation: ${kes(e.pendingMinor)}',
                      style: const TextStyle(color: Brand.slate600),
                    ),
                ],
              ),
            ),
          ],
        ];
      default:
        return [
          Panel(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  'Your account is ${labelOf(status).toLowerCase()}',
                  style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 17),
                ),
                const SizedBox(height: 8),
                const Text('You will not get new job offers for now. Please call the office if you have questions.'),
                TextButton(onPressed: () => context.go('/more/training'), child: const Text('See my training')),
              ],
            ),
          ),
        ];
    }
  }
}

/// Shows how many check-ins are saved on the phone and not yet sent.
class PendingSyncBanner extends StatelessWidget {
  const PendingSyncBanner({super.key});

  @override
  Widget build(BuildContext context) {
    final q = QueueScope.of(context);
    final waiting = q.waitingCount;
    final rejected = q.events.where((e) => !e.isWaiting).length;
    if (waiting == 0 && rejected == 0) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Notice(
        [
          if (waiting > 0) '$waiting check-in${waiting > 1 ? 's' : ''} saved on this phone, waiting to send.',
          if (rejected > 0) '$rejected not accepted by the office. Open the job to see why.',
        ].join(' '),
        warning: rejected > 0,
        onTap: () => q.flush(),
      ),
    );
  }
}
