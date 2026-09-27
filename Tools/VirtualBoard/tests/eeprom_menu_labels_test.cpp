#include <EEPROM.h>

#include "Project/EepromLayout.h"
#include "Project/EepromMenuLabels.h"

#include <cstdint>
#include <iostream>
#include <stdexcept>

namespace {

constexpr char kFactoryLabels[] =
    "doorVOLTCURRtLEDtBT LItEbEEPPWM rELYKEY uPWMr5-8MOVELErn";
static_assert(sizeof(kFactoryLabels) - 1 == EepromLayout::MenuLabelBytes,
              "test labels must match the EEPROM layout");

void require(bool condition, const char *message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

std::uint8_t xorChecksum(const char *data, std::uint8_t length) {
  std::uint8_t checksum = 0;
  while (length-- != 0) {
    checksum ^= static_cast<std::uint8_t>(*data++);
  }
  return checksum;
}

void writeFactoryLabels() {
  for (std::uint8_t index = 0; index < EepromLayout::MenuLabelBytes;
       ++index) {
    EEPROM.update(EepromLayout::MenuLabelsAddress + index,
                  static_cast<std::uint8_t>(kFactoryLabels[index]));
  }
  EEPROM.update(EepromLayout::MenuLabelsChecksumAddress,
                xorChecksum(kFactoryLabels, EepromLayout::MenuLabelBytes));
}

void testErasedAndCorruptBlocksFallBackSafely() {
  EEPROM.fill(0xFF);
  EepromMenuLabels::begin();
  require(!EepromMenuLabels::available(),
          "erased EEPROM label block must not become available");
  char label[EepromMenuLabels::LabelWidth];
  EepromMenuLabels::copy(0, label);
  for (std::uint8_t character = 0; character < EepromMenuLabels::LabelWidth;
       ++character) {
    require(label[character] == '-',
            "erased EEPROM labels did not use a safe fallback");
  }

  writeFactoryLabels();
  EEPROM.update(EepromLayout::MenuLabelsAddress + 3, 'X');
  EepromMenuLabels::begin();
  require(!EepromMenuLabels::available(),
          "checksum-corrupt EEPROM label block must not become available");
  EepromMenuLabels::copy(3, label);
  require(label[0] == '-', "corrupt EEPROM labels did not use a safe fallback");

  writeFactoryLabels();
  const std::uint8_t originalChecksum =
      EEPROM.read(EepromLayout::MenuLabelsChecksumAddress);
  EEPROM.update(EepromLayout::MenuLabelsAddress + 1, '\x01');
  EEPROM.update(EepromLayout::MenuLabelsChecksumAddress,
                static_cast<std::uint8_t>(originalChecksum ^
                                          kFactoryLabels[1] ^ '\x01'));
  EepromMenuLabels::begin();
  require(!EepromMenuLabels::available(),
          "non-printable EEPROM label block must not become available");
}

void testFactoryBlockReadsEveryPackedCell() {
  EEPROM.fill(0xFF);
  writeFactoryLabels();
  EepromMenuLabels::begin();
  require(EepromMenuLabels::available(),
          "factory EEPROM label block did not validate");
  for (std::uint8_t page = 0; page < EepromLayout::MenuLabelCount; ++page) {
    char label[EepromMenuLabels::LabelWidth];
    EepromMenuLabels::copy(page, label);
    for (std::uint8_t character = 0;
         character < EepromMenuLabels::LabelWidth; ++character) {
      const std::uint8_t index = static_cast<std::uint8_t>(
          page * EepromMenuLabels::LabelWidth + character);
      require(label[character] == kFactoryLabels[index],
              "validated EEPROM labels changed");
    }
  }
  char outOfRange[EepromMenuLabels::LabelWidth];
  EepromMenuLabels::copy(EepromLayout::MenuLabelCount, outOfRange);
  require(outOfRange[0] == '-', "out-of-range page did not use a safe fallback");
}

} // namespace

int main() {
  try {
    testErasedAndCorruptBlocksFallBackSafely();
    testFactoryBlockReadsEveryPackedCell();
    std::cout << "eeprom_menu_labels_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "eeprom_menu_labels_tests: " << error.what() << '\n';
    return 1;
  }
}
