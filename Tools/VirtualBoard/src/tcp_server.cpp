#include "virtual_board/tcp_server.hpp"

#include "virtual_board/protocol.hpp"

#include <array>
#include <chrono>
#include <iostream>
#include <stdexcept>
#include <thread>
#include <vector>

#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <cerrno>
#include <netinet/in.h>
#include <sys/select.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <unistd.h>
#endif

namespace pccontroller::virtual_board {
namespace {

#ifdef _WIN32
using Socket = SOCKET;
constexpr Socket kInvalidSocket = INVALID_SOCKET;

class NetworkRuntime {
public:
  NetworkRuntime() {
    WSADATA data{};
    const int result = WSAStartup(MAKEWORD(2, 2), &data);
    if (result != 0) {
      throw std::runtime_error("WSAStartup failed: " +
                               std::to_string(result));
    }
  }
  ~NetworkRuntime() { WSACleanup(); }
};

void closeSocket(Socket socket) {
  if (socket != kInvalidSocket) {
    closesocket(socket);
  }
}

int lastSocketError() { return WSAGetLastError(); }

bool wouldBlock(int error) { return error == WSAEWOULDBLOCK; }

bool setNonBlocking(Socket socket) {
  u_long enabled = 1;
  return ioctlsocket(socket, FIONBIO, &enabled) == 0;
}
#else
using Socket = int;
constexpr Socket kInvalidSocket = -1;

class NetworkRuntime {};

void closeSocket(Socket socket) {
  if (socket != kInvalidSocket) {
    close(socket);
  }
}

int lastSocketError() { return errno; }

bool wouldBlock(int error) {
  return error == EWOULDBLOCK || error == EAGAIN;
}

bool setNonBlocking(Socket socket) {
  int enabled = 1;
  return ioctl(socket, FIONBIO, &enabled) == 0;
}
#endif

class SocketOwner {
public:
  SocketOwner() = default;
  explicit SocketOwner(Socket value) : value_(value) {}
  ~SocketOwner() { closeSocket(value_); }
  SocketOwner(const SocketOwner &) = delete;
  SocketOwner &operator=(const SocketOwner &) = delete;

