import 'package:flutter/material.dart';
import 'package:geolocator/geolocator.dart';
import 'package:homend_core/homend_core.dart';

class AddressesScreen extends StatefulWidget {
  const AddressesScreen({super.key});

  @override
  State<AddressesScreen> createState() => _AddressesScreenState();
}

class _AddressesScreenState extends State<AddressesScreen> {
  final _view = GlobalKey<AsyncViewState<List<Address>>>();

  Future<void> _edit([Address? a]) async {
    final saved = await Navigator.of(context)
        .push<Address>(MaterialPageRoute(builder: (_) => AddressFormScreen(address: a)));
    if (saved != null) await _view.currentState?.reload();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Addresses')),
    floatingActionButton: FloatingActionButton.extended(
      onPressed: _edit,
      icon: const Icon(Icons.add),
      label: const Text('Add address'),
    ),
    body: AsyncView<List<Address>>(
      key: _view,
      load: () => context.api.addresses(),
      builder: (context, list, reload) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (list.isEmpty) const EmptyState('No addresses yet.'),
          ...spaced(
            list.map(
              (a) => Panel(
                onTap: () => _edit(a),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(a.label, style: const TextStyle(fontWeight: FontWeight.w600)),
                          if (a.line.isNotEmpty) Text(a.line, style: const TextStyle(color: Brand.slate600)),
                          if (a.landmark != null)
                            Text('Near ${a.landmark}', style: const TextStyle(color: Brand.slate500)),
                        ],
                      ),
                    ),
                    if (a.isDefault) const StatusBadge('default', text: 'Default'),
                    IconButton(
                      tooltip: 'Remove',
                      icon: const Icon(Icons.delete_outline),
                      onPressed: () async {
                        if (!await confirm(context, 'Remove ${a.label}?', yes: 'Remove')) return;
                        if (!context.mounted) return;
                        try {
                          await context.api.archiveAddress(a.id);
                          await reload();
                        } catch (e) {
                          if (context.mounted) showMessage(context, messageOf(e), error: true);
                        }
                      },
                    ),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(height: 72),
        ],
      ),
    ),
  );
}

/// Add or edit an address. Gate details are encrypted and shown only to the assigned provider.
class AddressFormScreen extends StatefulWidget {
  const AddressFormScreen({super.key, this.address});

  final Address? address;

  @override
  State<AddressFormScreen> createState() => _AddressFormScreenState();
}

class _AddressFormScreenState extends State<AddressFormScreen> {
  final _form = GlobalKey<FormState>();
  late final _label = TextEditingController(text: widget.address?.label);
  late final _estate = TextEditingController(text: widget.address?.estate);
  late final _house = TextEditingController(text: widget.address?.houseNumber);
  late final _street = TextEditingController(text: widget.address?.street);
  late final _landmark = TextEditingController(text: widget.address?.landmark);
  late final _access = TextEditingController(text: widget.address?.accessDetails);
  late String? _area = widget.address?.serviceAreaId;
  late double? _lat = widget.address?.lat;
  late double? _lng = widget.address?.lng;
  late bool _default = widget.address?.isDefault ?? false;

  String? _t(TextEditingController c) => c.text.trim().isEmpty ? null : c.text.trim();

  Future<void> _locate() async {
    try {
      if (!await Geolocator.isLocationServiceEnabled()) throw 'off';
      var p = await Geolocator.checkPermission();
      if (p == LocationPermission.denied) p = await Geolocator.requestPermission();
      if (p == LocationPermission.denied || p == LocationPermission.deniedForever) throw 'denied';
      final pos = await Geolocator.getCurrentPosition(
        locationSettings: const LocationSettings(accuracy: LocationAccuracy.high, timeLimit: Duration(seconds: 20)),
      );
      setState(() {
        _lat = pos.latitude;
        _lng = pos.longitude;
      });
    } catch (_) {
      if (mounted) showMessage(context, 'Could not get your location. Check that location is on.', error: true);
    }
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    final input = AddressInput(
      label: _label.text.trim(),
      serviceAreaId: _area,
      estate: _t(_estate),
      houseNumber: _t(_house),
      street: _t(_street),
      landmark: _t(_landmark),
      accessDetails: _t(_access),
      lat: _lat,
      lng: _lng,
      isDefault: _default,
    );
    final api = context.api;
    final saved = widget.address == null
        ? await api.addAddress(input)
        : await api.updateAddress(widget.address!.id, input);
    if (mounted) Navigator.of(context).pop(saved);
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(widget.address == null ? 'Add an address' : 'Edit address')),
    body: AsyncView<List<ServiceArea>>(
      load: () => context.api.serviceAreas(),
      builder: (context, areas, _) => Form(
        key: _form,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextFormField(
              controller: _label,
              decoration: const InputDecoration(labelText: 'Name', hintText: "Home, Mum's house…"),
              validator: (v) => (v ?? '').trim().isEmpty ? 'Give it a name' : null,
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              isExpanded: true,
              initialValue: areas.any((a) => a.id == _area) ? _area : null,
              decoration: const InputDecoration(
                labelText: 'Area',
                helperText: 'Not listed? We are not in your area yet.',
              ),
              items: [for (final a in areas) DropdownMenuItem(value: a.id, child: Text(a.name))],
              onChanged: (v) => setState(() => _area = v),
              validator: (v) => v == null ? 'Choose your area' : null,
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: TextFormField(
                    controller: _estate,
                    decoration: const InputDecoration(labelText: 'Estate'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: TextFormField(
                    controller: _house,
                    decoration: const InputDecoration(labelText: 'House / flat no.'),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            TextFormField(
              controller: _street,
              decoration: const InputDecoration(labelText: 'Street or road'),
            ),
            const SizedBox(height: 12),
            TextFormField(
              controller: _landmark,
              decoration: const InputDecoration(labelText: 'Landmark', hintText: 'Near the Total petrol station'),
            ),
            const SizedBox(height: 12),
            TextFormField(
              controller: _access,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                labelText: 'Gate or access details',
                helperText: 'Kept encrypted, shown only to your assigned provider.',
              ),
            ),
            const SizedBox(height: 12),
            OutlinedButton.icon(
              icon: Icon(_lat == null ? Icons.my_location : Icons.check_circle_outline),
              label: Text(_lat == null ? 'Use my location' : 'Location saved'),
              onPressed: _locate,
            ),
            const Padding(
              padding: EdgeInsets.only(top: 4),
              child: Text(
                'Helps the provider find you and check in at your door.',
                style: TextStyle(color: Brand.slate500, fontSize: 13),
              ),
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Make this my default'),
              value: _default,
              onChanged: (v) => setState(() => _default = v),
            ),
            const SizedBox(height: 12),
            BusyButton(label: 'Save address', onPressed: _save),
          ],
        ),
      ),
    ),
  );
}
