import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

/// The vetting checklist: upload documents, add referees, submit. The office does the rest.
class ApplicationScreen extends StatefulWidget {
  const ApplicationScreen({super.key, required this.id});

  final String id;

  @override
  State<ApplicationScreen> createState() => _ApplicationScreenState();
}

class _Data {
  _Data(this.app, this.docs, this.category);
  final Application app;
  final List<Document> docs;
  final String? category;
}

class _ApplicationScreenState extends State<ApplicationScreen> {
  final _view = GlobalKey<AsyncViewState<_Data>>();

  Future<_Data> _load() async {
    final api = context.api;
    final r = await Future.wait([api.application(widget.id), api.providerDocuments(), api.categories()]);
    final app = r[0] as Application;
    final cats = r[2] as List<Category>;
    return _Data(
      app,
      (r[1] as List<Document>).where((d) => d.status == 'uploaded' || d.status == 'verified').toList(),
      cats.where((c) => c.id == app.categoryId).map((c) => c.name).firstOrNull,
    );
  }

  Future<void> _reload() async => _view.currentState?.reload();

  Future<void> _upload(Application app, ApplicationItem item) async {
    DateTime? expires;
    if (item.templateItem.validityMonths != null) {
      expires = await showDatePicker(
        context: context,
        helpText: 'When does it expire? (skip if it does not)',
        firstDate: DateTime.now(),
        lastDate: DateTime.now().add(const Duration(days: 3650)),
      );
      if (!mounted) return;
    }
    final doc = await pickAndUploadDocument(
      context,
      documentType: item.templateItem.documentType ?? 'other',
      expiresOn: expires,
    );
    if (doc == null || !mounted) return;
    await context.api.attachDocument(app.id, itemId: item.id, documentId: doc.id);
    if (!mounted) return;
    showMessage(context, 'Uploaded.');
    await _reload();
  }

  Future<void> _attachExisting(Application app, ApplicationItem item, List<Document> docs) async {
    final doc = await showModalBottomSheet<Document>(
      context: context,
      builder: (c) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          children: [
            const Padding(
              padding: EdgeInsets.all(16),
              child: Text('Use one I already uploaded', style: TextStyle(fontWeight: FontWeight.w600)),
            ),
            for (final d in docs)
              ListTile(
                title: Text(labelOf(d.documentType)),
                subtitle: Text([d.originalFilename, day(d.createdAt)].whereType<String>().join(' · ')),
                onTap: () => Navigator.pop(c, d),
              ),
          ],
        ),
      ),
    );
    if (doc == null || !mounted) return;
    await context.api.attachDocument(app.id, itemId: item.id, documentId: doc.id);
    await _reload();
  }

  Future<void> _submit(Application app) async {
    try {
      await context.api.submitApplication(app.id);
      if (!mounted) return;
      showMessage(context, 'Submitted. We will SMS you when we need you, for example to book your interview.');
    } on ApiException catch (e) {
      if (!mounted) return;
      final missing = (e.details['missing'] as List?)?.join(', ');
      showMessage(context, missing == null ? e.message : 'Still needed: $missing', error: true);
    }
    await _reload();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Application')),
    body: AsyncView<_Data>(
      key: _view,
      load: _load,
      builder: (context, d, _) {
        final app = d.app;
        final editable = app.isEditable;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            PageTitle(d.category ?? 'Application', subtitle: app.reference, trailing: StatusBadge(app.status)),
            if (app.decisionReason != null) ...[const SizedBox(height: 12), Notice(app.decisionReason!, warning: true)],
            if (app.isUnderReview) ...[
              const SizedBox(height: 12),
              const Notice(
                'We are checking your application. We will SMS you when we need you, for example to book your interview.',
              ),
            ],
            if (editable && app.missing.isNotEmpty) ...[
              const SizedBox(height: 12),
              Notice(
                app.missing.map((m) => '•  $m').join('\n'),
                title: 'Still needed before you can submit',
                warning: true,
              ),
            ],
            const SectionTitle('Checklist'),
            ...spaced(
              app.items.map(
                (item) => _ItemCard(
                  item: item,
                  editable: editable,
                  onUpload: () => _upload(app, item),
                  onUseExisting: d.docs.isEmpty ? null : () => _attachExisting(app, item, d.docs),
                ),
              ),
            ),
            const SectionTitle('Referees'),
            if (app.referees.isEmpty) const EmptyState('No referees yet.'),
            ...spaced(
              app.referees.map(
                (r) => Panel(
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(r.fullName, style: const TextStyle(fontWeight: FontWeight.w600)),
                            Text(
                              [r.relationship, r.employerName].whereType<String>().join(' · '),
                              style: const TextStyle(color: Brand.slate500),
                            ),
                          ],
                        ),
                      ),
                      if (editable)
                        IconButton(
                          tooltip: 'Remove',
                          icon: const Icon(Icons.delete_outline),
                          onPressed: () async {
                            if (!await confirm(context, 'Remove this referee?', yes: 'Remove')) return;
                            if (!context.mounted) return;
                            await context.api.removeReferee(app.id, r.id);
                            await _reload();
                          },
                        )
                      else
                        StatusBadge(r.outcome),
                    ],
                  ),
                ),
              ),
            ),
            if (editable) ...[
              const SizedBox(height: 12),
              OutlinedButton.icon(
                icon: const Icon(Icons.person_add_alt),
                label: const Text('Add a referee'),
                onPressed: () async {
                  final added = await showModalBottomSheet<bool>(
                    context: context,
                    isScrollControlled: true,
                    builder: (_) => _RefereeSheet(applicationId: app.id),
                  );
                  if (added == true) await _reload();
                },
              ),
              const SizedBox(height: 24),
              BusyButton(label: 'Submit application', onPressed: () => _submit(app)),
            ],
            if (app.canWithdraw) ...[
              const SizedBox(height: 12),
              BusyButton(
                label: 'Withdraw application',
                outlined: true,
                danger: true,
                onPressed: () async {
                  if (!await confirm(
                    context,
                    'Withdraw this application? You can apply again later.',
                    yes: 'Withdraw',
                    danger: true,
                  )) {
                    return;
                  }
                  if (!context.mounted) return;
                  await context.api.withdrawApplication(app.id);
                  await _reload();
                },
              ),
            ],
          ],
        );
      },
    ),
  );
}

