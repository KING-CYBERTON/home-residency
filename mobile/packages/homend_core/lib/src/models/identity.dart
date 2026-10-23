import 'json.dart';

/// `Me`: the signed-in user, with provider and ID-verification status.
class Me {
  const Me({
    required this.id,
    required this.kind,
    required this.status,
    required this.fullName,
    this.phone,
    this.email,
    this.preferredLanguage = 'en',
    this.providerStatus,
    this.idVerification,
    this.hasPassword = false,
  });

  factory Me.fromJson(Json j) => Me(
    id: j.str('id'),
    kind: j.str('kind'),
    status: j.str('status'),
    fullName: j.str('full_name'),
    phone: j.strOrNull('phone'),
    email: j.strOrNull('email'),
    preferredLanguage: j.strOrNull('preferred_language') ?? 'en',
    providerStatus: j.strOrNull('provider_status'),
    idVerification: j.strOrNull('id_verification'),
    hasPassword: j.flag('has_password'),
  );

  final String id;

  /// `client`, `provider` or `staff`.
  final String kind;
  final String status;
  final String fullName;
  final String? phone;
  final String? email;
  final String preferredLanguage;

  /// `applicant`, `approved`, `active`, `suspended`, `disqualified` or `withdrawn`.
  final String? providerStatus;

  /// `none`, `pending`, `verified` or `rejected`.
  final String? idVerification;

  /// The account can sign in with its email and password.
  final bool hasPassword;

  String get firstName => fullName.split(' ').first;
}

class Session {
  const Session({required this.token, required this.expiresAt, required this.user});

  factory Session.fromJson(Json j) =>
      Session(token: j.str('token'), expiresAt: j.date('expires_at'), user: Me.fromJson(j.obj('user') ?? const {}));

  final String token;
  final DateTime? expiresAt;
  final Me user;
}

class OtpSent {
  const OtpSent({required this.otpId, this.expiresAt, this.resendAfterSeconds = 60});

  factory OtpSent.fromJson(Json j) => OtpSent(
    otpId: j.str('otp_id'),
    expiresAt: j.date('expires_at'),
    resendAfterSeconds: j.intOr('resend_after_seconds', 60),
  );

  final String otpId;
  final DateTime? expiresAt;
  final int resendAfterSeconds;
}

/// `ConsentInput`. Purposes: `privacy_notice`, `vetting_checks`, `health_data`, `marketing`.
class Consent {
  const Consent(this.purpose, this.noticeVersion, {this.granted = true});

  final String purpose;
  final String noticeVersion;
  final bool granted;

  Json toJson() => {'purpose': purpose, 'notice_version': noticeVersion, 'granted': granted};
}
