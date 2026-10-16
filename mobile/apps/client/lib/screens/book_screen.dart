import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:homend_core/homend_core.dart';

import '../app.dart';
import 'addresses_screen.dart';

class _Setup {
  _Setup(this.categories, this.addresses);
  final List<Category> categories;
  final List<Address> addresses;
}

/// Book a service: what, how much, where, when. Priced live from the rate card; management then
/// matches a vetted provider. Requests are never broadcast to providers.
class BookScreen extends StatelessWidget {
  const BookScreen({super.key, this.categorySlug});

  final String? categorySlug;

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: clientBar(context, title: 'Book a service'),
    body: AsyncView<_Setup>(
      load: () async {
        final api = context.api;
        final r = await Future.wait([api.categories(), api.addresses()]);
        return _Setup(r[0] as List<Category>, r[1] as List<Address>);
      },
      builder: (context, s, reload) => _BookForm(setup: s, initialSlug: categorySlug),
    ),
  );
}

class _BookForm extends StatefulWidget {
  const _BookForm({required this.setup, this.initialSlug});

  final _Setup setup;
  final String? initialSlug;

  @override
  State<_BookForm> createState() => _BookFormState();
}

class _BookFormState extends State<_BookForm> {
  late final List<Address> _addresses = [...widget.setup.addresses];
  Category? _category;
  List<Service> _services = const [];
  bool _loadingServices = false;
  Service? _service;
  final _qty = TextEditingController(text: '1');
  Address? _address;
  late DateTime _start = _defaultStart();
  final _notes = TextEditingController();

  Quote? _quote;
  String? _quoteError;
  Timer? _debounce;

  /// One key per booking attempt: a retry after a dropped connection cannot book twice.
  String _idempotencyKey = ApiClient.newIdempotencyKey();

  static DateTime _defaultStart() {
    final t = DateTime.now().add(const Duration(days: 1));
    return DateTime(t.year, t.month, t.day, 9);
  }

  @override
  void initState() {
    super.initState();
    final cats = widget.setup.categories;
    _address = _addresses.where((a) => a.isDefault).firstOrNull ?? _addresses.firstOrNull;
    final initial = cats.where((c) => c.slug == widget.initialSlug).firstOrNull ?? cats.firstOrNull;
    if (initial != null) _pickCategory(initial);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    super.dispose();
  }

  Future<void> _pickCategory(Category c) async {
    setState(() {
      _category = c;
      _services = const [];
      _service = null;
      _loadingServices = true;
      _quote = null;
    });
    try {
      final detail = await context.api.category(c.slug);
      if (!mounted || _category?.id != c.id) return;
      setState(() {
        _services = detail.services.where((s) => s.currentPrice != null).toList();
        _service = _services.firstOrNull;
        _loadingServices = false;
      });
      _requote();
    } catch (e) {
      if (!mounted) return;
      setState(() => _loadingServices = false);
      showMessage(context, messageOf(e), error: true);
    }
  }

  double get _quantity => _service?.pricingUnit == 'per_job' ? 1 : (double.tryParse(_qty.text.trim()) ?? 0);

