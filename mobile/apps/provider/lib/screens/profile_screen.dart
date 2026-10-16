import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

class ProfileScreen extends StatelessWidget {
  const ProfileScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('My profile')),
    body: AsyncView<(ProviderProfile, List<ServiceArea>)>(
      load: () async {
        final api = context.api;
        final r = await Future.wait([api.providerProfile(), api.serviceAreas()]);
        return (r[0] as ProviderProfile, r[1] as List<ServiceArea>);
      },
      builder: (context, d, reload) => _ProfileForm(profile: d.$1, areas: d.$2, saved: reload),
    ),
  );
}

class _ProfileForm extends StatefulWidget {
  const _ProfileForm({required this.profile, required this.areas, required this.saved});

  final ProviderProfile profile;
  final List<ServiceArea> areas;
  final Future<void> Function() saved;

  @override
  State<_ProfileForm> createState() => _ProfileFormState();
}

class _ProfileFormState extends State<_ProfileForm> {
  late final _msisdn = TextEditingController(text: displayPhone(widget.profile.payoutMsisdn));
  late final _bio = TextEditingController(text: widget.profile.bio);
  late final _years = TextEditingController(text: widget.profile.yearsExperience?.toString());
  late final _languages = TextEditingController(text: widget.profile.languages.join(', '));
  late String? _home = widget.profile.homeServiceAreaId;
  late final Set<String> _areas = {...widget.profile.serviceAreaIds};

  Future<void> _save() async {
    String? msisdn;
    if (_msisdn.text.trim().isNotEmpty) {
      msisdn = normalizeKenyanPhone(_msisdn.text);
      if (msisdn == null) {
        showMessage(context, 'Enter your M-Pesa number, e.g. 0712 345 678.', error: true);
        return;
      }
    }
    await context.api.updateProviderProfile(
      ProviderProfileUpdate(
        payoutMsisdn: msisdn,
        bio: _bio.text.trim(),
        yearsExperience: int.tryParse(_years.text.trim()),
        languages: _languages.text.split(',').map((s) => s.trim()).where((s) => s.isNotEmpty).toList(),
        homeServiceAreaId: _home,
        serviceAreaIds: _areas.toList(),
      ),
    );
    if (!mounted) return;
    showMessage(context, 'Saved.');
    await widget.saved();
  }

  @override
  Widget build(BuildContext context) {
    final p = widget.profile;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        PageTitle(p.fullName, trailing: StatusBadge(p.status)),
        const SizedBox(height: 12),
        Panel(
          child: Column(
            children: [
              KeyValue(
                'Rating',
                p.ratingAvg == null ? 'No ratings yet' : '★ ${p.ratingAvg!.toStringAsFixed(1)} (${p.ratingCount})',
              ),
              KeyValue('Jobs done', '${p.completedJobs}'),
              if (p.appraisalScore != null) KeyValue('Score', p.appraisalScore!.toStringAsFixed(0)),
            ],
          ),
        ),
        const SectionTitle('Getting paid'),
        TextField(
          controller: _msisdn,
          keyboardType: TextInputType.phone,
          decoration: const InputDecoration(labelText: 'M-Pesa number for payouts'),
        ),
        const SectionTitle('About you'),
        const Text(
          'Clients see this when the office offers them a choice of providers.',
          style: TextStyle(color: Brand.slate500),
        ),
        const SizedBox(height: 8),
        TextField(
          controller: _bio,
          minLines: 3,
          maxLines: 5,
          maxLength: 500,
          decoration: const InputDecoration(labelText: 'Short bio'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _years,
          keyboardType: TextInputType.number,
          decoration: const InputDecoration(labelText: 'Years of experience'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _languages,
          decoration: const InputDecoration(labelText: 'Languages', hintText: 'English, Kiswahili'),
        ),
        const SectionTitle('Where you work'),
        DropdownButtonFormField<String>(
          isExpanded: true,
          initialValue: widget.areas.any((a) => a.id == _home) ? _home : null,
          decoration: const InputDecoration(labelText: 'Where you live'),
          items: [for (final a in widget.areas) DropdownMenuItem(value: a.id, child: Text(a.name))],
          onChanged: (v) => setState(() => _home = v),
        ),
        const SizedBox(height: 12),
        const Text('Areas you can work in'),
        const SizedBox(height: 4),
        Wrap(
          spacing: 8,
          runSpacing: 4,
          children: [
            for (final a in widget.areas)
              FilterChip(
                label: Text(a.name),
                selected: _areas.contains(a.id),
                onSelected: (on) => setState(() => on ? _areas.add(a.id) : _areas.remove(a.id)),
              ),
          ],
        ),
        const SizedBox(height: 24),
        BusyButton(label: 'Save', onPressed: _save),
      ],
    );
  }
}
