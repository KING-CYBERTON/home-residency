import 'dart:typed_data';

import '../models/bookings.dart';
import '../models/catalogue.dart';
import '../models/identity.dart';
import '../models/inbox.dart';
import '../models/json.dart';
import '../models/money.dart';
import '../models/provider.dart';
import 'api_client.dart';

Json _j(dynamic v) => (v as Map).cast<String, dynamic>();
List<T> _l<T>(dynamic v, T Function(Json) f) =>
    (v as List).whereType<Map>().map((e) => f(e.cast<String, dynamic>())).toList();

/// Typed calls for every non-console operation in `backend/api/openapi.yaml`.
///
/// Every path here is checked against the spec by `test/contract_test.dart`, so a renamed or
/// removed endpoint fails CI instead of failing on someone's phone.
class HomendApi {
  HomendApi(this._c);

  final ApiClient _c;
  ApiClient get client => _c;

  // ---- Auth

  /// [purpose] is `login`, `register` or `verify_phone`. [phone] is E.164, e.g. `+254712345678`.
  Future<OtpSent> requestOtp(String phone, {String purpose = 'login'}) async =>
      OtpSent.fromJson(_j(await _c.post('/auth/otp/request', body: {'phone': phone, 'purpose': purpose})));

  Future<Session> verifyOtp({
    required String otpId,
    required String code,
    String? fullName,
    String? kind,
    List<Consent> consents = const [],
  }) async => Session.fromJson(
    _j(
      await _c.post(
        '/auth/otp/verify',
        body: compact({
          'otp_id': otpId,
          'code': code,
          'full_name': fullName,
          'kind': kind,
          'consents': consents.isEmpty ? null : consents.map((c) => c.toJson()).toList(),
        }),
      ),
    ),
  );

  Future<void> logout() => _c.post('/auth/logout');

  Future<Me> me() async => Me.fromJson(_j(await _c.get('/me')));

  Future<Me> updateMe({String? fullName, String? email, String? preferredLanguage}) async => Me.fromJson(
    _j(
      await _c.patch('/me', compact({'full_name': fullName, 'email': email, 'preferred_language': preferredLanguage})),
    ),
  );

  Future<void> recordConsents(List<Consent> consents) =>
      _c.post('/me/consents', body: consents.map((c) => c.toJson()).toList());

  // ---- Public catalogue

  Future<List<ServiceArea>> serviceAreas() async => _l(await _c.get('/public/service-areas'), ServiceArea.fromJson);

  Future<List<Category>> categories() async => _l(await _c.get('/public/categories'), Category.fromJson);

  Future<CategoryDetail> category(String slug) async =>
      CategoryDetail.fromJson(_j(await _c.get('/public/categories/${Uri.encodeComponent(slug)}')));

  Future<List<Service>> rateCard({String? serviceAreaId, String? categoryId}) async => _l(
    await _c.get('/public/rate-card', query: {'service_area_id': serviceAreaId, 'category_id': categoryId}),
    Service.fromJson,
  );

  Future<List<SubscriptionPlan>> subscriptionPlans() async =>
      _l(await _c.get('/public/subscription-plans'), SubscriptionPlan.fromJson);

  // ---- Documents (vault)

  /// Creates the vault record, PUTs the bytes to the presigned URL, then confirms.
  Future<Document> uploadDocument({
    required String documentType,
    required String mimeType,
    required Uint8List bytes,
    String? filename,
    DateTime? issuedOn,
    DateTime? expiresOn,
  }) async {
    final ticket = UploadTicket.fromJson(
      _j(
        await _c.post(
          '/documents/uploads',
          body: compact({
            'document_type': documentType,
            'mime_type': mimeType,
            'size_bytes': bytes.length,
            'original_filename': filename,
            'issued_on': issuedOn == null ? null : isoDate(issuedOn),
            'expires_on': expiresOn == null ? null : isoDate(expiresOn),
          }),
        ),
      ),
    );
    final headers = {'Content-Type': mimeType, ...ticket.uploadHeaders};
    await _c.putBytes(Uri.parse(_c.baseUrl).resolve(ticket.uploadUrl).toString(), bytes, headers);
    final id = ticket.document.id;
    return Document.fromJson(_j(await _c.post('/documents/$id/complete')));
  }

  Future<Document> document(String id) async => Document.fromJson(_j(await _c.get('/documents/$id')));

  // ---- Notifications and messages

  Future<ApiPage<AppNotification>> notifications({bool? unread, String? cursor, int limit = 25}) async =>
      ApiPage.fromJson(
        _j(await _c.get('/notifications', query: {'unread': unread, 'cursor': cursor, 'limit': limit})),
        AppNotification.fromJson,
      );

