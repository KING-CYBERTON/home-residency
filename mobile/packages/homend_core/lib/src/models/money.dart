import 'catalogue.dart';
import 'json.dart';

class InvoiceLine {
  const InvoiceLine({required this.kind, required this.description, required this.amountMinor, this.quantity});

  factory InvoiceLine.fromJson(Json j) => InvoiceLine(
    kind: j.str('kind'),
    description: j.str('description'),
    amountMinor: j.intOr('amount_minor'),
    quantity: j.numOrNull('quantity'),
  );

  final String kind;
  final String description;
  final int amountMinor;
  final double? quantity;
}

class Invoice {
  const Invoice({
    required this.id,
    required this.reference,
    required this.status,
    required this.totalMinor,
    required this.paidMinor,
    this.requestId,
    this.lines = const [],
    this.subtotalMinor = 0,
    this.vatMinor = 0,
    this.dueAt,
    this.issuedAt,
    this.paidAt,
  });

  factory Invoice.fromJson(Json j) => Invoice(
    id: j.str('id'),
    reference: j.str('reference'),
    status: j.str('status'),
    totalMinor: j.intOr('total_minor'),
    paidMinor: j.intOr('paid_minor'),
    requestId: j.strOrNull('request_id'),
    lines: j.list('lines', InvoiceLine.fromJson),
    subtotalMinor: j.intOr('subtotal_minor'),
    vatMinor: j.intOr('vat_minor'),
    dueAt: j.date('due_at'),
    issuedAt: j.date('issued_at'),
    paidAt: j.date('paid_at'),
  );

  final String id;
  final String reference;

  /// `draft`, `issued`, `partially_paid`, `paid`, `void` or `refunded`.
  final String status;
  final int totalMinor;
  final int paidMinor;
  final String? requestId;
  final List<InvoiceLine> lines;
  final int subtotalMinor;
  final int vatMinor;
  final DateTime? dueAt;
  final DateTime? issuedAt;
  final DateTime? paidAt;

  int get dueMinor => totalMinor - paidMinor;
  bool get isPayable => (status == 'issued' || status == 'partially_paid') && dueMinor > 0;
}

class Payment {
  const Payment({
    required this.id,
    required this.invoiceId,
    required this.status,
    required this.amountMinor,
    this.mpesaReceiptNumber,
    this.failureReason,
  });

  factory Payment.fromJson(Json j) => Payment(
    id: j.str('id'),
    invoiceId: j.str('invoice_id'),
    status: j.str('status'),
    amountMinor: j.intOr('amount_minor'),
    mpesaReceiptNumber: j.strOrNull('mpesa_receipt_number'),
    failureReason: j.strOrNull('failure_reason'),
  );

  final String id;
  final String invoiceId;

  /// `initiated`, `pending`, `succeeded`, `failed`, `cancelled` or `reversed`.
  final String status;
  final int amountMinor;
  final String? mpesaReceiptNumber;
  final String? failureReason;

  bool get isFinal => status != 'initiated' && status != 'pending';
}

class Subscription {
  const Subscription({
    required this.id,
    required this.plan,
    required this.status,
    this.currentPeriodEnd,
    this.cancelAtPeriodEnd = false,
  });

  factory Subscription.fromJson(Json j) => Subscription(
    id: j.str('id'),
    plan: SubscriptionPlan.fromJson(j.obj('plan') ?? const {}),
    status: j.str('status'),
    currentPeriodEnd: j.date('current_period_end'),
    cancelAtPeriodEnd: j.flag('cancel_at_period_end'),
  );

  final String id;
  final SubscriptionPlan plan;
  final String status;
  final DateTime? currentPeriodEnd;
  final bool cancelAtPeriodEnd;
}

class Earning {
  const Earning({
    required this.id,
    required this.netMinor,
    required this.status,
    required this.earnedAt,
    this.requestReference,
    this.grossMinor = 0,
    this.commissionMinor = 0,
    this.withholdingMinor = 0,
    this.holdReason,
  });

  factory Earning.fromJson(Json j) => Earning(
    id: j.str('id'),
    netMinor: j.intOr('net_minor'),
    status: j.str('status'),
    earnedAt: j.date('earned_at') ?? DateTime.now(),
    requestReference: j.strOrNull('request_reference'),
    grossMinor: j.intOr('gross_minor'),
    commissionMinor: j.intOr('commission_minor'),
    withholdingMinor: j.intOr('withholding_minor'),
    holdReason: j.strOrNull('hold_reason'),
  );

  final String id;
  final int netMinor;

  /// `pending`, `held`, `payable`, `in_payout`, `paid` or `cancelled`.
  final String status;
  final DateTime earnedAt;
  final String? requestReference;
  final int grossMinor;
  final int commissionMinor;
  final int withholdingMinor;
  final String? holdReason;
}

class EarningsSummary {
  const EarningsSummary({
    this.pendingMinor = 0,
    this.payableMinor = 0,
    this.heldMinor = 0,
    this.paidThisMonthMinor = 0,
    this.nextPayoutDate,
  });

  factory EarningsSummary.fromJson(Json j) => EarningsSummary(
    pendingMinor: j.intOr('pending_minor'),
    payableMinor: j.intOr('payable_minor'),
    heldMinor: j.intOr('held_minor'),
    paidThisMonthMinor: j.intOr('paid_this_month_minor'),
    nextPayoutDate: j.date('next_payout_date'),
  );

  final int pendingMinor;
  final int payableMinor;
  final int heldMinor;
  final int paidThisMonthMinor;
  final DateTime? nextPayoutDate;
}

class Payout {
  const Payout({
    required this.id,
    required this.amountMinor,
    required this.status,
    this.msisdn,
    this.mpesaReceiptNumber,
    this.failureReason,
    this.paidAt,
  });

  factory Payout.fromJson(Json j) => Payout(
    id: j.str('id'),
    amountMinor: j.intOr('amount_minor'),
    status: j.str('status'),
    msisdn: j.strOrNull('msisdn'),
    mpesaReceiptNumber: j.strOrNull('mpesa_receipt_number'),
    failureReason: j.strOrNull('failure_reason'),
    paidAt: j.date('paid_at'),
  );

  final String id;
  final int amountMinor;
  final String status;
  final String? msisdn;
  final String? mpesaReceiptNumber;
  final String? failureReason;
  final DateTime? paidAt;
}