  Socket get() const { return value_; }
  bool valid() const { return value_ != kInvalidSocket; }
  Socket release() {
    const Socket value = value_;
    value_ = kInvalidSocket;
    return value;
  }
  void reset(Socket value = kInvalidSocket) {
    closeSocket(value_);
    value_ = value;
  }

private:
  Socket value_ = kInvalidSocket;
};

bool sendBytes(Socket socket, const std::vector<std::uint8_t> &data,
               const std::atomic<bool> &stopRequested) {
  std::size_t offset = 0;
  while (offset < data.size() && !stopRequested.load()) {
#ifdef _WIN32
    const int sent =
        send(socket, reinterpret_cast<const char *>(data.data() + offset),
             static_cast<int>(data.size() - offset), 0);
#else
    const int sent =
        static_cast<int>(send(socket, data.data() + offset,
                              data.size() - offset, 0));
#endif
    if (sent > 0) {
      offset += static_cast<std::size_t>(sent);
      continue;
    }
    const int error = lastSocketError();
    if (!wouldBlock(error)) {
      return false;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(1));
  }
  return offset == data.size();
}

bool sendFrames(Socket socket, const std::vector<wire::Frame> &frames,
                const std::atomic<bool> &stopRequested) {
  for (const auto &frame : frames) {
    if (!sendBytes(socket, wire::encode(frame), stopRequested)) {
      return false;
    }
  }
  return true;
}

Socket makeListener(const TcpServerOptions &options) {
  Socket listener = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
  if (listener == kInvalidSocket) {
    throw std::runtime_error("cannot create TCP socket: " +
                             std::to_string(lastSocketError()));
  }
  SocketOwner guard(listener);

  int reuse = 1;
#ifdef _WIN32
  setsockopt(listener, SOL_SOCKET, SO_REUSEADDR,
             reinterpret_cast<const char *>(&reuse), sizeof(reuse));
#else
  setsockopt(listener, SOL_SOCKET, SO_REUSEADDR, &reuse, sizeof(reuse));
#endif

  sockaddr_in address{};
  address.sin_family = AF_INET;
  address.sin_port = htons(options.port);
  if (inet_pton(AF_INET, options.bindAddress.c_str(), &address.sin_addr) != 1) {
    throw std::runtime_error("invalid IPv4 bind address: " +
                             options.bindAddress);
  }
  if (bind(listener, reinterpret_cast<const sockaddr *>(&address),
           sizeof(address)) != 0) {
    throw std::runtime_error("cannot bind TCP listener: " +
                             std::to_string(lastSocketError()));
  }
  if (listen(listener, 4) != 0) {
    throw std::runtime_error("cannot listen on TCP socket: " +
                             std::to_string(lastSocketError()));
  }
  if (!setNonBlocking(listener)) {
    throw std::runtime_error("cannot make TCP listener nonblocking");
  }
  return guard.release();
}

} // namespace

void runTcpServer(VirtualBoard &board, const TcpServerOptions &options,
                  std::atomic<bool> &stopRequested) {
  [[maybe_unused]] NetworkRuntime network;
  SocketOwner listener(makeListener(options));
  SocketOwner client;
  wire::StreamDecoder decoder;

  if (!options.quiet) {
    std::cout << "Virtual board listening on tcp://" << options.bindAddress
              << ':' << options.port << '\n';
  }

  std::array<std::uint8_t, 512> receiveBuffer{};
  while (!stopRequested.load()) {
    if (!client.valid()) {
      sockaddr_in peer{};
#ifdef _WIN32
      int peerLength = sizeof(peer);
#else
      socklen_t peerLength = sizeof(peer);
#endif
      const Socket accepted =
          accept(listener.get(), reinterpret_cast<sockaddr *>(&peer),
                 &peerLength);
      if (accepted != kInvalidSocket) {
        if (!setNonBlocking(accepted)) {
          closeSocket(accepted);
        } else {
          client.reset(accepted);
          decoder.reset();
          if (!options.quiet) {
            std::cout << "Client connected\n";
          }
          if (!sendFrames(client.get(), board.connectedFrames(),
                          stopRequested)) {
            client.reset();
          }
        }
      } else if (!wouldBlock(lastSocketError())) {
        throw std::runtime_error("TCP accept failed: " +
                                 std::to_string(lastSocketError()));
      }
    }

    if (client.valid()) {
      fd_set readable;
      FD_ZERO(&readable);
      FD_SET(client.get(), &readable);
      timeval timeout{};
      timeout.tv_sec = 0;
      // One-millisecond service cadence keeps the native mock useful for
      // validating the real firmware's microsecond-timestamped macro deltas.
      timeout.tv_usec = 1000;
#ifdef _WIN32
      const int ready = select(0, &readable, nullptr, nullptr, &timeout);
#else
      const int ready =
          select(client.get() + 1, &readable, nullptr, nullptr, &timeout);
#endif
      if (ready < 0) {
        throw std::runtime_error("TCP select failed: " +
                                 std::to_string(lastSocketError()));
      }
      if (ready > 0 && FD_ISSET(client.get(), &readable)) {
#ifdef _WIN32
        const int received =
            recv(client.get(), reinterpret_cast<char *>(receiveBuffer.data()),
                 static_cast<int>(receiveBuffer.size()), 0);
#else
        const int received = static_cast<int>(
            recv(client.get(), receiveBuffer.data(), receiveBuffer.size(), 0));
#endif
        if (received == 0) {
          if (!options.quiet) {
            std::cout << "Client disconnected\n";
          }
          client.reset();
          decoder.reset();
        } else if (received < 0) {
          const int error = lastSocketError();
          if (!wouldBlock(error)) {
            if (!options.quiet) {
              std::cout << "Client socket error " << error << '\n';
            }
            client.reset();
            decoder.reset();
          }
        } else {
          wire::DecodeBatch batch = decoder.feed(
              receiveBuffer.data(), static_cast<std::size_t>(received));
          board.noteProtocolErrors(batch.framingErrors, batch.crcErrors);
          for (const auto &frame : batch.frames) {
            if (!sendFrames(client.get(), board.handle(frame),
                            stopRequested)) {
              client.reset();
              decoder.reset();
              break;
            }
          }
        }
      }
    } else {
      std::this_thread::sleep_for(std::chrono::milliseconds(10));
    }

    const auto asynchronous = board.tick();
    if (client.valid() &&
        !sendFrames(client.get(), asynchronous, stopRequested)) {
      client.reset();
      decoder.reset();
    }
  }
}

} // namespace pccontroller::virtual_board