  Future<void> markNotificationRead(String id) => _c.post('/notifications/$id/read');

  Future<void> markAllNotificationsRead() => _c.post('/notifications/read-all');

  Future<ApiPage<Conversation>> conversations({String? cursor, int limit = 25}) async => ApiPage.fromJson(
    _j(await _c.get('/conversations', query: {'cursor': cursor, 'limit': limit})),
    Conversation.fromJson,
  );

  Future<Conversation> startConversation({
    required String message,
    String? subject,
    String? requestId,
    String? complaintId,
  }) async => Conversation.fromJson(
    _j(
      await _c.post(
        '/conversations',
        body: compact({'message': message, 'subject': subject, 'request_id': requestId, 'complaint_id': complaintId}),
      ),
    ),
  );

  Future<ApiPage<Message>> messages(String conversationId, {String? cursor, int limit = 50}) async => ApiPage.fromJson(
    _j(await _c.get('/conversations/$conversationId/messages', query: {'cursor': cursor, 'limit': limit})),
    Message.fromJson,
  );

  Future<Message> sendMessage(String conversationId, String body) async =>
      Message.fromJson(_j(await _c.post('/conversations/$conversationId/messages', body: {'body': body})));

  // ---- Client: addresses and ID

  Future<List<Address>> addresses() async => _l(await _c.get('/client/addresses'), Address.fromJson);

  Future<Address> addAddress(AddressInput input) async =>
      Address.fromJson(_j(await _c.post('/client/addresses', body: input.toJson())));

  Future<Address> updateAddress(String id, AddressInput input) async =>
      Address.fromJson(_j(await _c.patch('/client/addresses/$id', input.toJson())));

  Future<void> archiveAddress(String id) => _c.delete('/client/addresses/$id');

  /// Returns the new verification status (`pending`).
  Future<String> submitIdVerification({required String nationalIdNumber, required String documentId}) async => _j(
    await _c.post('/client/id-verification', body: {'national_id_number': nationalIdNumber, 'document_id': documentId}),
  ).str('status');

  // ---- Client: requests

  Future<Quote> quote({
    required String serviceId,
    required String addressId,
    double? quantity,
    DateTime? requestedStart,
  }) async => Quote.fromJson(
    _j(
      await _c.post(
        '/client/quotes',
        body: compact({
          'service_id': serviceId,
          'address_id': addressId,
          'quantity': quantity,
          'requested_start': requestedStart == null ? null : isoDateTime(requestedStart),
        }),
      ),
    ),
  );

  Future<ServiceRequest> createRequest(ServiceRequestCreate input, {required String idempotencyKey}) async =>
      ServiceRequest.fromJson(
        _j(await _c.post('/client/requests', body: input.toJson(), idempotencyKey: idempotencyKey)),
      );

  Future<ApiPage<ServiceRequest>> requests({String? status, String? cursor, int limit = 25}) async => ApiPage.fromJson(
    _j(await _c.get('/client/requests', query: {'status': status, 'cursor': cursor, 'limit': limit})),
    ServiceRequest.fromJson,
  );

  Future<ServiceRequest> request(String id) async => ServiceRequest.fromJson(_j(await _c.get('/client/requests/$id')));

  Future<ServiceRequest> cancelRequest(String id, String reason) async =>
      ServiceRequest.fromJson(_j(await _c.post('/client/requests/$id/cancel', body: {'reason': reason})));

  Future<List<ShortlistEntry>> shortlist(String requestId) async =>
      _l(await _c.get('/client/requests/$requestId/shortlist'), ShortlistEntry.fromJson);

  Future<ServiceRequest> chooseFromShortlist(String requestId, String entryId) async =>
      ServiceRequest.fromJson(_j(await _c.post('/client/requests/$requestId/shortlist/$entryId/select')));

  Future<CheckinCode> doorCode(String visitId) async =>
      CheckinCode.fromJson(_j(await _c.get('/client/visits/$visitId/checkin-code')));

  Future<Visit> confirmVisit(String visitId) async =>
      Visit.fromJson(_j(await _c.post('/client/visits/$visitId/confirm')));

  Future<void> rate(String requestId, RatingInput input) =>
      _c.post('/client/requests/$requestId/rating', body: input.toJson());

  Future<List<Variation>> requestVariations(String requestId) async =>
      _l(await _c.get('/client/requests/$requestId/variations'), Variation.fromJson);

  Future<Variation> approveVariation(String id) async =>
      Variation.fromJson(_j(await _c.post('/client/variations/$id/approve')));

  Future<Variation> rejectVariation(String id, String reason) async =>
      Variation.fromJson(_j(await _c.post('/client/variations/$id/reject', body: {'reason': reason})));

