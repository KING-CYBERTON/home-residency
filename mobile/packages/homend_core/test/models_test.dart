import 'package:flutter_test/flutter_test.dart';
import 'package:homend_core/homend_core.dart';

void main() {
  test('ServiceRequest reads a full response and works out what the client can do', () {
    final r = ServiceRequest.fromJson({
      'id': 'r1',
      'reference': 'REQ-2026-000001',
      'status': 'confirmed',
      'service': {
        'id': 's1',
        'category_id': 'c1',
        'slug': 'deep-clean',
        'name': 'Deep clean',
        'pricing_unit': 'per_hour',
        'is_active': true,
      },
      'total_minor': 69600,
      'requested_start': '2026-10-14T14:00:00Z',
      'submitted_at': '2026-10-14T12:42:00Z',
      'address': {'id': 'a1', 'label': 'Home', 'estate': 'Kilimani Gardens', 'house_number': 'B4', 'is_default': true},
      'provider': {'id': 'p1', 'display_name': 'Grace W.', 'rating_avg': 4.8, 'rating_count': 12, 'completed_jobs': 30},
      'visits': [
        {
          'id': 'v1',
          'request_id': 'r1',
          'status': 'scheduled',
          'scheduled_start': '2026-10-14T14:00:00Z',
          'scheduled_end': '2026-10-14T18:00:00Z',
        },
      ],
      'some_future_field': 1,
    });
    expect(r.service.name, 'Deep clean');
    expect(r.address!.short, 'Home · Kilimani Gardens');
    expect(r.provider!.ratingAvg, 4.8);
    expect(r.visits.single.status, 'scheduled');
    expect(r.canCancel, isTrue);
    expect(r.needsClient, isFalse);
    expect(r.isActive, isTrue);
  });

  test('Invoice works out what is left to pay', () {
    final i = Invoice.fromJson({
      'id': 'i',
      'reference': 'INV-1',
      'status': 'partially_paid',
      'total_minor': 290000,
      'paid_minor': 90000,
    });
    expect(i.dueMinor, 200000);
    expect(i.isPayable, isTrue);
    expect(
      Invoice.fromJson({'id': 'i', 'reference': 'INV-1', 'status': 'paid', 'total_minor': 1, 'paid_minor': 1})
          .isPayable,
      isFalse,
    );
  });

  test('Application reads the summary and the detail shapes', () {
    final a = Application.fromJson({
      'id': 'a',
      'reference': 'APP-1',
      'category_id': 'c',
      'status': 'draft',
      'checklist_progress': {'satisfied': 2, 'total': 5},
      'items': [
        {
          'id': 'i1',
          'status': 'pending',
          'template_item': {
            'kind': 'document',
            'label': 'National ID',
            'is_mandatory': true,
            'document_type': 'national_id',
          },
        },
        {
          'id': 'i2',
          'status': 'pending',
          'template_item': {'kind': 'referee', 'label': 'Two referees', 'is_mandatory': true, 'min_count': 2},
        },
      ],
      'missing': ['National ID'],
    });
    expect(a.satisfied, 2);
    expect(a.isEditable, isTrue);
    expect(a.items.first.templateItem.takesUpload, isTrue);
    expect(a.items.last.templateItem.takesUpload, isFalse);
    expect(a.missing, ['National ID']);
  });

  test('Job picks the next open visit', () {
    final j = Job.fromJson({
      'assignment_id': 'as',
      'request_reference': 'REQ-1',
      'status': 'in_progress',
      'service': {'id': 's', 'category_id': 'c', 'name': 'Laundry', 'pricing_unit': 'per_job'},
      'visits': [
        {
          'id': 'v2',
          'request_id': 'r',
          'status': 'scheduled',
          'scheduled_start': '2026-10-20T08:00:00Z',
          'scheduled_end': '2026-10-20T12:00:00Z',
        },
        {
          'id': 'v1',
          'request_id': 'r',
          'status': 'completed',
          'scheduled_start': '2026-10-13T08:00:00Z',
          'scheduled_end': '2026-10-13T12:00:00Z',
        },
        {
          'id': 'v3',
          'request_id': 'r',
          'status': 'checked_in',
          'scheduled_start': '2026-10-16T08:00:00Z',
          'scheduled_end': '2026-10-16T12:00:00Z',
        },
      ],
    });
    expect(j.nextVisit!.id, 'v3');
    expect(j.canVary, isTrue);
  });
}
