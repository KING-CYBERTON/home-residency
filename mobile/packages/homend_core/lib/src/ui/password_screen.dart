import 'package:flutter/material.dart';

import 'theme.dart';
import 'widgets.dart';

/// Sets or changes the email and password the person signs in with, from either app.
class PasswordScreen extends StatefulWidget {
  const PasswordScreen({super.key});

  @override
  State<PasswordScreen> createState() => _PasswordScreenState();
}

class _PasswordScreenState extends State<PasswordScreen> {
  final _form = GlobalKey<FormState>();
  late final _email = TextEditingController(text: context.session.me?.email);
  final _current = TextEditingController();
  final _password = TextEditingController();

  @override
  void dispose() {
    _email.dispose();
    _current.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    final api = context.api;
    final session = context.session;
    final email = _email.text.trim();
    if (email.toLowerCase() != (session.me?.email ?? '').toLowerCase()) {
      await api.updateMe(email: email);
    }
    await api.setPassword(_password.text, currentPassword: session.me?.hasPassword == true ? _current.text : null);
    await session.refreshMe();
    if (!mounted) return;
    showMessage(context, 'Password saved. You can sign in with your email and password.');
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final hasPassword = context.session.me?.hasPassword == true;
    return Scaffold(
      appBar: AppBar(title: const Text('Email and password')),
      body: SafeArea(
        child: Form(
          key: _form,
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              Text(
                hasPassword
                    ? 'Change the password you sign in with.'
                    : 'Set a password so you can sign in with your email, without waiting for an SMS code.',
                style: const TextStyle(color: Brand.slate500),
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _email,
                keyboardType: TextInputType.emailAddress,
                autofillHints: const [AutofillHints.email],
                decoration: const InputDecoration(labelText: 'Email'),
                validator: validEmail,
              ),
              if (hasPassword) ...[
                const SizedBox(height: 16),
                TextFormField(
                  controller: _current,
                  obscureText: true,
                  autofillHints: const [AutofillHints.password],
                  decoration: const InputDecoration(labelText: 'Current password'),
                  validator: (v) => (v ?? '').isEmpty ? 'Enter your current password' : null,
                ),
              ],
              const SizedBox(height: 16),
              TextFormField(
                controller: _password,
                obscureText: true,
                autofillHints: const [AutofillHints.newPassword],
                decoration: const InputDecoration(labelText: 'New password', helperText: 'At least 8 characters'),
                validator: validNewPassword,
              ),
              const SizedBox(height: 24),
              BusyButton(label: 'Save password', onPressed: _save),
            ],
          ),
        ),
      ),
    );
  }
}

String? validEmail(String? v) {
  final s = (v ?? '').trim();
  return RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch(s) ? null : 'Enter your email address';
}

String? validNewPassword(String? v) {
  final n = (v ?? '').length;
  return n < 8 || n > 128 ? 'Use 8 to 128 characters' : null;
}

/// Asks for the email and sends a reset link. The link opens the web portal to choose a new password.
Future<void> showForgotPassword(BuildContext context, {String? email}) async {
  final controller = TextEditingController(text: email);
  final form = GlobalKey<FormState>();
  final api = context.api;
  final sent = await showDialog<bool>(
    context: context,
    builder: (dialog) => AlertDialog(
      title: const Text('Forgotten password'),
      content: Form(
        key: form,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('We will email you a link to choose a new password.'),
            const SizedBox(height: 12),
            TextFormField(
              controller: controller,
              autofocus: true,
              keyboardType: TextInputType.emailAddress,
              decoration: const InputDecoration(labelText: 'Email'),
              validator: validEmail,
            ),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(dialog).pop(false), child: const Text('Cancel')),
        TextButton(
          onPressed: () async {
            if (!form.currentState!.validate()) return;
            try {
              await api.forgotPassword(controller.text.trim());
              if (dialog.mounted) Navigator.of(dialog).pop(true);
            } catch (e) {
              if (dialog.mounted) showMessage(dialog, messageOf(e), error: true);
            }
          },
          child: const Text('Send link'),
        ),
      ],
    ),
  );
  controller.dispose();
  if (sent == true && context.mounted) {
    showMessage(context, 'If that email has an account, we have sent it a link. It works for one hour.');
  }
}
