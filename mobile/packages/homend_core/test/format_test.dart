import 'package:flutter_test/flutter_test.dart';
import 'package:homend_core/homend_core.dart';

void main() {
  test('kes formats integer cents like the portals', () {
    expect(kes(250000), 'KES 2,500');
    expect(kes(69600), 'KES 696');
    expect(kes(1234567), 'KES 12,345.67');
    expect(kes(0), 'KES 0');
    expect(kes(-50000), '−KES 500');
  });

  test('Kenyan phone numbers normalise to E.164', () {
    expect(normalizeKenyanPhone('0781 334 473'), '+254781334473');
    expect(normalizeKenyanPhone('781334473'), '+254781334473');
    expect(normalizeKenyanPhone('254712345678'), '+254712345678');
    expect(normalizeKenyanPhone('+254712345678'), '+254712345678');
    expect(normalizeKenyanPhone('0110 123 456'), '+254110123456');
    expect(normalizeKenyanPhone('12345'), isNull);
    expect(displayPhone('+254781334473'), '0781 334 473');
  });

  test('labels read like the portal badges', () {
    expect(labelOf('submitted'), 'Received');
    expect(labelOf('issued'), 'To pay');
    expect(labelOf('something_new'), 'Something new');
  });
}