  void _requote() {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 400), () async {
      final s = _service, a = _address;
      if (s == null || a == null || _quantity <= 0) {
        setState(() => _quote = null);
        return;
      }
      try {
        final q = await context.api.quote(
          serviceId: s.id,
          addressId: a.id,
          quantity: _quantity,
          requestedStart: _start,
        );
        if (mounted) {
          setState(() {
            _quote = q;
            _quoteError = null;
          });
        }
      } catch (e) {
        if (mounted) {
          setState(() {
            _quote = null;
            _quoteError = messageOf(e);
          });
        }
      }
    });
  }

  Future<void> _pickWhen() async {
    final d = await showDatePicker(
      context: context,
      initialDate: _start,
      firstDate: DateTime.now(),
      lastDate: DateTime.now().add(const Duration(days: 180)),
    );
    if (d == null || !mounted) return;
    final t = await showTimePicker(context: context, initialTime: TimeOfDay.fromDateTime(_start));
    if (t == null) return;
    setState(() => _start = DateTime(d.year, d.month, d.day, t.hour, t.minute));
    _requote();
  }

  Future<void> _addAddress() async {
    final a = await Navigator.of(context).push<Address>(MaterialPageRoute(builder: (_) => const AddressFormScreen()));
    if (a == null) return;
    setState(() {
      _addresses.add(a);
      _address = a;
    });
    _requote();
  }

  Future<void> _submit() async {
    final s = _service, a = _address;
    if (s == null || a == null) {
      showMessage(
        context,
        a == null ? 'Add the address where you need the service.' : 'Choose a service.',
        error: true,
      );
      return;
    }
    if (_quantity <= 0) {
      showMessage(context, 'Enter how much you need.', error: true);
      return;
    }
    final r = await context.api.createRequest(
      ServiceRequestCreate(
        serviceId: s.id,
        addressId: a.id,
        quantity: _quantity,
        requestedStart: _start,
        clientNotes: _notes.text.trim().isEmpty ? null : _notes.text.trim(),
      ),
      idempotencyKey: _idempotencyKey,
    );
    _idempotencyKey = ApiClient.newIdempotencyKey();
    if (mounted) context.go('/bookings/${r.id}?new=1');
  }

  @override
  Widget build(BuildContext context) {
    final cat = _category;
    final me = HomendScope.of(context).me;
    final needsId = cat?.requiresClientIdVerification == true && me?.idVerification != 'verified';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        DropdownButtonFormField<String>(
          isExpanded: true,
          initialValue: cat?.id,
          decoration: const InputDecoration(labelText: 'What do you need?'),
          items: [for (final c in widget.setup.categories) DropdownMenuItem(value: c.id, child: Text(c.name))],
          onChanged: (id) => _pickCategory(widget.setup.categories.firstWhere((c) => c.id == id)),
        ),
        if (cat?.description != null) ...[
          const SizedBox(height: 12),
          Text(cat!.name, style: const TextStyle(fontWeight: FontWeight.w600)),
          Text(cat.description!, style: const TextStyle(color: Brand.slate600)),
        ],
        if (needsId) ...[
          const SizedBox(height: 12),
          Notice(
            'We verify your ID before booking ${cat!.name.toLowerCase()}, so providers know who they are working for.',
            title: 'ID needed',
            warning: true,
            onTap: () => context.go('/account'),
          ),
        ],
        const SizedBox(height: 12),
        if (_loadingServices)
          const Center(
            child: Padding(padding: EdgeInsets.all(16), child: CircularProgressIndicator()),
          ),
        if (!_loadingServices && cat != null && _services.isEmpty) const EmptyState('No services priced for this yet.'),
        RadioGroup<String>(
          groupValue: _service?.id,
          onChanged: (id) {
            setState(() => _service = _services.firstWhere((s) => s.id == id));
            _requote();
          },
          child: Column(
            children: spaced(_services.map((s) => _ServiceOption(service: s, selected: s.id == _service?.id))),
          ),
        ),
        if (_service != null && _service!.pricingUnit != 'per_job') ...[
          const SizedBox(height: 16),
          TextField(
            controller: _qty,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: InputDecoration(
              labelText: 'How many ${_unitWord(_service!.pricingUnit)}?',
              helperText: _service!.minQuantity == null ? null : 'At least ${quantity(_service!.minQuantity!)}',
            ),
            onChanged: (_) => _requote(),
          ),
        ],
        const SizedBox(height: 16),
        if (_addresses.isEmpty)
          const Text('Where?', style: TextStyle(fontWeight: FontWeight.w600))
        else
          DropdownButtonFormField<String>(
            isExpanded: true,
            initialValue: _address?.id,
            decoration: const InputDecoration(labelText: 'Where?'),
            items: [for (final a in _addresses) DropdownMenuItem(value: a.id, child: Text(a.short))],
            onChanged: (id) {
              setState(() => _address = _addresses.firstWhere((a) => a.id == id));
              _requote();
            },
          ),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton(
            onPressed: _addAddress,
            child: Text(_addresses.isEmpty ? 'Add your address' : 'Add another address'),
          ),
        ),
        const SizedBox(height: 4),
        InkWell(
          onTap: _pickWhen,
          child: InputDecorator(
            decoration: const InputDecoration(labelText: 'When should the provider arrive?'),
            child: Text(when(_start)),
          ),
        ),
        const SizedBox(height: 16),
        TextField(
          controller: _notes,
          minLines: 3,
          maxLines: 5,
          maxLength: 1000,
          decoration: const InputDecoration(
            labelText: 'Anything the provider should know? (optional)',
            hintText: 'For example: two bedrooms, a dog, ironing needed',
          ),
        ),
        const SizedBox(height: 8),
        if (_quote case final q?)
          Panel(
            child: Column(
              children: [
                KeyValue('Service', kes(q.subtotalMinor - q.placementFeeMinor)),
                if (q.placementFeeMinor > 0) KeyValue('Placement fee', kes(q.placementFeeMinor)),
                KeyValue('VAT', kes(q.vatMinor)),
                const Divider(),
                KeyValue('Total', kes(q.totalMinor), bold: true),
                if (!q.serviceAreaSupported)
                  const Padding(
                    padding: EdgeInsets.only(top: 8),
                    child: Text('We do not serve this address yet.', style: TextStyle(color: Brand.red700)),
                  ),
              ],
            ),
          )
        else if (_quoteError != null)
          Text(_quoteError!, style: const TextStyle(color: Brand.red700)),
        const SizedBox(height: 16),
        BusyButton(
          label: 'Book now',
          onPressed: needsId || (_quote != null && !_quote!.serviceAreaSupported) ? null : _submit,
        ),
        const SizedBox(height: 8),
        const Text(
          'You pay after the work is done, by M-Pesa. We match you with a vetted provider and confirm by SMS.',
          textAlign: TextAlign.center,
          style: TextStyle(color: Brand.slate500, fontSize: 13),
        ),
      ],
    );
  }

  String _unitWord(String unit) => switch (unit) {
    'per_hour' => 'hours',
    'per_day' => 'days',
    'per_month' => 'months',
    'per_room' => 'rooms',
    _ => '',
  };
}

class _ServiceOption extends StatelessWidget {
  const _ServiceOption({required this.service, required this.selected});

  final Service service;
  final bool selected;

  @override
  Widget build(BuildContext context) {
    final s = service;
    final price = s.currentPrice!;
    return Panel(
      borderColor: selected ? Brand.c600 : null,
      onTap: () => RadioGroup.maybeOf<String>(context)?.onChanged(s.id),
      padding: const EdgeInsets.fromLTRB(4, 8, 16, 12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Radio<String>(value: s.id),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const SizedBox(height: 10),
                Row(
                  children: [
                    Expanded(
                      child: Text(s.name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 16)),
                    ),
                    Text(
                      '${kes(price.clientPriceMinor)}${s.pricingUnit == 'per_job' ? '' : ' / ${labelOf(s.pricingUnit).replaceFirst('per ', '')}'}',
                    ),
                  ],
                ),
                if (s.description != null) Text(s.description!, style: const TextStyle(color: Brand.slate600)),
                if (s.scopeItems.isNotEmpty)
                  Text(
                    'Includes: ${s.scopeItems.join(', ')}',
                    style: const TextStyle(color: Brand.slate500, fontSize: 13),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
