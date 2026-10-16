import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import 'offline_queue.dart';
import 'screens/application_screen.dart';
import 'screens/applications_screen.dart';
import 'screens/appraisal_screen.dart';
import 'screens/availability_screen.dart';
import 'screens/complaints_screen.dart';
import 'screens/contract_screen.dart';
import 'screens/documents_screen.dart';
import 'screens/home_screen.dart';
import 'screens/job_screen.dart';
import 'screens/jobs_screen.dart';
import 'screens/more_screen.dart';
import 'screens/offers_screen.dart';
import 'screens/pay_screen.dart';
import 'screens/profile_screen.dart';
import 'screens/training_screen.dart';

/// Privacy notice and vetting consent version, the same as the provider portal's `/join`.
const noticeVersion = '2026-10';

class ProviderApp extends StatefulWidget {
  const ProviderApp({super.key, required this.session, required this.queue});

  final SessionController session;
  final OfflineQueue queue;

  @override
  State<ProviderApp> createState() => _ProviderAppState();
}

class _ProviderAppState extends State<ProviderApp> {
  late final GoRouter _router = buildRouter(widget.session);

  @override
  void dispose() {
    _router.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => HomendScope(
    session: widget.session,
    child: QueueScope(
      queue: widget.queue,
      child: MaterialApp.router(
        title: 'Homend Provider',
        theme: homendTheme(),
        routerConfig: _router,
        debugShowCheckedModeBanner: false,
      ),
    ),
  );
}

GoRouter buildRouter(SessionController session) => GoRouter(
  refreshListenable: session,
  initialLocation: '/',
  redirect: (context, state) {
    final open = state.matchedLocation == '/sign-in' || state.matchedLocation == '/join';
    if (!session.isSignedIn) return open ? null : '/sign-in';
    return open ? '/' : null;
  },
  routes: [
    GoRoute(path: '/sign-in', builder: (_, _) => const ProviderSignIn()),
    GoRoute(path: '/join', builder: (_, _) => const ProviderSignIn(join: true)),
    StatefulShellRoute.indexedStack(
      builder: (context, state, shell) => ProviderShell(shell: shell),
      branches: [
        StatefulShellBranch(
          routes: [GoRoute(path: '/', builder: (_, _) => const HomeScreen())],
        ),
        StatefulShellBranch(
          routes: [GoRoute(path: '/offers', builder: (_, _) => const OffersScreen())],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/jobs',
              builder: (_, _) => const JobsScreen(),
              routes: [
                GoRoute(
                  path: ':id',
                  builder: (_, s) => JobScreen(assignmentId: s.pathParameters['id']!),
                ),
              ],
            ),
          ],
        ),
        StatefulShellBranch(
          routes: [GoRoute(path: '/pay', builder: (_, _) => const PayScreen())],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/more',
              builder: (_, _) => const MoreScreen(),
              routes: [
                GoRoute(path: 'profile', builder: (_, _) => const ProfileScreen()),
                GoRoute(path: 'availability', builder: (_, _) => const AvailabilityScreen()),
                GoRoute(
                  path: 'applications',
                  builder: (_, _) => const ApplicationsScreen(),
                  routes: [
                    GoRoute(
                      path: ':id',
                      builder: (_, s) => ApplicationScreen(id: s.pathParameters['id']!),
                    ),
                  ],
                ),
                GoRoute(path: 'documents', builder: (_, _) => const DocumentsScreen()),
                GoRoute(path: 'training', builder: (_, _) => const TrainingScreen()),
                GoRoute(path: 'contract', builder: (_, _) => const ContractScreen()),
                GoRoute(path: 'appraisal', builder: (_, _) => const AppraisalScreen()),
                GoRoute(path: 'complaints', builder: (_, _) => const ProviderComplaintsScreen()),
                GoRoute(path: 'messages', builder: (_, _) => const ConversationsScreen()),
                GoRoute(
                  path: 'notifications',
                  builder: (_, _) => NotificationsScreen(onOpen: openNotification),
                ),
              ],
            ),
          ],
        ),
      ],
    ),
  ],
);

/// Notifications carry ids in `data`; open the job, offer or application they are about.
void openNotification(BuildContext context, AppNotification n) {
  final d = n.data;
  final area = n.eventKey.split('.').first;
  if (d['assignment_id'] case final String id) {
    context.go('/jobs/$id');
  } else if (d['application_id'] case final String id) {
    context.go('/more/applications/$id');
  } else {
    final to = switch (area) {
      'offer' || 'assignment' => '/offers',
      'complaint' || 'provider' => '/more/complaints',
      'payout' || 'earning' => '/pay',
      'training' => '/more/training',
      'document' => '/more/documents',
      'message' => '/more/messages',
      'contract' => '/more/contract',
      _ => null,
    };
    if (to != null) context.go(to);
  }
}

class ProviderSignIn extends StatelessWidget {
  const ProviderSignIn({super.key, this.join = false});

  final bool join;

  @override
  Widget build(BuildContext context) => SignInScreen(
    kind: 'provider',
    noticeVersion: noticeVersion,
    startInRegister: join,
    title: 'Sign in to your work account',
    subtitle: 'For Homend cleaners, nannies, caregivers and tradespeople.',
    registerTitle: 'Work with Homend',
    registerSubtitle:
        'Apply in a few steps. We check your documents and references, train you, then send you jobs near you.',
    registerLabel: 'New? Apply to work with us',
    registerConsents: const [
      ConsentTerm(
        purpose: 'privacy_notice',
        text: 'I have read the privacy notice. Homend keeps my details safe and shares them only with the office.',
      ),
      ConsentTerm(
        purpose: 'vetting_checks',
        text: 'I agree to background checks: my ID, Certificate of Good Conduct and referees.',
      ),
    ],
  );
}

/// Bottom navigation like the portal: Home, Offers, Jobs, Pay, More.
class ProviderShell extends StatelessWidget {
  const ProviderShell({super.key, required this.shell});

  final StatefulNavigationShell shell;

  @override
  Widget build(BuildContext context) => Scaffold(
    body: shell,
    bottomNavigationBar: NavigationBar(
      selectedIndex: shell.currentIndex,
      onDestinationSelected: (i) => shell.goBranch(i, initialLocation: i == shell.currentIndex),
      destinations: const [
        NavigationDestination(icon: Icon(Icons.home_outlined), selectedIcon: Icon(Icons.home), label: 'Home'),
        NavigationDestination(
          icon: Icon(Icons.notifications_active_outlined),
          selectedIcon: Icon(Icons.notifications_active),
          label: 'Offers',
        ),
        NavigationDestination(icon: Icon(Icons.work_outline), selectedIcon: Icon(Icons.work), label: 'Jobs'),
        NavigationDestination(icon: Icon(Icons.payments_outlined), selectedIcon: Icon(Icons.payments), label: 'Pay'),
        NavigationDestination(icon: Icon(Icons.menu), label: 'More'),
      ],
    ),
  );
}

/// App bar with the brand and a bell, used by the tab screens.
PreferredSizeWidget providerBar(BuildContext context, {String? title}) => AppBar(
  title: title == null ? const BrandTitle(suffix: 'Work') : Text(title),
  actions: [
    IconButton(
      tooltip: 'Notifications',
      icon: const Icon(Icons.notifications_none),
      onPressed: () => context.go('/more/notifications'),
    ),
  ],
);