  // ---- Client: money

  Future<ApiPage<Invoice>> invoices({String? status, String? cursor, int limit = 25}) async => ApiPage.fromJson(
    _j(await _c.get('/client/invoices', query: {'status': status, 'cursor': cursor, 'limit': limit})),
    Invoice.fromJson,
  );

  Future<Invoice> invoice(String id) async => Invoice.fromJson(_j(await _c.get('/client/invoices/$id')));

  /// Sends the M-Pesa STK prompt. Poll [payment] until [Payment.isFinal].
  Future<Payment> payInvoice(String invoiceId, {required String phone, required String idempotencyKey}) async =>
      Payment.fromJson(
        _j(await _c.post('/client/invoices/$invoiceId/pay', body: {'phone': phone}, idempotencyKey: idempotencyKey)),
      );

  Future<Payment> payment(String id) async => Payment.fromJson(_j(await _c.get('/client/payments/$id')));

  /// Null when the household has no plan (404).
  Future<Subscription?> subscription() async {
    try {
      return Subscription.fromJson(_j(await _c.get('/client/subscription')));
    } on ApiException catch (e) {
      if (e.statusCode == 404) return null;
      rethrow;
    }
  }

  Future<Subscription> subscribe(String planId, {String? payPhone, required String idempotencyKey}) async =>
      Subscription.fromJson(
        _j(
          await _c.post(
            '/client/subscription',
            body: compact({'plan_id': planId, 'pay_phone': payPhone}),
            idempotencyKey: idempotencyKey,
          ),
        ),
      );

  Future<Subscription> cancelSubscription() async =>
      Subscription.fromJson(_j(await _c.post('/client/subscription/cancel')));

  // ---- Client: complaints

  Future<Complaint> raiseComplaint(ComplaintInput input) async =>
      Complaint.fromJson(_j(await _c.post('/client/complaints', body: input.toJson())));

  Future<ApiPage<Complaint>> clientComplaints({String? cursor, int limit = 25}) async => ApiPage.fromJson(
    _j(await _c.get('/client/complaints', query: {'cursor': cursor, 'limit': limit})),
    Complaint.fromJson,
  );

  Future<Complaint> clientComplaint(String id) async => Complaint.fromJson(_j(await _c.get('/client/complaints/$id')));

  // ---- Provider: profile and onboarding

  Future<ProviderProfile> providerProfile() async => ProviderProfile.fromJson(_j(await _c.get('/provider/profile')));

  Future<ProviderProfile> updateProviderProfile(ProviderProfileUpdate input) async =>
      ProviderProfile.fromJson(_j(await _c.patch('/provider/profile', input.toJson())));

  Future<List<Application>> applications() async => _l(await _c.get('/provider/applications'), Application.fromJson);

  Future<Application> startApplication(String categoryId) async =>
      Application.fromJson(_j(await _c.post('/provider/applications', body: {'category_id': categoryId})));

  Future<Application> application(String id) async =>
      Application.fromJson(_j(await _c.get('/provider/applications/$id')));

  Future<Application> attachDocument(
    String applicationId, {
    required String itemId,
    required String documentId,
  }) async => Application.fromJson(
    _j(
      await _c.patch('/provider/applications/$applicationId', {
        'item_documents': [
          {'application_item_id': itemId, 'document_id': documentId},
        ],
      }),
    ),
  );

  Future<Referee> addReferee(String applicationId, RefereeInput input) async =>
      Referee.fromJson(_j(await _c.post('/provider/applications/$applicationId/referees', body: input.toJson())));

  Future<void> removeReferee(String applicationId, String refereeId) =>
      _c.delete('/provider/applications/$applicationId/referees/$refereeId');

  /// 422 with `details.missing` when mandatory items are outstanding.
  Future<Application> submitApplication(String id) async =>
      Application.fromJson(_j(await _c.post('/provider/applications/$id/submit')));

  Future<Application> withdrawApplication(String id) async =>
      Application.fromJson(_j(await _c.post('/provider/applications/$id/withdraw')));

  Future<List<Document>> providerDocuments() async => _l(await _c.get('/provider/documents'), Document.fromJson);

  Future<List<TrainingEnrolment>> trainings() async =>
      _l(await _c.get('/provider/trainings'), TrainingEnrolment.fromJson);

  Future<Contract> contract() async => Contract.fromJson(_j(await _c.get('/provider/contract')));

  /// Signed with a `verify_phone` code sent to the provider's own number.
  Future<Contract> signContract(Contract c, {required String otpId, required String code}) async => Contract.fromJson(
    _j(
      await _c.post(
        '/provider/contract/sign',
        body: {
          'contract_version': c.contractVersion,
          'code_of_conduct_version': c.codeOfConductVersion,
          'otp_id': otpId,
          'code': code,
        },
      ),
    ),
  );

