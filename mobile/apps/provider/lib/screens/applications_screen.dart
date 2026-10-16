import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

/// Applications to do a kind of work (cleaning, childcare, plumbing…), one per category.
class ApplicationsScreen extends StatelessWidget {
  const ApplicationsScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('My applications')),
    body: AsyncView<(List<Application>, List<Category>)>(
      load: () async {
        final api = context.api;
        final r = await Future.wait([api.applications(), api.categories()]);
        return (r[0] as List<Application>, r[1] as List<Category>);
      },
      builder: (context, d, _) {
        final names = {for (final c in d.$2) c.id: c.name};
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (d.$1.isEmpty) const EmptyState('You have not applied yet.'),
            ...spaced(
              d.$1.map(
                (a) => Panel(
                  onTap: () => context.go('/more/applications/${a.id}'),
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              names[a.categoryId] ?? 'Application',
                              style: const TextStyle(fontWeight: FontWeight.w600),
                            ),
                            Text(
                              [a.reference, if (a.total > 0) '${a.satisfied} of ${a.total} done'].join(' · '),
                              style: const TextStyle(color: Brand.slate500),
                            ),
                          ],
                        ),
                      ),
                      StatusBadge(a.status),
                    ],
                  ),
                ),
              ),
            ),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => _start(context, d.$2, d.$1),
              child: Text(d.$1.isEmpty ? 'Start my application' : 'Apply for other work'),
            ),
          ],
        );
      },
    ),
  );

  Future<void> _start(BuildContext context, List<Category> cats, List<Application> existing) async {
    final open = existing.where((a) => !{'rejected', 'withdrawn'}.contains(a.status)).map((a) => a.categoryId).toSet();
    final choices = cats.where((c) => !open.contains(c.id)).toList();
    if (choices.isEmpty) {
      showMessage(context, 'You already have an application for every kind of work.');
      return;
    }
    final cat = await showModalBottomSheet<Category>(
      context: context,
      builder: (c) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          children: [
            const Padding(
              padding: EdgeInsets.all(16),
              child: Text('What work do you want to do?', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
            ),
            for (final cat in choices)
              ListTile(
                title: Text(cat.name),
                subtitle: cat.description == null ? null : Text(cat.description!),
                onTap: () => Navigator.pop(c, cat),
              ),
          ],
        ),
      ),
    );
    if (cat == null || !context.mounted) return;
    try {
      final app = await context.api.startApplication(cat.id);
      if (context.mounted) context.go('/more/applications/${app.id}');
    } catch (e) {
      if (context.mounted) showMessage(context, messageOf(e), error: true);
    }
  }
}
