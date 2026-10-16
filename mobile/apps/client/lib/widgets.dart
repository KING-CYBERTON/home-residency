import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

/// A booking in a list: service, reference, arrival and status.
class RequestCard extends StatelessWidget {
  const RequestCard(this.r, {super.key});

  final ServiceRequest r;

  @override
  Widget build(BuildContext context) => Panel(
    onTap: () => context.go('/bookings/${r.id}'),
    borderColor: r.needsClient ? Brand.amber200 : null,
    color: r.needsClient ? Brand.amber50 : null,
    child: Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(r.service.name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 16)),
              Text('${r.reference} · ${when(r.requestedStart)}', style: const TextStyle(color: Brand.slate500)),
            ],
          ),
        ),
        StatusBadge(r.status),
      ],
    ),
  );
}

/// What a client sees of a provider: initial or photo, rating, jobs, experience, bio.
class ProviderCard extends StatelessWidget {
  const ProviderCard(this.p, {super.key});

  final ProviderPublic p;

  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      CircleAvatar(
        radius: 24,
        backgroundColor: Brand.c100,
        foregroundImage: p.photoUrl == null ? null : NetworkImage(p.photoUrl!),
        child: Text(
          p.displayName.isEmpty ? '?' : p.displayName[0],
          style: const TextStyle(color: Brand.c800, fontWeight: FontWeight.w700),
        ),
      ),
      const SizedBox(width: 12),
      Expanded(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(p.displayName, style: const TextStyle(fontWeight: FontWeight.w600)),
            Text(
              [
                if (p.ratingAvg != null) '★ ${p.ratingAvg!.toStringAsFixed(1)} (${p.ratingCount})',
                '${p.completedJobs} jobs',
                if (p.yearsExperience != null) "${p.yearsExperience} years' experience",
              ].join(' · '),
              style: const TextStyle(color: Brand.slate600, fontSize: 13),
            ),
            if (p.bio != null) Padding(padding: const EdgeInsets.only(top: 4), child: Text(p.bio!)),
          ],
        ),
      ),
    ],
  );
}
