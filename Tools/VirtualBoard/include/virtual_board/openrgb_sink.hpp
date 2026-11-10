#pragma once
#include <memory>
#include <string>
#include <vector>
#include <cstdint>
namespace pccontroller::virtual_board {
class OpenRgbSink {
 public:
  explicit OpenRgbSink(const std::string &assignment); // device@IPv4:port
  ~OpenRgbSink();
  void observe(const std::vector<uint8_t> &rgb);
 private:
  struct Impl;
  std::unique_ptr<Impl> impl_;
};
}