  // ---- Provider: availability

  Future<List<WeeklySlot>> availability() async =>
      _j(await _c.get('/provider/availability')).list('weekly', WeeklySlot.fromJson);

  Future<List<WeeklySlot>> setAvailability(List<WeeklySlot> weekly) async =>
      _j(await _c.put('/provider/availability', {'weekly': weekly.map((s) => s.toJson()).toList()}))
          .list('weekly', WeeklySlot.fromJson);

  Future<List<TimeOff>> timeOff() async => _l(await _c.get('/provider/time-off'), TimeOff.fromJson);

  Future<TimeOff> addTimeOff(DateTime startsAt, DateTime endsAt, {String? reason}) async => TimeOff.fromJson(
    _j(
      await _c.post(
        '/provider/time-off',
        body: compact({'starts_at': isoDateTime(startsAt), 'ends_at': isoDateTime(endsAt), 'reason': reason}),
      ),
    ),
  );

  Future<void> removeTimeOff(String id) => _c.delete('/provider/time-off/$id');

  // ---- Provider: work

  Future<List<Offer>> offers() async => _l(await _c.get('/provider/offers'), Offer.fromJson);

  Future<Job> acceptOffer(String id, {required String idempotencyKey}) async =>
      Job.fromJson(_j(await _c.post('/provider/offers/$id/accept', idempotencyKey: idempotencyKey)));

  Future<void> declineOffer(String id, {String? reason}) =>
      _c.post('/provider/offers/$id/decline', body: compact({'reason': reason}));

  Future<ApiPage<Job>> jobs({String? status, String? cursor, int limit = 25}) async => ApiPage.fromJson(
    _j(await _c.get('/provider/jobs', query: {'status': status, 'cursor': cursor, 'limit': limit})),
    Job.fromJson,
  );

  Future<Job> job(String assignmentId) async => Job.fromJson(_j(await _c.get('/provider/jobs/$assignmentId')));

  Future<List<Visit>> providerVisits({DateTime? from, DateTime? to}) async => _l(
    await _c.get(
      '/provider/visits',
      query: {'from': from == null ? null : isoDate(from), 'to': to == null ? null : isoDate(to)},
    ),
    Visit.fromJson,
  );

  /// Check-in and check-out go through [sync] so an event made without signal is applied once,
  /// at the time it happened. These direct calls exist for completeness.
  Future<Visit> checkIn(String visitId, Json event) async =>
      Visit.fromJson(_j(await _c.post('/provider/visits/$visitId/check-in', body: event)));

  Future<Visit> checkOut(String visitId, Json event) async =>
      Visit.fromJson(_j(await _c.post('/provider/visits/$visitId/check-out', body: event)));

  /// `SyncBatch` of 1 to 200 events.
  Future<List<SyncOutcome>> sync(List<Json> events) async =>
      _j(await _c.post('/provider/sync', body: {'events': events})).list('results', SyncOutcome.fromJson);

  Future<Variation> proposeVariation(
    String assignmentId, {
    required String description,
    double? quantity,
    String? visitId,
  }) async => Variation.fromJson(
    _j(
      await _c.post(
        '/provider/jobs/$assignmentId/variations',
        body: compact({'description': description, 'quantity': quantity, 'visit_id': visitId}),
      ),
    ),
  );

  // ---- Provider: pay and quality

  Future<ApiPage<Earning>> earnings({String? status, String? cursor, int limit = 25}) async => ApiPage.fromJson(
    _j(await _c.get('/provider/earnings', query: {'status': status, 'cursor': cursor, 'limit': limit})),
    Earning.fromJson,
  );

  Future<EarningsSummary> earningsSummary() async =>
      EarningsSummary.fromJson(_j(await _c.get('/provider/earnings/summary')));

  Future<ApiPage<Payout>> payouts({String? cursor, int limit = 25}) async => ApiPage.fromJson(
    _j(await _c.get('/provider/payouts', query: {'cursor': cursor, 'limit': limit})),
    Payout.fromJson,
  );

  Future<Appraisal> appraisal() async => Appraisal.fromJson(_j(await _c.get('/provider/appraisal')));

  Future<List<Complaint>> providerComplaints() async => _l(await _c.get('/provider/complaints'), Complaint.fromJson);

  Future<Complaint> giveAccount(String complaintId, String account, {List<String> documentIds = const []}) async =>
      Complaint.fromJson(
        _j(
          await _c.post(
            '/provider/complaints/$complaintId/account',
            body: compact({'account': account, 'document_ids': documentIds.isEmpty ? null : documentIds}),
          ),
        ),
      );
}
