#include <EEPROM.h>

#include "Project/EepromLayout.h"
#include "Project/EepromMenuLabels.h"
#include "Project/ProtocolCodec.h"

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

std::uint8_t labelCrc(const char *data, std::uint8_t length) {
	std::uint8_t crc = EepromLayout::MenuLabelsFormatMarker;
	while (length-- != 0) {
		crc = ControllerProtocol::WireCodec::crc8Update(
			crc, static_cast<std::uint8_t>(*data++));
	}
	return crc;
}

void beginLabelWrite(const char *labels) {
	EEPROM.update(EepromLayout::MenuLabelsCommitAddress, 0);
	for (std::uint8_t index = 0; index < EepromLayout::MenuLabelBytes;
       ++index) {
		EEPROM.update(EepromLayout::MenuLabelsAddress + index,
				  static_cast<std::uint8_t>(labels[index]));
	}
	EEPROM.update(EepromLayout::MenuLabelsCrcAddress,
				  labelCrc(labels, EepromLayout::MenuLabelBytes));
}

void commitLabelWrite() {
	EEPROM.update(EepromLayout::MenuLabelsCommitAddress,
				  EepromLayout::MenuLabelsFormatMarker);
}

void writeFactoryLabels() {
	beginLabelWrite(kFactoryLabels);
	commitLabelWrite();
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

void testVersionedRecordAndTornWriteStayUnavailable() {
	EEPROM.fill(0xFF);
	writeFactoryLabels();
	EepromMenuLabels::begin();
	require(EepromMenuLabels::available(),
			"factory EEPROM label block did not validate");
	require(labelCrc(kFactoryLabels, EepromLayout::MenuLabelBytes) == 0x06,
			"factory CRC vector drifted from the Go-compatible contract");

	char replacement[EepromLayout::MenuLabelBytes + 1] = {};
	for (std::uint8_t index = 0; index < EepromLayout::MenuLabelBytes;
		 ++index) {
		replacement[index] = kFactoryLabels[index];
	}
	replacement[0] = 'D';

	// Commit invalidation is deliberately first. Every torn prefix stays hidden,
	// including prefixes with all payload and CRC bytes already written.
	EEPROM.update(EepromLayout::MenuLabelsCommitAddress, 0);
	EepromMenuLabels::begin();
	require(!EepromMenuLabels::available(),
			"invalidated record remained available");
	for (std::uint8_t index = 0; index < EepromLayout::MenuLabelBytes;
		 ++index) {
		EEPROM.update(EepromLayout::MenuLabelsAddress + index,
				  static_cast<std::uint8_t>(replacement[index]));
		EepromMenuLabels::begin();
		require(!EepromMenuLabels::available(),
				"torn payload became available before commit");
	}
	EEPROM.update(EepromLayout::MenuLabelsCrcAddress,
			  labelCrc(replacement, EepromLayout::MenuLabelBytes));
	EepromMenuLabels::begin();
	require(!EepromMenuLabels::available(),
			"complete uncommitted record became available");
	commitLabelWrite();
	EepromMenuLabels::begin();
	require(EepromMenuLabels::available(),
			"committed versioned record did not become available");
	require(EepromMenuLabels::read(0, 0) == 'D',
			"committed replacement label was not rendered");

	EEPROM.update(EepromLayout::MenuLabelsCommitAddress,
			  static_cast<std::uint8_t>(EepromLayout::MenuLabelsFormatMarker + 1U));
	EepromMenuLabels::begin();
	require(!EepromMenuLabels::available(),
			"unknown record format marker became available");
}

void testCrcRejectsPrintableXorCollisions() {
	EEPROM.fill(0xFF);
	writeFactoryLabels();
	EEPROM.update(EepromLayout::MenuLabelsAddress,
			  static_cast<std::uint8_t>(kFactoryLabels[1]));
	EEPROM.update(EepromLayout::MenuLabelsAddress + 1,
			  static_cast<std::uint8_t>(kFactoryLabels[0]));
	EepromMenuLabels::begin();
	require(!EepromMenuLabels::available(),
			"printable label transposition became valid");

	writeFactoryLabels();
	EEPROM.update(EepromLayout::MenuLabelsAddress,
			  static_cast<std::uint8_t>(kFactoryLabels[0] ^ 0x01));
	EEPROM.update(EepromLayout::MenuLabelsAddress + 1,
			  static_cast<std::uint8_t>(kFactoryLabels[1] ^ 0x01));
	EepromMenuLabels::begin();
	require(!EepromMenuLabels::available(),
			"two-cell equal-delta corruption became valid");

	char nonPrintable[EepromLayout::MenuLabelBytes + 1] = {};
	for (std::uint8_t index = 0; index < EepromLayout::MenuLabelBytes;
		 ++index) {
		nonPrintable[index] = kFactoryLabels[index];
	}
	nonPrintable[7] = '\n';
	beginLabelWrite(nonPrintable);
	commitLabelWrite();
	EepromMenuLabels::begin();
	require(EepromMenuLabels::available(),
			"CRC-valid record lost its integrity state");
	require(EepromMenuLabels::read(1, 3) == '-',
			"non-printable record cell did not use safe fallback");
	char label[EepromMenuLabels::LabelWidth] = {};
	EepromMenuLabels::copy(1, label);
	require(label[3] == '-',
			"copy/read disagreed on non-printable cell fallback");
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
		testVersionedRecordAndTornWriteStayUnavailable();
		testCrcRejectsPrintableXorCollisions();
		testFactoryBlockReadsEveryPackedCell();
    std::cout << "eeprom_menu_labels_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "eeprom_menu_labels_tests: " << error.what() << '\n';
    return 1;
  }
}
