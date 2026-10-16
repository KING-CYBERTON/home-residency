import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import 'screens/account_screen.dart';
import 'screens/addresses_screen.dart';
import 'screens/book_screen.dart';
import 'screens/bookings_screen.dart';
import 'screens/complaints_screen.dart';
import 'screens/home_screen.dart';
import 'screens/invoice_screen.dart';
import 'screens/pay_screen.dart';
import 'screens/plan_screen.dart';
import 'screens/request_screen.dart';

/// Privacy notice version households accept, the same as the client portal.
const privacyNoticeVersion = '2026-10';

class ClientApp extends StatefulWidget {
  const ClientApp({super.key, required this.session});

  final SessionController session;

  @override
  State<ClientApp> createState() => _ClientAppState();
}

class _ClientAppState extends State<ClientApp> {
  late final GoRouter _router = buildRouter(widget.session);

  @override
  void dispose() {
    _router.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => HomendScope(
    session: widget.session,
    child: MaterialApp.router(
      title: 'Homend',
      theme: homendTheme(),
      routerConfig: _router,
      debugShowCheckedModeBanner: false,
    ),
  );
}

GoRouter buildRouter(SessionController session) => GoRouter(
  refreshListenable: session,
  initialLocation: '/',
  redirect: (context, state) {
    final open = state.matchedLocation == '/sign-in';
    if (!session.isSignedIn) return open ? null : '/sign-in';
    return open ? '/' : null;
  },
  routes: [
    GoRoute(path: '/sign-in', builder: (_, _) => const ClientSignIn()),
    StatefulShellRoute.indexedStack(
      builder: (context, state, shell) => ClientShell(shell: shell),
      branches: [
        StatefulShellBranch(
          routes: [GoRoute(path: '/', builder: (_, _) => const HomeScreen())],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/bookings',
              builder: (_, _) => const BookingsScreen(),
              routes: [
                GoRoute(
                  path: ':id',
                  builder: (_, s) =>
                      RequestScreen(id: s.pathParameters['id']!, justBooked: s.uri.queryParameters['new'] == '1'),
                ),
              ],
            ),
          ],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/book',
              builder: (_, s) => BookScreen(categorySlug: s.uri.queryParameters['category']),
            ),
          ],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/pay',
              builder: (_, _) => const PayScreen(),
              routes: [
                GoRoute(
                  path: ':id',
                  builder: (_, s) => InvoiceScreen(id: s.pathParameters['id']!),
                ),
              ],
            ),
          ],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/account',
              builder: (_, _) => const AccountScreen(),
              routes: [
                GoRoute(path: 'addresses', builder: (_, _) => const AddressesScreen()),
                GoRoute(path: 'plan', builder: (_, _) => const PlanScreen()),
                GoRoute(
                  path: 'messages',
                  builder: (_, s) => ConversationsScreen(requestId: s.uri.queryParameters['request']),
                ),
                GoRoute(
                  path: 'complaints',
                  builder: (_, _) => const ComplaintsScreen(),
                  routes: [
                    GoRoute(
                      path: 'new',
                      builder: (_, s) => NewComplaintScreen(requestId: s.uri.queryParameters['request']),
                    ),
                    GoRoute(
                      path: ':id',
                      builder: (_, s) => ComplaintScreen(id: s.pathParameters['id']!),
                    ),
                  ],
                ),
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

/// Notifications carry ids in `data`; open the booking, invoice or case they are about.
void openNotification(BuildContext context, AppNotification n) {
  final d = n.data;
  if (d['request_id'] case final String id) {
    context.go('/bookings/$id');
  } else if (d['invoice_id'] case final String id) {
    context.go('/pay/$id');
  } else if (d['complaint_id'] case final String id) {
    context.go('/account/complaints/$id');
  } else if (d['conversation_id'] != null) {
    context.go('/account/messages');
  }
}

class ClientSignIn extends StatelessWidget {
  const ClientSignIn({super.key});

  @override
  Widget build(BuildContext context) => const SignInScreen(
    kind: 'client',
    noticeVersion: privacyNoticeVersion,
    title: 'Sign in',
    subtitle: 'Vetted, trained home help, managed by Homend.',
    registerTitle: 'Create your account',
    registerSubtitle: 'Book cleaning, laundry, childcare and more from vetted, trained providers.',
    registerConsents: [
      ConsentTerm(
        purpose: 'privacy_notice',
        text: 'I have read the privacy notice and agree to Homend Residence using my details to arrange services.',
      ),
    ],
  );
}

/// Bottom navigation like the portal: Home, Bookings, Book, Pay, Account.
class ClientShell extends StatelessWidget {
  const ClientShell({super.key, required this.shell});

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
          icon: Icon(Icons.calendar_today_outlined),
          selectedIcon: Icon(Icons.calendar_today),
          label: 'Bookings',
        ),
        NavigationDestination(icon: Icon(Icons.add), label: 'Book'),
        NavigationDestination(
          icon: Icon(Icons.credit_card_outlined),
          selectedIcon: Icon(Icons.credit_card),
          label: 'Pay',
        ),
        NavigationDestination(icon: Icon(Icons.person_outline), selectedIcon: Icon(Icons.person), label: 'Account'),
      ],
    ),
  );
}

/// App bar with the brand and a bell, as in the portal header.
PreferredSizeWidget clientBar(BuildContext context, {String? title}) => AppBar(
  title: title == null ? const BrandTitle() : Text(title),
  actions: [
    IconButton(
      tooltip: 'Notifications',
      icon: const Icon(Icons.notifications_none),
      onPressed: () => context.go('/account/notifications'),
    ),
  ],
);
