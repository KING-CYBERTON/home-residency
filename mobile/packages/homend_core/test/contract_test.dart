// Checks the Dart API client against the backend contract, `backend/api/openapi.yaml`.
//
// Every call in homend_api.dart must hit an operation that exists with the same method, every
// body the apps send must only use properties the spec defines, and every client or provider
// operation in the spec must be used by the apps or listed below as deliberately unused.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:homend_core/homend_core.dart';
import 'package:path/path.dart' as p;
import 'package:yaml/yaml.dart';

final specFile = File(p.join('..', '..', '..', 'backend', 'api', 'openapi.yaml'));
final apiSource = File(p.join('lib', 'src', 'api', 'homend_api.dart'));

/// Operations the mobile apps do not call, and why.
const unused = {
  'GET /healthz': 'ops only',
  'POST /auth/staff/login': 'staff use the console',
  'POST /auth/staff/mfa': 'staff use the console',
  'GET /public/pages/{}': 'public website content',
  'GET /public/faqs': 'public website content',
  'POST /public/leads': 'public website form',
  'GET /documents/{}/download-url': 'apps do not re-open vault files',
  'POST /push-subscriptions': 'web push is for the PWA; native push is a later step',
  'DELETE /push-subscriptions': 'web push is for the PWA; native push is a later step',
  'GET /provider/checklists/{}': 'the application carries its own checklist items',
};

String norm(String path) => path.replaceAll(RegExp(r'\{[^}]+\}'), '{}');

void main() {
  late YamlMap spec;
  late Set<String> specOps;

  setUpAll(() {
    spec = loadYaml(specFile.readAsStringSync()) as YamlMap;
    specOps = {
      for (final e in (spec['paths'] as YamlMap).entries)
        for (final m in (e.value as YamlMap).keys)
          if (const {'get', 'post', 'put', 'patch', 'delete'}.contains(m))
            '${(m as String).toUpperCase()} ${norm(e.key as String)}',
    };
  });

  Set<String> clientOps() {
    final src = apiSource.readAsStringSync();
    final call = RegExp(r"_c\.(get|post|put|patch|delete)\(\s*'([^']+)'");
    return {
      for (final m in call.allMatches(src))
        '${m.group(1)!.toUpperCase()} ${m.group(2)!.replaceAll(RegExp(r'\$\{[^}]+\}|\$[a-zA-Z_]+'), '{}')}',
    };
  }

  test('every call in HomendApi exists in the OpenAPI spec', () {
    final missing = clientOps().difference(specOps);
    expect(missing, isEmpty, reason: 'Not in openapi.yaml: $missing');
  });

  test('every client and provider operation is used or listed as unused', () {
    final relevant = specOps.where((op) => !op.contains(' /console/') && !op.contains(' /webhooks/'));
    final notCovered = relevant.toSet().difference(clientOps()).difference(unused.keys.toSet());
    expect(notCovered, isEmpty, reason: 'Add these to HomendApi or to `unused`: $notCovered');
  });

  group('request bodies only use properties the spec defines', () {
    Set<String> props(String schema) {
      final s = (spec['components'] as YamlMap)['schemas'][schema] as YamlMap;
      return {...((s['properties'] as YamlMap?)?.keys.cast<String>() ?? const <String>[])};
    }

    void check(String schema, Map<String, dynamic> body) =>
        expect(body.keys.toSet().difference(props(schema)), isEmpty, reason: '$schema has no such field');

    test(
      'AddressInput',
      () => check(
        'AddressInput',
        const AddressInput(
          label: 'Home',
          serviceAreaId: 'a',
          estate: 'e',
          street: 's',
          houseNumber: 'h',
          landmark: 'l',
          lat: 1,
          lng: 2,
          accessDetails: 'gate',
          isDefault: true,
        ).toJson(),
      ),
    );
    test(
      'ServiceRequestCreate',
      () => check(
        'ServiceRequestCreate',
        ServiceRequestCreate(
          serviceId: 's',
          addressId: 'a',
          requestedStart: DateTime(2026),
          quantity: 2,
          requestedEnd: DateTime(2026, 1, 2),
          recurrenceRule: 'r',
          clientNotes: 'n',
        ).toJson(),
      ),
    );
    test(
      'RatingCreate',
      () => check(
        'RatingCreate',
        const RatingInput(stars: 5, visitId: 'v', punctuality: 4, quality: 5, conduct: 5, comment: 'Great').toJson(),
      ),
    );
    test(
      'ComplaintCreate',
      () => check(
        'ComplaintCreate',
        const ComplaintInput(
          severity: 'quality',
          summary: 's',
          requestId: 'r',
          visitId: 'v',
          details: 'd',
          evidenceDocumentIds: ['x'],
        ).toJson(),
      ),
    );
    test(
      'ProviderProfileUpdate',
      () => check(
        'ProviderProfileUpdate',
        const ProviderProfileUpdate(
          homeServiceAreaId: 'a',
          serviceAreaIds: ['a'],
          bio: 'b',
          yearsExperience: 3,
          languages: ['en'],
          payoutMsisdn: '+254700000000',
        ).toJson(),
      ),
    );
    test(
      'RefereeInput',
      () => check(
        'RefereeInput',
        RefereeInput(
          fullName: 'n',
          relationship: 'r',
          phone: 'p',
          employerName: 'e',
          employmentFrom: DateTime(2020),
          employmentTo: DateTime(2021),
        ).toJson(),
      ),
    );
    test('ConsentInput', () => check('ConsentInput', const Consent('privacy_notice', '2026-10').toJson()));
    test('Availability slot', () {
      final weekly =
          ((spec['components'] as YamlMap)['schemas']['Availability']['properties']['weekly']['items']
                  as YamlMap)['properties']
              as YamlMap;
      final body = const WeeklySlot(dayOfWeek: 1, startTime: '08:00', endTime: '17:00').toJson();
      expect(body.keys.toSet().difference(weekly.keys.cast<String>().toSet()), isEmpty);
    });
    test('SyncEvent', () {
      final props0 = props('SyncEvent');
      for (final k in ['client_event_id', 'visit_id', 'event_type', 'payload', 'device_recorded_at']) {
        expect(props0, contains(k));
      }
    });
    test('CheckInInput carries what a queued check-in sends', () {
      expect(props('CheckInInput'), containsAll(['method', 'code', 'lat', 'lng']));
      expect(props('CheckOutInput'), containsAll(['notes', 'lat', 'lng']));
    });
  });
}
