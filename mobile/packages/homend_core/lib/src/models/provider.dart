import 'bookings.dart';
import 'catalogue.dart';
import 'json.dart';

class ProviderCategoryStatus {
  const ProviderCategoryStatus(this.categoryId, this.status);
  final String categoryId;
  final String status;
}

class ProviderProfile {
  const ProviderProfile({
    required this.id,
    required this.fullName,
    required this.status,
    this.homeServiceAreaId,
    this.serviceAreaIds = const [],
    this.bio,
    this.yearsExperience,
    this.languages = const [],
    this.payoutMsisdn,
    this.categories = const [],
    this.ratingAvg,
    this.ratingCount = 0,
    this.appraisalScore,
    this.completedJobs = 0,
  });

  factory ProviderProfile.fromJson(Json j) => ProviderProfile(
    id: j.str('id'),
    fullName: j.str('full_name'),
    status: j.str('status'),
    homeServiceAreaId: j.strOrNull('home_service_area_id'),
    serviceAreaIds: j.strings('service_area_ids'),
    bio: j.strOrNull('bio'),
    yearsExperience: j.intOrNull('years_experience'),
    languages: j.strings('languages'),
    payoutMsisdn: j.strOrNull('payout_msisdn'),
    categories: j.list('categories', (c) => ProviderCategoryStatus(c.str('category_id'), c.str('status'))),
    ratingAvg: j.numOrNull('rating_avg'),
    ratingCount: j.intOr('rating_count'),
    appraisalScore: j.numOrNull('appraisal_score'),
    completedJobs: j.intOr('completed_jobs'),
  );

  final String id;
  final String fullName;
  final String status;
  final String? homeServiceAreaId;
  final List<String> serviceAreaIds;
  final String? bio;
  final int? yearsExperience;
  final List<String> languages;
  final String? payoutMsisdn;
  final List<ProviderCategoryStatus> categories;
  final double? ratingAvg;
  final int ratingCount;
  final double? appraisalScore;
  final int completedJobs;
}

/// `ProviderProfileUpdate`: only the fields the provider changed are sent.
class ProviderProfileUpdate {
  const ProviderProfileUpdate({
    this.homeServiceAreaId,
    this.serviceAreaIds,
    this.bio,
    this.yearsExperience,
    this.languages,
    this.payoutMsisdn,
  });

  final String? homeServiceAreaId;
  final List<String>? serviceAreaIds;
  final String? bio;
  final int? yearsExperience;
  final List<String>? languages;
  final String? payoutMsisdn;

  Json toJson() => compact({
    'home_service_area_id': homeServiceAreaId,
    'service_area_ids': serviceAreaIds,
    'bio': bio,
    'years_experience': yearsExperience,
    'languages': languages,
    'payout_msisdn': payoutMsisdn,
  });
}

class ChecklistTemplateItem {
  const ChecklistTemplateItem({
    required this.kind,
    required this.label,
    required this.isMandatory,
    this.documentType,
    this.minCount,
    this.validityMonths,
  });

  factory ChecklistTemplateItem.fromJson(Json j) => ChecklistTemplateItem(
    kind: j.str('kind'),
    label: j.str('label'),
    isMandatory: j.flag('is_mandatory'),
    documentType: j.strOrNull('document_type'),
    minCount: j.intOrNull('min_count'),
    validityMonths: j.intOrNull('validity_months'),
  );

  /// `document`, `referee`, `clearance`, `interview`, `practical_assessment`, `trade_licence`,
  /// `medical` or `training`.
  final String kind;
  final String label;
  final bool isMandatory;
  final String? documentType;
  final int? minCount;
  final int? validityMonths;

  /// Items the provider fulfils by uploading a file.
  bool get takesUpload => kind == 'document' || kind == 'trade_licence' || kind == 'medical';
}

class ApplicationItem {
  const ApplicationItem({
    required this.id,
    required this.templateItem,
    required this.status,
    this.documentId,
    this.notes,
  });

  factory ApplicationItem.fromJson(Json j) => ApplicationItem(
    id: j.str('id'),
    templateItem: ChecklistTemplateItem.fromJson(j.obj('template_item') ?? const {}),
    status: j.str('status'),
    documentId: j.strOrNull('document_id'),
    notes: j.strOrNull('notes'),
  );

  final String id;
  final ChecklistTemplateItem templateItem;

  /// `pending`, `submitted`, `satisfied`, `failed` or `waived`.
  final String status;
  final String? documentId;
  final String? notes;
}

class Referee {
  const Referee({
    required this.id,
    required this.fullName,
    required this.outcome,
    this.relationship,
    this.employerName,
  });

