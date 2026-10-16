import 'package:flutter/material.dart';
import 'package:homend_core/homend_core.dart';

/// Weekly working hours (one block per day) and time off. Offers only come inside these hours.
class AvailabilityScreen extends StatelessWidget {
  const AvailabilityScreen({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('When I can work')),
    body: AsyncView<(List<WeeklySlot>, List<TimeOff>)>(
      load: () async {
        final api = context.api;
        final r = await Future.wait([api.availability(), api.timeOff()]);
        return (r[0] as List<WeeklySlot>, r[1] as List<TimeOff>);
      },
      builder: (context, d, reload) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _WeeklyForm(initial: d.$1),
          const SectionTitle('Time off'),
          if (d.$2.isEmpty) const EmptyState('No time off booked.'),
          ...spaced(
            d.$2.map(
              (t) => Panel(
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${day(t.startsAt)} to ${day(t.endsAt)}',
                            style: const TextStyle(fontWeight: FontWeight.w600),
                          ),
                          if (t.reason != null) Text(t.reason!, style: const TextStyle(color: Brand.slate500)),
                        ],
                      ),
                    ),
                    IconButton(
                      tooltip: 'Remove',
                      icon: const Icon(Icons.delete_outline),
                      onPressed: () async {
                        if (!await confirm(context, 'Remove this time off?', yes: 'Remove')) return;
                        if (!context.mounted) return;
                        try {
                          await context.api.removeTimeOff(t.id);
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
          const SizedBox(height: 12),
          OutlinedButton.icon(
            icon: const Icon(Icons.add),
            label: const Text('Add time off'),
            onPressed: () async {
              final range = await showDateRangePicker(
                context: context,
                firstDate: DateTime.now(),
                lastDate: DateTime.now().add(const Duration(days: 365)),
                helpText: 'Days you cannot work',
              );
              if (range == null || !context.mounted) return;
              try {
                final end = DateTime(range.end.year, range.end.month, range.end.day, 23, 59);
                await context.api.addTimeOff(range.start, end);
                await reload();
              } catch (e) {
                if (context.mounted) showMessage(context, messageOf(e), error: true);
              }
            },
          ),
        ],
      ),
    ),
  );
}

class _WeeklyForm extends StatefulWidget {
  const _WeeklyForm({required this.initial});

  final List<WeeklySlot> initial;

  @override
  State<_WeeklyForm> createState() => _WeeklyFormState();
}

class _WeeklyFormState extends State<_WeeklyForm> {
  // Monday first, as people read a week; the API counts 0 = Sunday.
  static const _order = [1, 2, 3, 4, 5, 6, 0];
  static const _names = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

  late final Map<int, (TimeOfDay, TimeOfDay)?> _days = {
    for (final d in _order) d: null,
    for (final s in widget.initial) s.dayOfWeek: (_parse(s.startTime), _parse(s.endTime)),
  };

  static TimeOfDay _parse(String hhmm) {
    final p = hhmm.split(':');
    return TimeOfDay(hour: int.tryParse(p[0]) ?? 8, minute: p.length > 1 ? int.tryParse(p[1]) ?? 0 : 0);
  }

  static String _fmt(TimeOfDay t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

  Future<void> _pick(int day, bool start) async {
    final cur = _days[day]!;
    final t = await showTimePicker(context: context, initialTime: start ? cur.$1 : cur.$2);
    if (t == null) return;
    setState(() => _days[day] = start ? (t, cur.$2) : (cur.$1, t));
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      const Text('Your usual hours', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
      const SizedBox(height: 4),
      const Text('The office only offers you jobs inside these hours.', style: TextStyle(color: Brand.slate500)),
      const SizedBox(height: 12),
      Card(
        child: Column(
          children: [
            for (final d in _order) ...[
              if (d != _order.first) const Divider(),
              ListTile(
                title: Text(_names[d]),
                subtitle: _days[d] == null
                    ? const Text('Not working')
                    : Row(
                        children: [
                          TextButton(onPressed: () => _pick(d, true), child: Text(_fmt(_days[d]!.$1))),
                          const Text('to'),
                          TextButton(onPressed: () => _pick(d, false), child: Text(_fmt(_days[d]!.$2))),
                        ],
                      ),
                trailing: Switch(
                  value: _days[d] != null,
                  onChanged: (on) => setState(
                    () => _days[d] = on
                        ? (const TimeOfDay(hour: 8, minute: 0), const TimeOfDay(hour: 17, minute: 0))
                        : null,
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
      const SizedBox(height: 12),
      BusyButton(
        label: 'Save hours',
        onPressed: () async {
          final weekly = [
            for (final e in _days.entries)
              if (e.value != null)
                WeeklySlot(dayOfWeek: e.key, startTime: _fmt(e.value!.$1), endTime: _fmt(e.value!.$2)),
          ];
          if (weekly.any((s) => s.startTime.compareTo(s.endTime) >= 0)) {
            showMessage(context, 'Each day must end after it starts.', error: true);
            return;
          }
          await context.api.setAvailability(weekly);
          if (context.mounted) showMessage(context, 'Hours saved.');
        },
      ),
    ],
  );
}
