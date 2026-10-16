import 'catalogue.dart';
import 'json.dart';

class Address {
  const Address({
    required this.id,
    required this.label,
    this.serviceAreaId,
    this.estate,
    this.street,
    this.houseNumber,
    this.landmark,
    this.lat,
    this.lng,
    this.accessDetails,
    this.isDefault = false,
  });

  factory Address.fromJson(Json j) => Address(
    id: j.str('id'),
    label: j.str('label'),
    serviceAreaId: j.strOrNull('service_area_id'),
    estate: j.strOrNull('estate'),
    street: j.strOrNull('street'),
    houseNumber: j.strOrNull('house_number'),
    landmark: j.strOrNull('landmark'),
    lat: j.numOrNull('lat'),
    lng: j.numOrNull('lng'),
    accessDetails: j.strOrNull('access_details'),
    isDefault: j.flag('is_default'),
  );

  final String id;
  final String label;
  final String? serviceAreaId;
  final String? estate;
  final String? street;
  final String? houseNumber;
  final String? landmark;
  final double? lat;
  final double? lng;
  final String? accessDetails;
  final bool isDefault;

  /// "Home · Kilimani Gardens".
  String get short => [label, if (estate != null) estate].join(' · ');

  /// "B4, Argwings Kodhek Rd, Kilimani Gardens".
  String get line => [houseNumber, street, estate].whereType<String>().join(', ');
}

/// `AddressInput`.
class AddressInput {
  const AddressInput({
    required this.label,
    this.serviceAreaId,
    this.estate,
    this.street,
    this.houseNumber,
    this.landmark,
    this.lat,
    this.lng,
    this.accessDetails,
    this.isDefault,
  });

  final String label;
  final String? serviceAreaId;
  final String? estate;
  final String? street;
  final String? houseNumber;
  final String? landmark;
  final double? lat;
  final double? lng;
  final String? accessDetails;
  final bool? isDefault;

  Json toJson() => compact({
    'label': label,
    'service_area_id': serviceAreaId,
    'estate': estate,
    'street': street,
    'house_number': houseNumber,
    'landmark': landmark,
    'lat': lat,
    'lng': lng,
    'access_details': accessDetails,
    'is_default': isDefault,
  });
}

class Quote {
  const Quote({
    required this.rateCardEntryId,
    required this.totalMinor,
    required this.serviceAreaSupported,
    this.quantity = 1,
    this.unitPriceMinor = 0,
    this.placementFeeMinor = 0,
    this.subtotalMinor = 0,
    this.vatMinor = 0,
  });

  factory Quote.fromJson(Json j) => Quote(
    rateCardEntryId: j.str('rate_card_entry_id'),
    totalMinor: j.intOr('total_minor'),
    serviceAreaSupported: j.flag('service_area_supported'),
    quantity: j.numOrNull('quantity') ?? 1,
    unitPriceMinor: j.intOr('unit_price_minor'),
    placementFeeMinor: j.intOr('placement_fee_minor'),
    subtotalMinor: j.intOr('subtotal_minor'),
    vatMinor: j.intOr('vat_minor'),
  );

  final String rateCardEntryId;
  final int totalMinor;
  final bool serviceAreaSupported;
  final double quantity;
  final int unitPriceMinor;
  final int placementFeeMinor;
  final int subtotalMinor;
  final int vatMinor;
}

/// What a client sees of a provider. Never carries contact details.
class ProviderPublic {
  const ProviderPublic({
    required this.id,
    required this.displayName,
    this.photoUrl,
    this.bio,
    this.yearsExperience,
    this.ratingAvg,
    this.ratingCount = 0,
    this.completedJobs = 0,
  });

  factory ProviderPublic.fromJson(Json j) => ProviderPublic(
    id: j.str('id'),
    displayName: j.str('display_name'),
    photoUrl: j.strOrNull('photo_url'),
    bio: j.strOrNull('bio'),
    yearsExperience: j.intOrNull('years_experience'),
    ratingAvg: j.numOrNull('rating_avg'),
    ratingCount: j.intOr('rating_count'),
    completedJobs: j.intOr('completed_jobs'),
  );

  final String id;
  final String displayName;
  final String? photoUrl;
  final String? bio;
  final int? yearsExperience;
  final double? ratingAvg;
  final int ratingCount;
  final int completedJobs;
}

