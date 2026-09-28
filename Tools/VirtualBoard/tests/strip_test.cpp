#include "Project/AddressableLeds.h"
#include "ProjectConfig.h"
#include <array>
#include <iostream>
#include <stdexcept>

static void require(bool condition, const char *message) {
  if (!condition) throw std::runtime_error(message);
}

int main() {
  try {
    std::array<uint8_t, 302> storage{};
    storage.front() = 0xAB;
    storage.back() = 0xCD;
    uint8_t *wire = storage.data() + 1;
    AddressableLeds::bindWorkspace(wire);
    AddressableLeds::begin();
    require(AddressableLeds::count() == 100, "default count");
    uint8_t chunk[] = {0xFD, 99, 255, 128, 64};
    require(AddressableLeds::apply(chunk, sizeof(chunk)), "last pixel stage");
#if PCCONTROLLER_USE_WS2812B
    require(wire[297] == 128 && wire[298] == 255 && wire[299] == 64, "exact GRB wire bytes");
#else
    require(wire[297] == 64 && wire[298] == 255 && wire[299] == 128, "exact BRG wire bytes");
#endif
    chunk[1] = 100;
    require(!AddressableLeds::apply(chunk, sizeof(chunk)), "out of range pixel accepted");
    const uint8_t invalid[] = {0xFD, 0, 1, 2, 3, 4};
    const auto beforeInvalid = storage;
    require(!AddressableLeds::apply(invalid, sizeof(invalid)), "partial RGB accepted");
    require(storage == beforeInvalid, "invalid chunk partially changed frame");
    const uint8_t configure[] = {0xFE, 3};
    require(AddressableLeds::apply(configure, sizeof(configure)), "configure failed");
    require(AddressableLeds::count() == 3, "count changed incorrectly");
    for (unsigned i = 0; i < 300; ++i) require(wire[i] == 0, "old tail not cleared");
    const uint8_t fill[] = {0xFF, 1, 2, 3, 255};
    require(AddressableLeds::apply(fill, sizeof(fill)), "fill failed");
    require(wire[9] == 0, "fill exceeded configured count");
    require(!AddressableLeds::configure(0) && !AddressableLeds::configure(101), "invalid count accepted");
    require(storage.front() == 0xAB && storage.back() == 0xCD, "workspace bounds corrupted");
    std::cout << "strip_tests: exact colors, bounds, chunks, configuration passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