class _ItemCard extends StatelessWidget {
  const _ItemCard({required this.item, required this.editable, required this.onUpload, this.onUseExisting});

  final ApplicationItem item;
  final bool editable;
  final Future<void> Function() onUpload;
  final Future<void> Function()? onUseExisting;

  @override
  Widget build(BuildContext context) {
    final t = item.templateItem;
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text.rich(
                  TextSpan(
                    children: [
                      TextSpan(
                        text: t.label,
                        style: const TextStyle(fontWeight: FontWeight.w600),
                      ),
                      if (!t.isMandatory)
                        const TextSpan(
                          text: '  (optional)',
                          style: TextStyle(color: Brand.slate500),
                        ),
                    ],
                  ),
                ),
              ),
              StatusBadge(item.status),
            ],
          ),
          if (item.notes != null) Padding(padding: const EdgeInsets.only(top: 4), child: Text(item.notes!)),
          const SizedBox(height: 8),
          if (t.takesUpload) ...[
            if (editable) ...[
              BusyButton(
                label: item.documentId == null ? 'Upload a photo or PDF' : 'Replace with a new photo or PDF',
                outlined: true,
                onPressed: onUpload,
              ),
              if (onUseExisting != null)
                TextButton(onPressed: onUseExisting, child: const Text('Use one I already uploaded')),
            ],
          ] else if (t.kind == 'referee')
            Text(
              'At least ${t.minCount ?? 1} people who have employed you or know your work. We will call them.',
              style: const TextStyle(color: Brand.slate600),
            )
          else
            const Text('We will arrange this with you after you submit.', style: TextStyle(color: Brand.slate600)),
        ],
      ),
    );
  }
}

class _RefereeSheet extends StatefulWidget {
  const _RefereeSheet({required this.applicationId});

  final String applicationId;

  @override
  State<_RefereeSheet> createState() => _RefereeSheetState();
}

class _RefereeSheetState extends State<_RefereeSheet> {
  final _form = GlobalKey<FormState>();
  final _name = TextEditingController();
  final _phone = TextEditingController();
  final _rel = TextEditingController();
  final _emp = TextEditingController();

  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + MediaQuery.viewInsetsOf(context).bottom),
    child: Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Text('Add a referee', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
          const SizedBox(height: 12),
          TextFormField(
            controller: _name,
            textCapitalization: TextCapitalization.words,
            decoration: const InputDecoration(labelText: 'Full name'),
            validator: (v) => (v ?? '').trim().isEmpty ? 'Required' : null,
          ),
          const SizedBox(height: 12),
          TextFormField(
            controller: _phone,
            keyboardType: TextInputType.phone,
            decoration: const InputDecoration(labelText: 'Phone'),
            validator: (v) => normalizeKenyanPhone(v ?? '') == null ? 'Enter a phone number' : null,
          ),
          const SizedBox(height: 12),
          TextFormField(
            controller: _rel,
            decoration: const InputDecoration(labelText: 'How they know you', hintText: 'Former employer'),
            validator: (v) => (v ?? '').trim().isEmpty ? 'Required' : null,
          ),
          const SizedBox(height: 12),
          TextFormField(
            controller: _emp,
            decoration: const InputDecoration(labelText: 'Employer or household (optional)'),
          ),
          const SizedBox(height: 16),
          BusyButton(
            label: 'Add referee',
            onPressed: () async {
              if (!_form.currentState!.validate()) return;
              await context.api.addReferee(
                widget.applicationId,
                RefereeInput(
                  fullName: _name.text.trim(),
                  relationship: _rel.text.trim(),
                  phone: normalizeKenyanPhone(_phone.text)!,
                  employerName: _emp.text.trim().isEmpty ? null : _emp.text.trim(),
                ),
              );
              if (context.mounted) Navigator.pop(context, true);
            },
          ),
        ],
      ),
    ),
  );
}