  factory Referee.fromJson(Json j) => Referee(
    id: j.str('id'),
    fullName: j.str('full_name'),
    outcome: j.str('outcome'),
    relationship: j.strOrNull('relationship'),
    employerName: j.strOrNull('employer_name'),
  );

  final String id;
  final String fullName;
  final String outcome;
  final String? relationship;
  final String? employerName;
}

/// `RefereeInput`.
class RefereeInput {
  const RefereeInput({
    required this.fullName,
    required this.relationship,
    required this.phone,
    this.employerName,
    this.employmentFrom,
    this.employmentTo,
  });

  final String fullName;
  final String relationship;
  final String phone;
  final String? employerName;
  final DateTime? employmentFrom;
  final DateTime? employmentTo;

  Json toJson() => compact({
    'full_name': fullName,
    'relationship': relationship,
    'phone': phone,
    'employer_name': employerName,
    'employment_from': employmentFrom == null ? null : isoDate(employmentFrom!),
    'employment_to': employmentTo == null ? null : isoDate(employmentTo!),
  });
}

class Application {
  const Application({
    required this.id,
    required this.reference,
    required this.categoryId,
    required this.status,
    this.satisfied = 0,
    this.total = 0,
    this.items = const [],
    this.referees = const [],
    this.decisionReason,
    this.missing = const [],
  });

  /// Reads both `ApplicationSummary` (list) and `Application` (detail).
  factory Application.fromJson(Json j) {
    final progress = j.obj('checklist_progress') ?? const {};
    return Application(
      id: j.str('id'),
      reference: j.str('reference'),
      categoryId: j.str('category_id'),
      status: j.str('status'),
      satisfied: progress.intOr('satisfied'),
      total: progress.intOr('total'),
      items: j.list('items', ApplicationItem.fromJson),
      referees: j.list('referees', Referee.fromJson),
      decisionReason: j.strOrNull('decision_reason'),
      missing: j.strings('missing'),
    );
  }

  final String id;
  final String reference;
  final String categoryId;

  /// `draft`, `submitted`, `incomplete`, `in_vetting`, `awaiting_approval`, `approved`,
  /// `deferred`, `rejected` or `withdrawn`.
  final String status;
  final int satisfied;
  final int total;
  final List<ApplicationItem> items;
  final List<Referee> referees;
  final String? decisionReason;
  final List<String> missing;

  /// The provider can still change it (the portal's rule).
  bool get isEditable => status == 'draft' || status == 'incomplete' || status == 'deferred';
  bool get canWithdraw => const {'draft', 'incomplete', 'deferred', 'submitted', 'in_vetting'}.contains(status);
  bool get isUnderReview => const {'submitted', 'in_vetting', 'awaiting_approval'}.contains(status);
}

class TrainingEnrolment {
  const TrainingEnrolment({
    required this.id,
    required this.courseName,
    required this.status,
    this.isRetraining = false,
    this.scheduledFor,
    this.score,
    this.passMark,
  });

  factory TrainingEnrolment.fromJson(Json j) {
    final course = j.obj('course') ?? const {};
    return TrainingEnrolment(
      id: j.str('id'),
      courseName: course.str('name'),
      status: j.str('status'),
      isRetraining: j.flag('is_retraining'),
      scheduledFor: j.date('scheduled_for'),
      score: j.numOrNull('score'),
      passMark: course.intOrNull('pass_mark'),
    );
  }

  final String id;
  final String courseName;
  final String status;
  final bool isRetraining;
  final DateTime? scheduledFor;
  final double? score;
  final int? passMark;
}

class Contract {
  const Contract({
    required this.contractVersion,
    required this.codeOfConductVersion,
    this.engagementBasis,
    this.documentUrl,
    this.signedAt,
  });

  factory Contract.fromJson(Json j) => Contract(
    contractVersion: j.str('contract_version'),
    codeOfConductVersion: j.str('code_of_conduct_version'),
    engagementBasis: j.strOrNull('engagement_basis'),
    documentUrl: j.strOrNull('document_url'),
    signedAt: j.date('signed_at'),
  );

  final String contractVersion;
  final String codeOfConductVersion;
  final String? engagementBasis;
  final String? documentUrl;
  final DateTime? signedAt;
}

class WeeklySlot {
  const WeeklySlot({required this.dayOfWeek, required this.startTime, required this.endTime});

  factory WeeklySlot.fromJson(Json j) =>
      WeeklySlot(dayOfWeek: j.intOr('day_of_week'), startTime: j.str('start_time'), endTime: j.str('end_time'));

  /// 0 = Sunday … 6 = Saturday, as stored by the backend.
  final int dayOfWeek;

  /// `HH:MM`.
  final String startTime;
  final String endTime;

  Json toJson() => {'day_of_week': dayOfWeek, 'start_time': startTime, 'end_time': endTime};
}

