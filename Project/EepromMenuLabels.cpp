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
  const uint8_t commit = EEPROM.read(EepromLayout::MenuLabelsCommitAddress);
  uint8_t crc = EepromLayout::MenuLabelsFormatMarker;
  for (uint8_t index = 0; index < EepromLayout::MenuLabelBytes; ++index) {
    crc = ControllerProtocol::WireCodec::crc8Update(
        crc, EEPROM.read(EepromLayout::MenuLabelsAddress + index));
  }
  labelsAvailable = commit == EepromLayout::MenuLabelsFormatMarker &&
                    crc == EEPROM.read(EepromLayout::MenuLabelsCrcAddress);
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
    const uint8_t value = EEPROM.read(EepromLayout::MenuLabelsAddress +
                                      offset + character);
    output[character] = printable(value) ? static_cast<char>(value) : '-';
  }
}

} // namespace EepromMenuLabels

#endif
