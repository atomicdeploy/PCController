#include "virtual_board/hardware.hpp"
#include "virtual_board/tcp_server.hpp"
#include "virtual_board/virtual_board.hpp"

#include <atomic>
#include <csignal>
#include <cstdint>
#include <cstdlib>
#include <filesystem>
#include <iostream>
#include <stdexcept>
#include <string>
#include <thread>

namespace {

std::atomic<bool> *gStopRequested = nullptr;

void stopSignal(int) {
  if (gStopRequested != nullptr) {
    gStopRequested->store(true);
  }
}

struct Options {
  pccontroller::virtual_board::TcpServerOptions server;
  std::filesystem::path eeprom = "virtual-mcu-eeprom.bin";
  bool stdinEnabled = true;
};

std::uint16_t parsePort(const std::string &value) {
  std::size_t used = 0;
  const unsigned long parsed = std::stoul(value, &used, 10);
  if (used != value.size() || parsed == 0 || parsed > 65535) {
    throw std::invalid_argument("port must be in 1..65535");
  }
  return static_cast<std::uint16_t>(parsed);
}

void printHelp() {
  std::cout
      << "PCController Virtual Board\n\n"
      << "Usage: virtual_board [options]\n\n"
      << "  --bind ADDRESS    IPv4 listen address (default 127.0.0.1)\n"
      << "  --port PORT       TCP listen port (default 8765)\n"
      << "  --eeprom FILE     virtual MCU EEPROM image\n"
      << "  --no-stdin        disable interactive hardware controls\n"
      << "  --quiet           suppress connection messages\n"
      << "  --help            show this help\n\n"
      << "The endpoint is tcp://127.0.0.1:8765 by default. Type `help` on "
         "stdin for hardware controls.\n";
}

Options parseOptions(int argc, char **argv) {
  Options options;
  for (int index = 1; index < argc; ++index) {
    const std::string argument = argv[index];
    auto requireValue = [&](const char *name) -> std::string {
      if (++index >= argc) {
        throw std::invalid_argument(std::string(name) + " requires a value");
      }
      return argv[index];
    };
    if (argument == "--bind") {
      options.server.bindAddress = requireValue("--bind");
    } else if (argument == "--port") {
      options.server.port = parsePort(requireValue("--port"));
    } else if (argument == "--eeprom") {
      options.eeprom = requireValue("--eeprom");
    } else if (argument == "--no-stdin") {
      options.stdinEnabled = false;
    } else if (argument == "--quiet") {
      options.server.quiet = true;
    } else if (argument == "--help" || argument == "-h") {
      printHelp();
      std::exit(0);
    } else {
      throw std::invalid_argument("unknown option: " + argument);
    }
  }
  return options;
}

} // namespace

int main(int argc, char **argv) {
  try {
    const Options options = parseOptions(argc, argv);
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(options.eeprom);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);

    std::atomic<bool> stopRequested{false};
    gStopRequested = &stopRequested;
    std::signal(SIGINT, stopSignal);
    std::signal(SIGTERM, stopSignal);

    if (options.stdinEnabled) {
      std::thread([&board, &stopRequested]() {
        std::cout << "Interactive controls ready; type `help`.\n";
        for (std::string line; !stopRequested.load() &&
                               std::getline(std::cin, line);) {
          const auto result = board.console(line);
          if (!result.message.empty()) {
            std::cout << result.message << '\n';
          }
          if (result.stopRequested) {
            stopRequested.store(true);
            break;
          }
        }
      }).detach();
    }

    pccontroller::virtual_board::runTcpServer(board, options.server,
                                               stopRequested);
    eeprom.flush();
    gStopRequested = nullptr;
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "virtual_board: " << error.what() << '\n';
    return 1;
  }
}