class TimeOff {
  const TimeOff({required this.id, required this.startsAt, required this.endsAt, this.reason});

  factory TimeOff.fromJson(Json j) => TimeOff(
    id: j.str('id'),
    startsAt: j.date('starts_at') ?? DateTime.now(),
    endsAt: j.date('ends_at') ?? DateTime.now(),
    reason: j.strOrNull('reason'),
  );

  final String id;
  final DateTime startsAt;
  final DateTime endsAt;
  final String? reason;
}

class Offer {
  const Offer({
    required this.id,
    required this.status,
    required this.offerExpiresAt,
    required this.service,
    required this.requestedStart,
    required this.providerPayMinor,
    this.requestReference,
    this.estate,
    this.serviceArea,
    this.requestedEnd,
    this.recurrenceRule,
    this.clientNotes,
  });

  factory Offer.fromJson(Json j) => Offer(
    id: j.str('id'),
    status: j.str('status'),
    offerExpiresAt: j.date('offer_expires_at') ?? DateTime.now(),
    service: Service.fromJson(j.obj('service') ?? const {}),
    requestedStart: j.date('requested_start') ?? DateTime.now(),
    providerPayMinor: j.intOr('provider_pay_minor'),
    requestReference: j.strOrNull('request_reference'),
    estate: j.strOrNull('estate'),
    serviceArea: j.model('service_area', ServiceArea.fromJson),
    requestedEnd: j.date('requested_end'),
    recurrenceRule: j.strOrNull('recurrence_rule'),
    clientNotes: j.strOrNull('client_notes'),
  );

  final String id;
  final String status;
  final DateTime offerExpiresAt;
  final Service service;
  final DateTime requestedStart;
  final int providerPayMinor;
  final String? requestReference;
  final String? estate;
  final ServiceArea? serviceArea;
  final DateTime? requestedEnd;
  final String? recurrenceRule;
  final String? clientNotes;
}

class Job {
  const Job({
    required this.assignmentId,
    required this.requestReference,
    required this.service,
    this.status,
    this.clientDisplayName,
    this.address,
    this.visits = const [],
    this.providerPayMinor = 0,
  });

  factory Job.fromJson(Json j) => Job(
    assignmentId: j.str('assignment_id'),
    requestReference: j.str('request_reference'),
    service: Service.fromJson(j.obj('service') ?? const {}),
    status: j.strOrNull('status'),
    clientDisplayName: j.strOrNull('client_display_name'),
    address: j.model('address', Address.fromJson),
    visits: j.list('visits', Visit.fromJson),
    providerPayMinor: j.intOr('provider_pay_minor'),
  );

  final String assignmentId;
  final String requestReference;
  final Service service;

  /// The request's status (`confirmed`, `in_progress`, …).
  final String? status;
  final String? clientDisplayName;
  final Address? address;
  final List<Visit> visits;
  final int providerPayMinor;

  Visit? get nextVisit {
    final open = visits.where((v) => v.status == 'scheduled' || v.status == 'checked_in').toList()
      ..sort((a, b) => a.scheduledStart.compareTo(b.scheduledStart));
    return open.isEmpty ? null : open.first;
  }

  /// The portal offers "the client wants more work" while the job is live.
  bool get canVary => status == 'confirmed' || status == 'in_progress';
}

class Appraisal {
  const Appraisal({
    this.totalScore,
    this.rating,
    this.complaints,
    this.spotChecks,
    this.reliability,
    this.periodEnd,
    this.history = const [],
  });

  factory Appraisal.fromJson(Json j) {
    final c = j.obj('components') ?? const {};
    return Appraisal(
      totalScore: j.numOrNull('total_score'),
      rating: c.numOrNull('rating'),
      complaints: c.numOrNull('complaints'),
      spotChecks: c.numOrNull('spot_checks'),
      reliability: c.numOrNull('reliability'),
      periodEnd: j.date('period_end'),
      history: j.list('history', (h) => (h.date('period_end') ?? DateTime.now(), h.numOrNull('total_score') ?? 0)),
    );
  }

  final double? totalScore;
  final double? rating;
  final double? complaints;
  final double? spotChecks;
  final double? reliability;
  final DateTime? periodEnd;
  final List<(DateTime, double)> history;
}

class SyncOutcome {
  const SyncOutcome({required this.clientEventId, required this.status, this.message});

  factory SyncOutcome.fromJson(Json j) => SyncOutcome(
    clientEventId: j.str('client_event_id'),
    status: j.str('status'),
    message: j.obj('error')?.strOrNull('message'),
  );

  final String clientEventId;

  /// `applied`, `duplicate` or `rejected`.
  final String status;
  final String? message;
}
