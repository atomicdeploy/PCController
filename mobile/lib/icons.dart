import 'package:flutter/widgets.dart';

/// Stable subset of the Phosphor regular icon vocabulary used by the app.
///
/// These codepoints come from the vendored upstream Phosphor font. Plain
/// [IconData] constants work with current Flutter releases, where IconData is
/// final and can no longer be subclassed by older icon packages.
abstract final class PakarIcons {
  static const arrowClockwise = IconData(0xe036, fontFamily: 'Phosphor');
  static const arrowCounterClockwise = IconData(0xe038, fontFamily: 'Phosphor');
  static const broadcast = IconData(0xe0f2, fontFamily: 'Phosphor');
  static const check = IconData(0xe182, fontFamily: 'Phosphor');
  static const caretDown = IconData(0xe136, fontFamily: 'Phosphor');
  static const circle = IconData(0xe18a, fontFamily: 'Phosphor');
  static const circuitry = IconData(0xe9c2, fontFamily: 'Phosphor');
  static const clock = IconData(0xe19a, fontFamily: 'Phosphor');
  static const cloud = IconData(0xe1aa, fontFamily: 'Phosphor');
  static const cpu = IconData(0xe610, fontFamily: 'Phosphor');
  static const floppyDisk = IconData(0xe248, fontFamily: 'Phosphor');
  static const gauge = IconData(0xe628, fontFamily: 'Phosphor');
  static const gear = IconData(0xe270, fontFamily: 'Phosphor');
  static const gridFour = IconData(0xe296, fontFamily: 'Phosphor');
  static const globe = IconData(0xe288, fontFamily: 'Phosphor');
  static const link = IconData(0xe2e2, fontFamily: 'Phosphor');
  static const lock = IconData(0xe2fa, fontFamily: 'Phosphor');
  static const wifiHigh = IconData(0xe4ea, fontFamily: 'Phosphor');
  static const keyboard = IconData(0xe2d8, fontFamily: 'Phosphor');
  static const lightning = IconData(0xe2de, fontFamily: 'Phosphor');
  static const musicNote = IconData(0xe33c, fontFamily: 'Phosphor');
  static const play = IconData(0xe3d0, fontFamily: 'Phosphor');
  static const radio = IconData(0xe77e, fontFamily: 'Phosphor');
  static const warning = IconData(0xe4e0, fontFamily: 'Phosphor');
}
