#pragma once
#include "../../../../Project/ProtocolContract.h"
#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

// Client/trace helpers only. Runtime UART uses production UartProtocol directly.
namespace pccontroller::wire {
using namespace ControllerProtocol::WireContract;
constexpr uint8_t kMagic=Magic, kReservedEnvelopeByte=ReservedEnvelopeByte;
constexpr size_t kMaximumPayload=MaximumPayload, kRawOverhead=RawFrameOverhead;
constexpr size_t kMaximumRaw=MaximumRawFrame, kMaximumEncoded=MaximumEncodedFrame;
struct Frame { uint8_t opcode=0, sequence=0; std::vector<uint8_t> payload; };
enum class DecodeError { None, Framing, Crc };
struct DecodeResult {
  Frame frame;
  DecodeError error=DecodeError::None;
  std::string message;
  explicit operator bool() const { return error==DecodeError::None; }
};
struct DecodeBatch { std::vector<Frame> frames; size_t framingErrors=0,crcErrors=0; };
uint8_t crc8(const uint8_t *,size_t);
std::vector<uint8_t> cobsEncode(const uint8_t *,size_t);
bool cobsDecode(const uint8_t *,size_t,std::vector<uint8_t> &);
std::vector<uint8_t> encode(const Frame &);
DecodeResult decode(const uint8_t *,size_t);
class StreamDecoder {
public:
  DecodeBatch feed(const uint8_t *,size_t);
  void reset();
private:
  std::vector<uint8_t> encoded_;
  bool dropping_=false;
};
}
