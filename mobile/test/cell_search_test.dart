import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/cell_search.dart';

void main() {
  test('Logical IDs lists ranges and duplicates are exact', () {
    expect(parseCellSearch(''), isEmpty);
    expect(parseCellSearch('12, 3–5, 12'), {3, 4, 5, 12});
    expect(parseCellSearch('1-80').length, 80);
  });
  test('Invalid searches never silently match a different cell', () {
    for (final text in ['0', '81', '8-3', '2--4', ',', 'physical 12', '-1']) {
      expect(() => parseCellSearch(text), throwsFormatException);
    }
  });
  test('RayanLamp numeric normalization and exact shared range formatting', () {
    expect(parseCellSearch('۱،٢;۳–۵ #۸۰'), {1, 2, 3, 4, 5, 80});
    expect(formatCellSelection(parseCellSearch('1,2,3')), '1–3');
    for (final cells in [
      <int>{},
      {1},
      {2, 3, 4, 12, 80},
      parseCellSearch('1–80'),
    ]) {
      expect(parseCellSearch(formatCellSelection(cells)), cells);
    }
    expect(() => parseCellSearch('1' * 257), throwsFormatException);
    expect(latinDigits('۱۲٣۴ name'), '1234 name');
  });
}
