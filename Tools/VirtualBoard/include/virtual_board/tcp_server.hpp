#pragma once

#include "virtual_board/virtual_board.hpp"

#include <atomic>
#include <cstdint>
#include <string>

namespace pccontroller::virtual_board {

struct TcpServerOptions {
  std::string bindAddress = "127.0.0.1";
  std::uint16_t port = 8765;
  bool quiet = false;
};

void runTcpServer(VirtualBoard &board, const TcpServerOptions &options,
                  std::atomic<bool> &stopRequested);

} // namespace pccontroller::virtual_board
