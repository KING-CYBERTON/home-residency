import 'package:intl/intl.dart';

final _kes = NumberFormat.decimalPattern('en_KE');

/// Integer KES cents to `KES 2,500` (cents shown only when there are any), as the portals do.
String kes(int minor) {
  final neg = minor < 0;
  final abs = minor.abs();
  final whole = _kes.format(abs ~/ 100);
  final cents = abs % 100;
  return '${neg ? '−' : ''}KES $whole${cents == 0 ? '' : '.${cents.toString().padLeft(2, '0')}'}';
}

/// `Wed 14 Oct, 15:42`.
String when(DateTime d) => DateFormat('EEE d MMM, HH:mm').format(d);

/// `Wed 14 Oct`.
String day(DateTime d) => DateFormat('EEE d MMM').format(d);

/// `15:42`.
String clock(DateTime d) => DateFormat('HH:mm').format(d);

/// `14 Oct 2026`.
String date(DateTime d) => DateFormat('d MMM y').format(d);

String quantity(num q) => q == q.roundToDouble() ? q.toInt().toString() : q.toString();

/// Words for status and type codes. Unknown codes fall back to "snake case" → "Snake case".
String labelOf(String code) => _labels[code] ?? _humanize(code);

String _humanize(String code) {
  if (code.isEmpty) return code;
  final s = code.replaceAll('_', ' ');
  return s[0].toUpperCase() + s.substring(1);
}

const _labels = <String, String>{
  // Requests
  'submitted': 'Received',
  'triaging': 'Finding a provider',
  'awaiting_client_choice': 'Choose your provider',
  'awaiting_provider': 'Waiting for provider',
  'confirmed': 'Confirmed',
  'in_progress': 'In progress',
  'awaiting_rating': 'Rate the job',
  'closed': 'Done',
  'cancelled': 'Cancelled',
  'unfilled': 'Not filled',
  // Visits
  'scheduled': 'Scheduled',
  'checked_in': 'Checked in',
  'completed': 'Work done',
  'client_confirmed': 'Confirmed',
  'missed': 'Missed',
  // Invoices and payments
  'issued': 'To pay',
  'partially_paid': 'Part paid',
  'paid': 'Paid',
  'void': 'Void',
  'refunded': 'Refunded',
  'initiated': 'Waiting',
  'pending': 'Pending',
  'succeeded': 'Paid',
  'failed': 'Failed',
  'reversed': 'Reversed',
  // Earnings and payouts
  'held': 'On hold',
  'payable': 'To be paid',
  'in_payout': 'Being paid',
  'processing': 'Sending',
  'approved': 'Approved',
  // Applications and checklist
  'draft': 'Not submitted',
  'incomplete': 'Needs more',
  'in_vetting': 'Being checked',
  'awaiting_approval': 'Awaiting decision',
  'deferred': 'Deferred',
  'rejected': 'Not approved',
  'withdrawn': 'Withdrawn',
  'satisfied': 'Done',
  'waived': 'Not needed',
  // Providers
  'applicant': 'Applicant',
  'active': 'Active',
  'suspended': 'Suspended',
  'disqualified': 'Disqualified',
  // Offers
  'offered': 'New offer',
  'accepted': 'Accepted',
  'declined': 'Declined',
  'expired': 'Expired',
  // Complaints
  'open': 'Open',
  'investigating': 'Investigating',
  'awaiting_provider_account': 'Your side needed',
  'awaiting_decision': 'Awaiting decision',
  'decided': 'Decided',
  'quality': 'Quality of work',
  'conduct': 'Conduct',
  'safeguarding': 'Safety of a person',
  'loss': 'Loss or damage',
  // Documents
  'pending_upload': 'Not uploaded',
  'uploaded': 'Uploaded',
  'verified': 'Verified',
  'purged': 'Deleted',
  'national_id': 'National ID',
  'kra_pin': 'KRA PIN',
  'good_conduct_certificate': 'Certificate of Good Conduct',
  'nssf_card': 'NSSF card',
  'shif_card': 'SHIF card',
  'passport_photo': 'Passport photo',
  // Pricing
  'per_job': 'per job',
  'per_hour': 'per hour',
  'per_day': 'per day',
  'per_month': 'per month',
  'per_room': 'per room',
  // ID verification
  'none': 'Not verified',
  // Training
  'attended': 'Attended',
  'passed': 'Passed',
  'no_show': 'Missed',
};

/// Accepts `0712 345 678`, `712345678`, `254712345678` or `+254712345678`; returns E.164 or null.
String? normalizeKenyanPhone(String input) {
  final digits = input.replaceAll(RegExp(r'[^0-9+]'), '');
  String? out;
  if (RegExp(r'^\+[1-9][0-9]{7,14}$').hasMatch(digits)) {
    out = digits;
  } else if (RegExp(r'^254[17][0-9]{8}$').hasMatch(digits)) {
    out = '+$digits';
  } else if (RegExp(r'^0[17][0-9]{8}$').hasMatch(digits)) {
    out = '+254${digits.substring(1)}';
  } else if (RegExp(r'^[17][0-9]{8}$').hasMatch(digits)) {
    out = '+254$digits';
  }
  return out;
}

/// `+254781334473` → `0781 334 473` for display.
String displayPhone(String? e164) {
  if (e164 == null) return '';
  if (e164.startsWith('+254') && e164.length == 13) {
    final n = '0${e164.substring(4)}';
    return '${n.substring(0, 4)} ${n.substring(4, 7)} ${n.substring(7)}';
  }
  return e164;
}
