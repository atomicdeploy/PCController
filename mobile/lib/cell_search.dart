import 'dart:convert';

// Adapted from RayanLamp internal/i18n/digits.go ShapeLatinDigits.
// Input-only normalization: never shape hostnames, passwords or peer names.
String latinDigits(String text) => String.fromCharCodes(
  text.runes.map((rune) {
    if (rune >= 0x06f0 && rune <= 0x06f9) return rune - 0x06f0 + 48;
    if (rune >= 0x0660 && rune <= 0x0669) return rune - 0x0660 + 48;
    return rune;
  }),
);

// Actual desktop/src/ui/cell_search.rs parse/format_selection contract.
// Searching changes the shared selection only, never physical output.
Set<int> parseCellSearch(String text) {
  if (utf8.encode(text).length > 256) {
    throw const FormatException('Search is too long');
  }
  final normalized = latinDigits(text)
      .replaceAll(RegExp('[–—]'), '-')
      .replaceAll('،', ',');
  final cells = <int>{};
  int number(String text) {
    final value = text.startsWith('#') ? text.substring(1) : text;
    final id = RegExp(r'^[0-9]+$').hasMatch(value) ? int.tryParse(value) : null;
    if (id == null || id < 1 || id > 80) {
      throw const FormatException('Use cell IDs 1–80 or ranges such as 20–28');
    }
    return id;
  }

  for (final token in normalized.split(RegExp(r'[,;\s]'))) {
    if (token.isEmpty) continue;
    final split = token.indexOf('-');
    if (split < 0) {
      cells.add(number(token));
    } else {
      final start = number(token.substring(0, split));
      final end = number(token.substring(split + 1));
      if (start > end) {
        throw const FormatException('Range start must not exceed its end');
      }
      cells.addAll(Iterable.generate(end - start + 1, (i) => start + i));
    }
  }
  if (text.trim().isNotEmpty && cells.isEmpty) {
    throw const FormatException('Enter a cell ID or range');
  }
  return cells;
}

String formatCellSelection(Set<int> selection) {
  final cells = selection.toList()..sort();
  final parts = <String>[];
  for (var i = 0; i < cells.length; ++i) {
    final start = cells[i];
    var end = start;
    while (i + 1 < cells.length && cells[i + 1] == end + 1) {
      end = cells[++i];
    }
    parts.add(start == end ? '$start' : '$start–$end');
  }
  return parts.join(', ');
}
