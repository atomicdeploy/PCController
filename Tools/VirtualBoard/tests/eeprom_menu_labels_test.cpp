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
  for (std::uint8_t character = 0; character < EepromMenuLabels::LabelWidth;
       ++character) {
    require(EepromMenuLabels::read(0, character) == '-',
            "erased EEPROM label byte did not use a safe fallback");
  }

  writeFactoryLabels();
  EEPROM.update(EepromLayout::MenuLabelsAddress + 3, 'X');
  EepromMenuLabels::begin();
  require(!EepromMenuLabels::available(),
          "checksum-corrupt EEPROM label block must not become available");
  require(EepromMenuLabels::read(3, 0) == '-',
          "corrupt EEPROM label byte did not use a safe fallback");
}

void testFactoryBlockReadsEveryPackedCell() {
  EEPROM.fill(0xFF);
  writeFactoryLabels();
  EepromMenuLabels::begin();
  require(EepromMenuLabels::available(),
          "factory EEPROM label block did not validate");
  for (std::uint8_t page = 0; page < EepromLayout::MenuLabelCount; ++page) {
    for (std::uint8_t character = 0;
         character < EepromMenuLabels::LabelWidth; ++character) {
      const std::uint8_t index = static_cast<std::uint8_t>(
          page * EepromMenuLabels::LabelWidth + character);
      require(EepromMenuLabels::read(page, character) == kFactoryLabels[index],
              "validated EEPROM label byte changed");
    }
  }
  require(EepromMenuLabels::read(EepromLayout::MenuLabelCount, 0) == '-',
          "out-of-range page did not use a safe fallback");
  require(EepromMenuLabels::read(0, EepromMenuLabels::LabelWidth) == '-',
          "out-of-range character did not use a safe fallback");
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
