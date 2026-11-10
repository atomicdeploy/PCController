#pragma once
#include "virtual_board/virtual_board.hpp"
#include <string>
#include <vector>

namespace pccontroller::virtual_board {
// Diagnostic observations only, never command acknowledgement or a wire parser.
std::vector<std::string> activityLines(const OutputSnapshot &before,
                                     const OutputSnapshot &after,
                                     bool pwm, bool peripherals,
                                     bool legacy = false);
}
