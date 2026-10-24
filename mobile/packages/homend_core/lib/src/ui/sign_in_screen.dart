import 'dart:async';

import 'package:flutter/material.dart';

import '../models/identity.dart';
import 'format.dart';
import 'password_screen.dart';
import 'theme.dart';
import 'widgets.dart';

/// One consent the person must tick to register (Data Protection Act 2019).
class ConsentTerm {
  const ConsentTerm({required this.purpose, required this.text});

  final String purpose;
  final String text;
}

/// Sign-in and registration shared by both apps: email and password, or phone and an SMS code.
/// Email comes first because it works whether or not SMS codes can be sent.
class SignInScreen extends StatefulWidget {
  const SignInScreen({
    super.key,
    required this.kind,
    required this.noticeVersion,
    required this.registerConsents,
    this.title = 'Sign in',
    this.subtitle,
    this.registerTitle = 'Create your account',
    this.registerSubtitle,
    this.registerLabel = 'New here? Create an account',
    this.startInRegister = false,
  });

  /// `client` or `provider`.
  final String kind;
  final String noticeVersion;
  final List<ConsentTerm> registerConsents;
  final String title;
  final String? subtitle;
  final String registerTitle;
  final String? registerSubtitle;
  final String registerLabel;
  final bool startInRegister;

  @override
  State<SignInScreen> createState() => _SignInScreenState();
}

class _SignInScreenState extends State<SignInScreen> {
  late bool _register = widget.startInRegister;
  bool _usePhone = false;
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _phone = TextEditingController();
  final _name = TextEditingController();
  final _code = TextEditingController();
  final _form = GlobalKey<FormState>();
  late final Map<String, bool> _ticked = {for (final c in widget.registerConsents) c.purpose: false};

  OtpSent? _sent;
  String? _e164;
  int _resendIn = 0;
  Timer? _timer;

  @override
  void dispose() {
    _timer?.cancel();
    _email.dispose();
    _password.dispose();
    _phone.dispose();
    _name.dispose();
    _code.dispose();
    super.dispose();
  }

  bool _consentsTicked() {
    if (_register && _ticked.values.any((v) => !v)) {
      showMessage(context, 'Please tick the boxes to continue.', error: true);
      return false;
    }
    return true;
  }

  Future<void> _withPassword() async {
    if (!_form.currentState!.validate() || !_consentsTicked()) return;
    final session = context.session;
    final api = context.api;
    final email = _email.text.trim();
    final s = _register
        ? await api.register(
            kind: widget.kind,
            fullName: _name.text.trim(),
            email: email,
            password: _password.text,
            phone: _askPhone ? normalizeKenyanPhone(_phone.text) : null,
            consents: [for (final c in widget.registerConsents) Consent(c.purpose, widget.noticeVersion)],
          )
        : await api.login(email, _password.text);
    await session.signedIn(s);
  }

  Future<void> _sendCode() async {
    final resend = _sent != null;
    if (!resend) {
      if (!_form.currentState!.validate() || !_consentsTicked()) return;
    }
    final phone = resend ? _e164! : normalizeKenyanPhone(_phone.text)!;
    final sent = await context.api.requestOtp(phone, purpose: _register ? 'register' : 'login');
    if (!mounted) return;
    setState(() {
      _sent = sent;
      _e164 = phone;
      _code.clear();
    });
    _startCountdown(sent.resendAfterSeconds);
  }

  void _startCountdown(int seconds) {
    _timer?.cancel();
    setState(() => _resendIn = seconds);
    _timer = Timer.periodic(const Duration(seconds: 1), (t) {
      if (!mounted || _resendIn <= 1) {
        t.cancel();
        if (mounted) setState(() => _resendIn = 0);
        return;
      }
      setState(() => _resendIn--);
    });
  }

  Future<void> _verify() async {
    final code = _code.text.trim();
    if (code.length < 4) {
      showMessage(context, 'Enter the code from the SMS.', error: true);
      return;
    }
    final session = context.session;
    final s = await context.api.verifyOtp(
      otpId: _sent!.otpId,
      code: code,
      fullName: _register ? _name.text.trim() : null,
      kind: _register ? widget.kind : null,
      consents: _register
          ? [for (final c in widget.registerConsents) Consent(c.purpose, widget.noticeVersion)]
          : const [],
    );
    await session.signedIn(s);
  }

