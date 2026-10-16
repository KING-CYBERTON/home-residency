import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

/// Everything in the provider's part of the vault, with what the office decided.
class DocumentsScreen extends StatelessWidget {
  const DocumentsScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('My documents')),
    body: AsyncView<List<Document>>(
      load: () => context.api.providerDocuments(),
      builder: (context, docs, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Notice(
            'Upload documents from your application checklist. They are stored encrypted and only the vetting team can open them.',
          ),
          const SizedBox(height: 16),
          if (docs.isEmpty) const EmptyState('No documents yet.'),
          ...spaced(
            docs.map(
              (d) => Panel(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(labelOf(d.documentType), style: const TextStyle(fontWeight: FontWeight.w600)),
                        ),
                        StatusBadge(d.status),
                      ],
                    ),
                    Text(
                      [
                        d.originalFilename,
                        'added ${day(d.createdAt)}',
                        if (d.expiresOn != null) 'expires ${date(d.expiresOn!)}',
                      ].whereType<String>().join(' · '),
                      style: const TextStyle(color: Brand.slate500, fontSize: 13),
                    ),
                    if (d.rejectionReason != null)
                      Text(d.rejectionReason!, style: const TextStyle(color: Brand.red700)),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    ),
  );
}
