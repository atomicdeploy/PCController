#include "virtual_board/protocol.hpp"

#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>
#endif

namespace {

#ifdef _WIN32
using Socket = SOCKET;
constexpr Socket kInvalidSocket = INVALID_SOCKET;
void closeSocket(Socket value) { closesocket(value); }
#else
using Socket = int;
constexpr Socket kInvalidSocket = -1;
void closeSocket(Socket value) { close(value); }
#endif

class SocketOwner {
public:
  explicit SocketOwner(Socket socket) : socket_(socket) {}
  ~SocketOwner() {
    if (socket_ != kInvalidSocket) {
      closeSocket(socket_);
    }
  }
  Socket get() const { return socket_; }

private:
  Socket socket_;
};

void sendAll(Socket socket, const std::vector<std::uint8_t> &data) {
  std::size_t offset = 0;
  while (offset < data.size()) {
#ifdef _WIN32
    const int sent =
        send(socket, reinterpret_cast<const char *>(data.data() + offset),
             static_cast<int>(data.size() - offset), 0);
#else
    const int sent = static_cast<int>(
        send(socket, data.data() + offset, data.size() - offset, 0));
#endif
    if (sent <= 0) {
      throw std::runtime_error("send failed");
    }
    offset += static_cast<std::size_t>(sent);
  }
}

std::uint16_t parsePort(const std::string &value) {
  std::size_t used = 0;
  const unsigned long parsed = std::stoul(value, &used, 10);
  if (used != value.size() || parsed == 0 || parsed > 65535) {
    throw std::invalid_argument("invalid port");
  }
  return static_cast<std::uint16_t>(parsed);
}

} // namespace