  @override
  Widget build(BuildContext context) {
    final title = _register ? widget.registerTitle : widget.title;
    final subtitle = _register ? widget.registerSubtitle : widget.subtitle;
    return Scaffold(
      appBar: AppBar(title: const BrandTitle()),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            PageTitle(title, subtitle: subtitle),
            const SizedBox(height: 24),
            if (!_usePhone) _emailStep() else if (_sent == null) _phoneStep() else _codeStep(),
          ],
        ),
      ),
    );
  }

  Widget _nameField() => TextFormField(
    controller: _name,
    textCapitalization: TextCapitalization.words,
    autofillHints: const [AutofillHints.name],
    decoration: const InputDecoration(labelText: 'Your full name'),
    validator: (v) => (v ?? '').trim().length < 2 ? 'Enter your name' : null,
  );

  List<Widget> _consents() => [
    if (_register)
      for (final c in widget.registerConsents)
        CheckboxListTile(
          value: _ticked[c.purpose],
          onChanged: (v) => setState(() => _ticked[c.purpose] = v ?? false),
          controlAffinity: ListTileControlAffinity.leading,
          contentPadding: EdgeInsets.zero,
          title: Text(c.text, style: const TextStyle(fontSize: 14)),
        ),
  ];

  Widget _switchMethod() => TextButton(
    onPressed: () => setState(() {
      _usePhone = !_usePhone;
      _sent = null;
    }),
    child: Text(_usePhone ? 'Use email and password instead' : 'Use my phone number instead'),
  );

  /// Providers give a phone with email sign-up too: the vetting team calls them, and one phone
  /// belongs to one account.
  bool get _askPhone => _register && widget.kind == 'provider';

  Widget _emailStep() => Form(
    key: _form,
    child: AutofillGroup(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (_register) ...[_nameField(), const SizedBox(height: 16)],
          TextFormField(
            controller: _email,
            keyboardType: TextInputType.emailAddress,
            autofillHints: const [AutofillHints.email],
            decoration: const InputDecoration(labelText: 'Email'),
            validator: validEmail,
          ),
          if (_askPhone) ...[
            const SizedBox(height: 16),
            TextFormField(
              controller: _phone,
              keyboardType: TextInputType.phone,
              autofillHints: const [AutofillHints.telephoneNumber],
              decoration: const InputDecoration(
                labelText: 'Phone number',
                hintText: '0712 345 678',
                helperText: 'The vetting team calls you on this number.',
              ),
              validator: (v) =>
                  normalizeKenyanPhone(v ?? '') == null ? 'Enter a Kenyan mobile number, e.g. 0712 345 678' : null,
            ),
          ],
          const SizedBox(height: 16),
          TextFormField(
            controller: _password,
            obscureText: true,
            autofillHints: [_register ? AutofillHints.newPassword : AutofillHints.password],
            decoration: InputDecoration(labelText: 'Password', helperText: _register ? 'At least 8 characters' : null),
            validator: _register ? validNewPassword : (v) => (v ?? '').isEmpty ? 'Enter your password' : null,
          ),
          ..._consents(),
          const SizedBox(height: 20),
          BusyButton(label: _register ? 'Create account' : 'Sign in', onPressed: _withPassword),
          if (!_register)
            TextButton(
              onPressed: () => showForgotPassword(context, email: _email.text.trim()),
              child: const Text('Forgotten your password?'),
            ),
          _switchMethod(),
          TextButton(
            onPressed: () => setState(() => _register = !_register),
            child: Text(_register ? 'I already have an account' : widget.registerLabel),
          ),
        ],
      ),
    ),
  );

  Widget _phoneStep() => Form(
    key: _form,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (_register) ...[_nameField(), const SizedBox(height: 16)],
        TextFormField(
          controller: _phone,
          keyboardType: TextInputType.phone,
          autofillHints: const [AutofillHints.telephoneNumber],
          decoration: const InputDecoration(labelText: 'Phone number', hintText: '0712 345 678'),
          validator: (v) =>
              normalizeKenyanPhone(v ?? '') == null ? 'Enter a Kenyan mobile number, e.g. 0712 345 678' : null,
        ),
        const SizedBox(height: 8),
        const Text('We will send you a code by SMS.', style: TextStyle(color: Brand.slate500)),
        ..._consents(),
        const SizedBox(height: 20),
        BusyButton(label: 'Send code', onPressed: _sendCode),
        const SizedBox(height: 12),
        _switchMethod(),
        TextButton(
          onPressed: () => setState(() => _register = !_register),
          child: Text(_register ? 'I already have an account' : widget.registerLabel),
        ),
      ],
    ),
  );

  Widget _codeStep() => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Text('Enter the code we sent to ${displayPhone(_e164)}.'),
      const SizedBox(height: 16),
      TextField(
        controller: _code,
        autofocus: true,
        keyboardType: TextInputType.number,
        autofillHints: const [AutofillHints.oneTimeCode],
        maxLength: 8,
        style: const TextStyle(fontSize: 24, letterSpacing: 8),
        decoration: const InputDecoration(labelText: 'Code', counterText: ''),
        onSubmitted: (_) => _verify(),
      ),
      const SizedBox(height: 20),
      BusyButton(label: _register ? 'Create account' : 'Sign in', onPressed: _verify),
      const SizedBox(height: 12),
      TextButton(
        onPressed: _resendIn > 0
            ? null
            : () async {
                try {
                  await _sendCode();
                } catch (e) {
                  if (mounted) showMessage(context, messageOf(e), error: true);
                }
              },
        child: Text(_resendIn > 0 ? 'Send a new code in ${_resendIn}s' : 'Send a new code'),
      ),
      TextButton(onPressed: () => setState(() => _sent = null), child: const Text('Use a different number')),
      if (!_register)
        const Padding(
          padding: EdgeInsets.only(top: 8),
          child: Text(
            'If the code is not accepted and you have never signed up, go back and create an account.',
            textAlign: TextAlign.center,
            style: TextStyle(color: Brand.slate500, fontSize: 13),
          ),
        ),
    ],
  );
}
