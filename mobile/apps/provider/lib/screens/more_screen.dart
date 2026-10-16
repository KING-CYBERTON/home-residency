import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';
import '../offline_queue.dart';

class MoreScreen extends StatelessWidget {
  const MoreScreen({super.key});

  Future<void> _signOut(BuildContext context) async {
    final queue = QueueScope.of(context);
    final session = context.session;
    final waiting = queue.waitingCount;
    if (waiting > 0) await queue.flush();
    if (!context.mounted) return;
    final left = queue.waitingCount;
    final ok = await confirm(
      context,
      left > 0
          ? '$left check-in${left > 1 ? 's have' : ' has'} not been sent yet. If you sign out now ${left > 1 ? 'they' : 'it'} will be lost. Sign out anyway?'
          : 'Sign out? Job details saved on this phone will be removed.',
      yes: 'Sign out',
      danger: left > 0,
    );
    if (!ok) return;
    await queue.clear();
    await session.signOut();
  }

  @override
  Widget build(BuildContext context) {
    final me = HomendScope.of(context).me;
    return Scaffold(
      appBar: providerBar(context, title: 'More'),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (me != null) ...[
            Text(me.fullName, style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w600)),
            Text(displayPhone(me.phone), style: const TextStyle(color: Brand.slate500)),
            const SizedBox(height: 16),
          ],
          MenuGroup(
            items: [
              MenuItem('My profile and M-Pesa number', () => context.go('/more/profile')),
              MenuItem('When I can work, and time off', () => context.go('/more/availability')),
              MenuItem('My applications', () => context.go('/more/applications')),
              MenuItem('My documents', () => context.go('/more/documents')),
              MenuItem('Training', () => context.go('/more/training')),
              MenuItem('Contract', () => context.go('/more/contract')),
            ],
          ),
          const SizedBox(height: 16),
          MenuGroup(
            items: [
              MenuItem('My score', () => context.go('/more/appraisal')),
              MenuItem('Complaints', () => context.go('/more/complaints')),
              MenuItem('Messages with Homend', () => context.go('/more/messages')),
              MenuItem('Notifications', () => context.go('/more/notifications')),
            ],
          ),
          const SizedBox(height: 16),
          const Text(
            'No smartphone data? Dial our USSD code or reply to our SMS to accept jobs and check in.',
            style: TextStyle(color: Brand.slate500),
          ),
          const SizedBox(height: 24),
          BusyButton(label: 'Sign out', outlined: true, onPressed: () => _signOut(context)),
        ],
      ),
    );
  }
}