class Visit {
  const Visit({
    required this.id,
    required this.requestId,
    required this.status,
    required this.scheduledStart,
    required this.scheduledEnd,
    this.checkedInAt,
    this.checkedOutAt,
    this.clientConfirmedAt,
    this.providerNotes,
  });

  factory Visit.fromJson(Json j) => Visit(
    id: j.str('id'),
    requestId: j.str('request_id'),
    status: j.str('status'),
    scheduledStart: j.date('scheduled_start') ?? DateTime.now(),
    scheduledEnd: j.date('scheduled_end') ?? DateTime.now(),
    checkedInAt: j.date('checked_in_at'),
    checkedOutAt: j.date('checked_out_at'),
    clientConfirmedAt: j.date('client_confirmed_at'),
    providerNotes: j.strOrNull('provider_notes'),
  );

  final String id;
  final String requestId;

  /// `scheduled`, `checked_in`, `completed`, `client_confirmed`, `missed` or `cancelled`.
  final String status;
  final DateTime scheduledStart;
  final DateTime scheduledEnd;
  final DateTime? checkedInAt;
  final DateTime? checkedOutAt;
  final DateTime? clientConfirmedAt;
  final String? providerNotes;
}

class ServiceRequest {
  const ServiceRequest({
    required this.id,
    required this.reference,
    required this.status,
    required this.service,
    required this.totalMinor,
    required this.requestedStart,
    required this.submittedAt,
    this.address,
    this.category,
    this.quantity,
    this.requestedEnd,
    this.recurrenceRule,
    this.clientNotes,
    this.provider,
    this.visits = const [],
    this.invoiceId,
    this.ratingRequired = false,
    this.cancelReason,
  });

  factory ServiceRequest.fromJson(Json j) => ServiceRequest(
    id: j.str('id'),
    reference: j.str('reference'),
    status: j.str('status'),
    service: Service.fromJson(j.obj('service') ?? const {}),
    totalMinor: j.intOr('total_minor'),
    requestedStart: j.date('requested_start') ?? DateTime.now(),
    submittedAt: j.date('submitted_at') ?? DateTime.now(),
    address: j.model('address', Address.fromJson),
    category: j.model('category', Category.fromJson),
    quantity: j.numOrNull('quantity'),
    requestedEnd: j.date('requested_end'),
    recurrenceRule: j.strOrNull('recurrence_rule'),
    clientNotes: j.strOrNull('client_notes'),
    provider: j.model('provider', ProviderPublic.fromJson),
    visits: j.list('visits', Visit.fromJson),
    invoiceId: j.strOrNull('invoice_id'),
    ratingRequired: j.flag('rating_required'),
    cancelReason: j.strOrNull('cancel_reason'),
  );

  final String id;
  final String reference;

  /// `submitted`, `triaging`, `awaiting_client_choice`, `awaiting_provider`, `confirmed`,
  /// `in_progress`, `awaiting_rating`, `closed`, `cancelled` or `unfilled`.
  final String status;
  final Service service;
  final int totalMinor;
  final DateTime requestedStart;
  final DateTime submittedAt;
  final Address? address;
  final Category? category;
  final double? quantity;
  final DateTime? requestedEnd;
  final String? recurrenceRule;
  final String? clientNotes;
  final ProviderPublic? provider;
  final List<Visit> visits;
  final String? invoiceId;
  final bool ratingRequired;
  final String? cancelReason;

  bool get isActive => const {
    'submitted',
    'triaging',
    'awaiting_client_choice',
    'awaiting_provider',
    'confirmed',
    'in_progress',
  }.contains(status);

  bool get needsClient => status == 'awaiting_client_choice' || status == 'awaiting_rating';

  /// The portal offers cancellation until the work has started.
  bool get canCancel =>
      const {'submitted', 'triaging', 'awaiting_client_choice', 'awaiting_provider', 'confirmed'}.contains(status);
}

/// `ServiceRequestCreate`.
class ServiceRequestCreate {
  const ServiceRequestCreate({
    required this.serviceId,
    required this.addressId,
    required this.requestedStart,
    this.quantity,
    this.requestedEnd,
    this.recurrenceRule,
    this.clientNotes,
  });

