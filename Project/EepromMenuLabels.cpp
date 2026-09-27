#include "EepromMenuLabels.h"

#include "../ProjectConfig.h"

#if PCCONTROLLER_ENABLE_EEPROM_MENU_LABELS

#include <EEPROM.h>

#include "EepromLayout.h"
#include "ProtocolCodec.h"

namespace EepromMenuLabels {
namespace {

bool labelsAvailable = false;

} // namespace

void begin() {
  uint8_t checksum = 0;
  bool printable = true;
  for (uint8_t index = 0; index < EepromLayout::MenuLabelBytes; ++index) {
    const uint8_t value = EEPROM.read(EepromLayout::MenuLabelsAddress + index);
    checksum ^= value;
    printable = printable && value >= ' ' && value <= '~';
  }
  labelsAvailable = printable &&
                    checksum == EEPROM.read(EepromLayout::MenuLabelsChecksumAddress);
}

bool available() { return labelsAvailable; }

void copy(uint8_t page, char output[LabelWidth]) {
  if (!labelsAvailable || page >= EepromLayout::MenuLabelCount) {
    for (uint8_t character = 0; character < LabelWidth; ++character) {
      output[character] = '-';
    }
    return;
  }
  const uint8_t offset = static_cast<uint8_t>(page << 2);
  for (uint8_t character = 0; character < LabelWidth; ++character) {
    output[character] = static_cast<char>(EEPROM.read(
        EepromLayout::MenuLabelsAddress + offset + character));
  }
}

} // namespace EepromMenuLabels

#endif