int main(int argc, char **argv) {
  try {
    const std::string host = argc > 1 ? argv[1] : "127.0.0.1";
    const std::uint16_t port = argc > 2 ? parsePort(argv[2]) : 8765;

#ifdef _WIN32
    WSADATA data{};
    if (WSAStartup(MAKEWORD(2, 2), &data) != 0) {
      throw std::runtime_error("WSAStartup failed");
    }
#endif

    Socket socketValue = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (socketValue == kInvalidSocket) {
      throw std::runtime_error("socket creation failed");
    }
    SocketOwner socket(socketValue);

    sockaddr_in address{};
    address.sin_family = AF_INET;
    address.sin_port = htons(port);
    if (inet_pton(AF_INET, host.c_str(), &address.sin_addr) != 1) {
      throw std::runtime_error("host must be an IPv4 address");
    }
    if (connect(socket.get(), reinterpret_cast<sockaddr *>(&address),
                sizeof(address)) != 0) {
      throw std::runtime_error("connect failed");
    }

#ifdef _WIN32
    const DWORD timeout = 3000;
    setsockopt(socket.get(), SOL_SOCKET, SO_RCVTIMEO,
               reinterpret_cast<const char *>(&timeout), sizeof(timeout));
#else
    timeval timeout{};
    timeout.tv_sec = 3;
    setsockopt(socket.get(), SOL_SOCKET, SO_RCVTIMEO, &timeout,
               sizeof(timeout));
#endif

    constexpr std::uint8_t helloSequence = 42;
    constexpr std::uint8_t stripSequence = 43;
    constexpr std::uint8_t statusSequence = 44;
    constexpr std::uint8_t resetSequence = 45;
    sendAll(socket.get(), pccontroller::wire::encode(
                              {pccontroller::wire::Hello, helloSequence, {}}));

    pccontroller::wire::StreamDecoder decoder;
    std::array<std::uint8_t, 512> buffer{};
    bool helloValidated = false;
    bool stripValidated = false;
    bool statusValidated = false;
    bool resetAcknowledged = false;
    bool resetEventValidated = false;
    std::uint32_t initialResetCount = 0;
    for (;;) {
#ifdef _WIN32
      const int received =
          recv(socket.get(), reinterpret_cast<char *>(buffer.data()),
               static_cast<int>(buffer.size()), 0);
#else
      const int received = static_cast<int>(
          recv(socket.get(), buffer.data(), buffer.size(), 0));
#endif
      if (received <= 0) {
        throw std::runtime_error("timed out waiting for protocol response");
      }
      const auto batch =
          decoder.feed(buffer.data(), static_cast<std::size_t>(received));
      for (const auto &frame : batch.frames) {
        if (frame.opcode == pccontroller::wire::Ack &&
            frame.sequence == stripSequence) {
          if (!helloValidated || frame.payload.size() != 6 ||
              frame.payload[0] != pccontroller::wire::AddressableLed ||
              frame.payload[1] != pccontroller::wire::NoError) {
            throw std::runtime_error(
                "addressable LED acknowledgement is invalid");
          }
          std::cout << "raw addressable LED ACK OK\n";
          stripValidated = true;
          sendAll(socket.get(),
                  pccontroller::wire::encode(
                      {pccontroller::wire::GetStatus, statusSequence, {}}));
          continue;
        }
        if (frame.opcode == pccontroller::wire::StatusResponse &&
            frame.sequence == statusSequence) {
          if (!stripValidated || frame.payload.size() != 48) {
            throw std::runtime_error("48-byte STATUS payload is invalid");
          }
          initialResetCount =
              static_cast<std::uint32_t>(frame.payload[44]) |
              (static_cast<std::uint32_t>(frame.payload[45]) << 8U) |
              (static_cast<std::uint32_t>(frame.payload[46]) << 16U) |
              (static_cast<std::uint32_t>(frame.payload[47]) << 24U);
          statusValidated = true;
          std::cout << "raw STATUS OK: reset_cause=0x" << std::hex
                    << static_cast<unsigned>(frame.payload[43]) << std::dec
                    << " reset_count=" << initialResetCount << '\n';
          sendAll(socket.get(),
                  pccontroller::wire::encode(
                      {pccontroller::wire::Reset, resetSequence, {0}}));
          continue;
        }
        if (frame.opcode == pccontroller::wire::Ack &&
            frame.sequence == resetSequence) {
          if (!statusValidated || frame.payload.size() != 6 ||
              frame.payload[0] != pccontroller::wire::Reset ||
              frame.payload[1] != pccontroller::wire::NoError) {
            throw std::runtime_error("reset acknowledgement is invalid");
          }
          resetAcknowledged = true;
        } else if (frame.opcode == pccontroller::wire::Event &&
                   !frame.payload.empty() && (frame.payload[0] & 0x7FU) == 7) {
          if (!statusValidated || frame.payload.size() != 10 ||
              frame.payload[1] != 0x08) {
            throw std::runtime_error("reset event shape is invalid");
          }
          const std::uint32_t resetCount =
              static_cast<std::uint32_t>(frame.payload[2]) |
              (static_cast<std::uint32_t>(frame.payload[3]) << 8U) |
              (static_cast<std::uint32_t>(frame.payload[4]) << 16U) |
              (static_cast<std::uint32_t>(frame.payload[5]) << 24U);
          if (resetCount != initialResetCount + 1U) {
            throw std::runtime_error(
                "reset event did not advance the persistent count");
          }
          resetEventValidated = true;
          std::cout << "raw reset EVENT OK: cause=0x08 count="
                    << resetCount << '\n';
        }
        if (resetAcknowledged && resetEventValidated) {
#ifdef _WIN32
          WSACleanup();
#endif
          return 0;
        }
        if (frame.opcode != pccontroller::wire::HelloResponse ||
            frame.sequence != helloSequence || helloValidated) {
          continue;
        }
        if (frame.payload.size() < 21 || frame.payload[8] != 12) {
          throw std::runtime_error("HELLO payload shape is invalid");
        }
        const std::string name(frame.payload.begin() + 9,
                               frame.payload.begin() + 21);
        if (name != "PCController") {
          throw std::runtime_error("unexpected device identity: " + name);
        }
        const std::uint32_t hash =
            static_cast<std::uint32_t>(frame.payload[22]) |
            (static_cast<std::uint32_t>(frame.payload[23]) << 8U) |
            (static_cast<std::uint32_t>(frame.payload[24]) << 16U) |
            (static_cast<std::uint32_t>(frame.payload[25]) << 24U);
        std::cout << "raw HELLO OK: " << name << " build=0x" << std::hex
                  << hash << std::dec << '\n';
        helloValidated = true;
        sendAll(socket.get(),
                pccontroller::wire::encode(
                    {pccontroller::wire::AddressableLed, stripSequence,
                     {0xFF, 1, 2, 3, 4}}));
      }
    }
  } catch (const std::exception &error) {
    std::cerr << "virtual_board_smoke: " << error.what() << '\n';
#ifdef _WIN32
    WSACleanup();
#endif
    return 1;
  }
}