  final String serviceId;
  final String addressId;
  final DateTime requestedStart;
  final double? quantity;
  final DateTime? requestedEnd;
  final String? recurrenceRule;
  final String? clientNotes;

  Json toJson() => compact({
    'service_id': serviceId,
    'address_id': addressId,
    'requested_start': isoDateTime(requestedStart),
    'quantity': quantity,
    'requested_end': requestedEnd == null ? null : isoDateTime(requestedEnd!),
    'recurrence_rule': recurrenceRule,
    'client_notes': clientNotes,
  });
}

class ShortlistEntry {
  const ShortlistEntry({required this.id, required this.rank, required this.provider});

  factory ShortlistEntry.fromJson(Json j) => ShortlistEntry(
    id: j.str('id'),
    rank: j.intOr('rank'),
    provider: ProviderPublic.fromJson(j.obj('provider') ?? const {}),
  );

  final String id;
  final int rank;
  final ProviderPublic provider;
}

class CheckinCode {
  const CheckinCode({required this.code, this.validUntil});

  factory CheckinCode.fromJson(Json j) => CheckinCode(code: j.str('code'), validUntil: j.date('valid_until'));

  final String code;
  final DateTime? validUntil;
}

class Variation {
  const Variation({
    required this.id,
    required this.requestId,
    required this.raisedBy,
    required this.description,
    required this.clientDeltaMinor,
    required this.status,
    this.quantity,
  });

  factory Variation.fromJson(Json j) => Variation(
    id: j.str('id'),
    requestId: j.str('request_id'),
    raisedBy: j.str('raised_by'),
    description: j.str('description'),
    clientDeltaMinor: j.intOr('client_delta_minor'),
    status: j.str('status'),
    quantity: j.numOrNull('quantity'),
  );

  final String id;
  final String requestId;
  final String raisedBy;
  final String description;
  final int clientDeltaMinor;

  /// `pending`, `approved`, `rejected` or `withdrawn`.
  final String status;
  final double? quantity;
}

/// `RatingCreate`.
class RatingInput {
  const RatingInput({required this.stars, this.visitId, this.punctuality, this.quality, this.conduct, this.comment});

  final int stars;
  final String? visitId;
  final int? punctuality;
  final int? quality;
  final int? conduct;
  final String? comment;

  Json toJson() {
    final sub = compact({'punctuality': punctuality, 'quality': quality, 'conduct': conduct});
    return compact({
      'stars': stars,
      'visit_id': visitId,
      'sub_scores': sub.isEmpty ? null : sub,
      'comment': (comment ?? '').trim().isEmpty ? null : comment!.trim(),
    });
  }
}

class Complaint {
  const Complaint({
    required this.id,
    required this.reference,
    required this.severity,
    required this.status,
    required this.summary,
    required this.createdAt,
    this.requestId,
    this.outcomeSummary,
    this.closedAt,
  });

  factory Complaint.fromJson(Json j) => Complaint(
    id: j.str('id'),
    reference: j.str('reference'),
    severity: j.str('severity'),
    status: j.str('status'),
    summary: j.str('summary'),
    createdAt: j.date('created_at') ?? DateTime.now(),
    requestId: j.strOrNull('request_id'),
    outcomeSummary: j.strOrNull('outcome_summary'),
    closedAt: j.date('closed_at'),
  );

  final String id;
  final String reference;

  /// `quality`, `conduct`, `safeguarding` or `loss`.
  final String severity;
  final String status;
  final String summary;
  final DateTime createdAt;
  final String? requestId;
  final String? outcomeSummary;
  final DateTime? closedAt;
}

/// `ComplaintCreate`.
class ComplaintInput {
  const ComplaintInput({
    required this.severity,
    required this.summary,
    this.requestId,
    this.visitId,
    this.details,
    this.evidenceDocumentIds = const [],
  });

  final String severity;
  final String summary;
  final String? requestId;
  final String? visitId;
  final String? details;
  final List<String> evidenceDocumentIds;

  Json toJson() => compact({
    'severity': severity,
    'summary': summary,
    'request_id': requestId,
    'visit_id': visitId,
    'details': (details ?? '').trim().isEmpty ? null : details!.trim(),
    'evidence_document_ids': evidenceDocumentIds.isEmpty ? null : evidenceDocumentIds,
  });
}
