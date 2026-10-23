import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';

class AccountScreen extends StatelessWidget {
  const AccountScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: clientBar(context, title: 'Account'),
    body: AsyncView<(Me, Subscription?)>(
      load: () async {
        final session = context.session;
        final api = context.api;
        final r = await Future.wait([session.refreshMe(), api.subscription()]);
        return (r[0] as Me, r[1] as Subscription?);
      },
      builder: (context, d, reload) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          MenuGroup(
            items: [
              MenuItem('Addresses', () => context.go('/account/addresses')),
              MenuItem(
                'Household plan',
                () => context.go('/account/plan'),
                value: d.$2 == null || d.$2!.status != 'active' ? 'None' : d.$2!.plan.name,
              ),
              MenuItem('Messages with Homend', () => context.go('/account/messages')),
              MenuItem('Complaints', () => context.go('/account/complaints')),
              MenuItem('Notifications', () => context.go('/account/notifications')),
              MenuItem(
                'Email and password',
                () => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => const PasswordScreen())),
                value: d.$1.hasPassword ? 'Set' : 'Not set',
              ),
            ],
          ),
          const SectionTitle('Your details'),
          _DetailsForm(me: d.$1),
          const SizedBox(height: 8),
          Row(
            children: [
              const Expanded(
                child: Text('ID verification', style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
              ),
              StatusBadge(d.$1.idVerification ?? 'none'),
            ],
          ),
          const SizedBox(height: 8),
          _IdVerification(me: d.$1, onSubmitted: reload),
          const SizedBox(height: 32),
          BusyButton(
            label: 'Sign out',
            outlined: true,
            onPressed: () async {
              if (await confirm(context, 'Sign out of Homend on this phone?', yes: 'Sign out') && context.mounted) {
                await context.session.signOut();
              }
            },
          ),
        ],
      ),
    ),
  );
}

class _DetailsForm extends StatefulWidget {
  const _DetailsForm({required this.me});

  final Me me;

  @override
  State<_DetailsForm> createState() => _DetailsFormState();
}

class _DetailsFormState extends State<_DetailsForm> {
  late final _name = TextEditingController(text: widget.me.fullName);
  late final _email = TextEditingController(text: widget.me.email);
  late String _lang = widget.me.preferredLanguage;

  @override
  Widget build(BuildContext context) => Panel(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextField(
          controller: _name,
          decoration: const InputDecoration(labelText: 'Name'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: TextEditingController(text: displayPhone(widget.me.phone)),
          enabled: false,
          decoration: const InputDecoration(labelText: 'Phone'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _email,
          keyboardType: TextInputType.emailAddress,
          decoration: const InputDecoration(labelText: 'Email (for receipts, optional)'),
        ),
        const SizedBox(height: 12),
        DropdownButtonFormField<String>(
          isExpanded: true,
          initialValue: _lang,
          decoration: const InputDecoration(labelText: 'Language'),
          items: const [
            DropdownMenuItem(value: 'en', child: Text('English')),
            DropdownMenuItem(value: 'sw', child: Text('Kiswahili')),
          ],
          onChanged: (v) => setState(() => _lang = v ?? 'en'),
        ),
        const SizedBox(height: 16),
        BusyButton(
          label: 'Save',
          onPressed: () async {
            final session = context.session;
            final me = await context.api.updateMe(
              fullName: _name.text.trim(),
              email: _email.text.trim().isEmpty ? null : _email.text.trim(),
              preferredLanguage: _lang,
            );
            session.replaceMe(me);
            if (context.mounted) showMessage(context, 'Saved.');
          },
        ),
      ],
    ),
  );
}

class _IdVerification extends StatefulWidget {
  const _IdVerification({required this.me, required this.onSubmitted});

  final Me me;
  final Future<void> Function() onSubmitted;

  @override
  State<_IdVerification> createState() => _IdVerificationState();
}

class _IdVerificationState extends State<_IdVerification> {
  final _number = TextEditingController();
  Document? _photo;

  @override
  Widget build(BuildContext context) {
    final status = widget.me.idVerification ?? 'none';
    if (status == 'verified') return const Notice('Your ID is verified.');
    if (status == 'pending') return const Notice('We are checking your ID. This usually takes one working day.');
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            status == 'rejected'
                ? 'We could not verify your ID. Please send it again.'
                : 'Needed before booking childcare or caregiving, so providers know who they are working for.',
            style: const TextStyle(color: Brand.slate600),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _number,
            decoration: const InputDecoration(labelText: 'National ID or passport number'),
          ),
          const SizedBox(height: 12),
          BusyButton(
            label: _photo == null ? 'Add a photo of the ID' : 'Photo added. Replace it',
            outlined: true,
            onPressed: () async {
              final doc = await pickAndUploadDocument(context, documentType: 'national_id');
              if (doc != null && mounted) setState(() => _photo = doc);
            },
          ),
          const SizedBox(height: 12),
          BusyButton(
            label: 'Send for verification',
            onPressed: () async {
              if (_number.text.trim().length < 5 || _photo == null) {
                showMessage(context, 'Add your ID number and a photo of the ID.', error: true);
                return;
              }
              await context.api.submitIdVerification(nationalIdNumber: _number.text.trim(), documentId: _photo!.id);
              await widget.onSubmitted();
            },
          ),
        ],
      ),
    );
  }
}
