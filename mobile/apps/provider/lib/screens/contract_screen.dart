import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';
import 'package:url_launcher/url_launcher.dart';

/// Contract and code of conduct, signed with an SMS code to the provider's own phone.
class ContractScreen extends StatefulWidget {
  const ContractScreen({super.key});

  @override
  State<ContractScreen> createState() => _ContractScreenState();
}

class _ContractScreenState extends State<ContractScreen> {
  final _view = GlobalKey<AsyncViewState<Contract>>();
  OtpSent? _sent;
  bool _agreed = false;
  final _code = TextEditingController();

  Future<void> _sendCode() async {
    final phone = context.session.me?.phone;
    if (phone == null) {
      showMessage(context, 'We could not find your phone number. Please sign in again.', error: true);
      return;
    }
    final sent = await context.api.requestOtp(phone, purpose: 'verify_phone');
    if (mounted) setState(() => _sent = sent);
  }

  Future<void> _sign(Contract c) async {
    await context.api.signContract(c, otpId: _sent!.otpId, code: _code.text.trim());
    if (!mounted) return;
    showMessage(context, 'Signed. Thank you.');
    setState(() => _sent = null);
    await _view.currentState?.reload();
  }

  @override
  Widget build(BuildContext context) {
    final status = HomendScope.of(context).me?.providerStatus;
    return Scaffold(
      appBar: AppBar(title: const Text('Contract')),
      body: AsyncView<Contract>(
        key: _view,
        load: () => context.api.contract(),
        builder: (context, c, _) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Panel(
              child: Column(
                children: [
                  KeyValue('Contract', 'Version ${c.contractVersion}'),
                  KeyValue('Code of conduct', 'Version ${c.codeOfConductVersion}'),
                  if (c.engagementBasis != null) KeyValue('Basis', labelOf(c.engagementBasis!)),
                  KeyValue('Signed', c.signedAt == null ? 'Not yet' : date(c.signedAt!)),
                ],
              ),
            ),
            if (c.documentUrl != null) ...[
              const SizedBox(height: 8),
              TextButton.icon(
                icon: const Icon(Icons.description_outlined),
                label: const Text('Read the contract'),
                onPressed: () => launchUrl(Uri.parse(c.documentUrl!), mode: LaunchMode.externalApplication),
              ),
            ],
            const SizedBox(height: 16),
            if (c.signedAt != null)
              const Notice('You have signed the current contract and code of conduct.')
            else if (status != 'approved' && status != 'active')
              const Notice('You can sign once your application is approved.')
            else if (_sent == null) ...[
              CheckboxListTile(
                value: _agreed,
                onChanged: (v) => setState(() => _agreed = v ?? false),
                controlAffinity: ListTileControlAffinity.leading,
                contentPadding: EdgeInsets.zero,
                title: const Text('I have read and agree to the contract and the code of conduct.'),
              ),
              const SizedBox(height: 8),
              BusyButton(label: 'Sign with an SMS code', onPressed: _agreed ? _sendCode : null),
            ] else ...[
              const Text('Enter the code we sent to your phone to sign.'),
              const SizedBox(height: 12),
              TextField(
                controller: _code,
                keyboardType: TextInputType.number,
                autofillHints: const [AutofillHints.oneTimeCode],
                decoration: const InputDecoration(labelText: 'Code'),
              ),
              const SizedBox(height: 12),
              BusyButton(label: 'Sign', onPressed: () => _sign(c)),
            ],
          ],
        ),
      ),
    );
  }
}
