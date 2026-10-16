import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';
import '../widgets.dart';

class BookingsScreen extends StatefulWidget {
  const BookingsScreen({super.key});

  @override
  State<BookingsScreen> createState() => _BookingsScreenState();
}

class _BookingsScreenState extends State<BookingsScreen> {
  final _all = <ServiceRequest>[];
  String? _cursor;

  Future<List<ServiceRequest>> _load({bool more = false}) async {
    final page = await context.api.requests(cursor: more ? _cursor : null, limit: 25);
    if (!more) _all.clear();
    _all.addAll(page.data);
    _cursor = page.nextCursor;
    return _all;
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: clientBar(context, title: 'My bookings'),
    body: AsyncView<List<ServiceRequest>>(
      load: _load,
      builder: (context, list, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (list.isEmpty) const EmptyState('No bookings yet. Tap Book to get started.'),
          ...spaced(list.map((r) => RequestCard(r))),
          if (_cursor != null)
            TextButton(
              onPressed: () async {
                await _load(more: true);
                setState(() {});
              },
              child: const Text('Show more'),
            ),
        ],
      ),
    ),
  );
}
