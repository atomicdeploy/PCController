// This is the production sketch, not a copy of its implementation.
#include "../../../PCController.ino"
#include <EEPROM.h>
#include "virtual_board/virtual_board.hpp"
#include <atomic>
#include <sstream>
#include <stdexcept>

namespace pccontroller::virtual_board {
namespace { std::atomic<bool> claimed{false}; }
VirtualBoard::VirtualBoard(const std::filesystem::path &eeprom,bool deterministic) {
  if(claimed.exchange(true)) throw std::runtime_error("one firmware instance per process; use an isolated process/EEPROM for each board");
  NativeHal::configureClock(deterministic);
  EEPROM.open(eeprom);
  setup();
  EEPROM.flush();
}
VirtualBoard::~VirtualBoard() { /* Process owns firmware globals until exit. */ }
void VirtualBoard::feed(const uint8_t *data,size_t length) {
  std::lock_guard<std::mutex> lock(mutex_);
  if(length && !data) throw std::invalid_argument("null UART input");
  if(length) Serial.feed(data,length);
}
std::vector<uint8_t> VirtualBoard::tick(uint64_t advanceUs) {
  std::lock_guard<std::mutex> lock(mutex_);
  if(!resetRequested_) {
    NativeHal::advance(advanceUs);
    try { loop(); } catch(const NativeHal::ResetRequested &) { resetRequested_=true; EEPROM.flush(); }
    // Coalesce native image writes; EEPROM.update still owns wear reduction.
    static uint32_t lastFlush=0;
    if(static_cast<uint32_t>(millis()-lastFlush)>=1000) { EEPROM.flush(); lastFlush=millis(); }
  }
  return Serial.drain();
}
std::string nativeEepromSnapshot() { return EEPROM.snapshotHex(); }
void releaseNativeEeprom() { EEPROM.unlock(); }
std::vector<uint8_t> VirtualBoard::connectedBytes() {
  std::lock_guard<std::mutex> lock(mutex_);
  Serial.drain();
  sendHello(0);
  now=millis();
  sendTelemetry(0);
  return Serial.drain();
}
void VirtualBoard::disconnected() {
  std::lock_guard<std::mutex> lock(mutex_);
  Serial.disconnect();
  // Reset the stream parser, not firmware clocks/settings/outputs. Production
  // host-offline safety expires through the same serviceController deadline.
  appProtocol.begin(Serial,PCCONTROLLER_UART_BAUD,handleProtocolFrame);
}
void VirtualBoard::flush() { std::lock_guard<std::mutex> lock(mutex_); EEPROM.flush(); }
bool VirtualBoard::resetRequested() const { std::lock_guard<std::mutex> lock(mutex_); return resetRequested_; }
OutputSnapshot VirtualBoard::outputSnapshot() const {
  std::lock_guard<std::mutex> lock(mutex_);
  OutputSnapshot s;
  s.relayMask=relays.activeRelayMask();
  for(uint8_t i=0;i<16;++i) s.pwm[i]=pwm.logicalValue(i);
  std::copy(display.rawSegments(),display.rawSegments()+4,s.segments.begin());
  s.brightness=display.brightness(); s.tm1637Detected=display.detected();
  s.buzzerHz=NativeHal::toneFrequency; s.strip=NativeHal::strip;
  return s;
}
std::string VirtualBoard::describe() const {
  const auto s=outputSnapshot();
  std::ostringstream out;
  out << "production firmware; relays=" << unsigned(s.relayMask) << "; TM1637=" << (s.tm1637Detected ? "present" : "absent") << "; buzzer=" << s.buzzerHz << " Hz; strip=" << s.strip.size()/3 << " pixels; INA219=unavailable; temperatures=unavailable unless explicitly injected";
  return out.str();
}
ConsoleResult VirtualBoard::console(const std::string &line) {
  if(line=="status") return {describe()};
  if(line=="help") return {"status | door open/closed | raw 0..255 | tm1637 on/off | temp 0/1 centiC/none | rf code bits protocol pulseUs | quit. Inputs are explicit demo fixtures, not physical measurements."};
  if(line=="quit" || line=="exit") return {"",true};
  std::lock_guard<std::mutex> lock(mutex_);
  std::istringstream input(line); std::string command,value;
  input >> command >> value;
  if(command=="door" && (value=="open" || value=="closed")) {
    if(value=="open") NativeHal::shiftInput |= 0x80;
    else NativeHal::shiftInput &= 0x7F;
  } else if(command=="tm1637" && (value=="on" || value=="off")) {
    NativeHal::tm1637Present=value=="on";
  } else if(command=="raw") {
    size_t used=0; const auto v=std::stoul(value,&used,0);
    if(used!=value.size() || v>255) return {"raw input must be 0..255"};
    NativeHal::shiftInput=static_cast<uint8_t>(v);
  } else if(command=="temp" && (value=="0" || value=="1")) {
    std::string reading; input >> reading;
    if(reading=="none") NativeHal::temperature[value=="1"]=INT16_MIN;
    else { size_t used=0; const auto v=std::stol(reading,&used); if(used!=reading.size() || v<-5500 || v>12500) return {"temperature fixture must be -5500..12500 centiC or none"}; NativeHal::temperature[value=="1"]=static_cast<int16_t>(v); }
    temperatureBus.begin(); discoverTemperatureSensors();
  } else if(command=="rf") {
    size_t used=0; const auto code=std::stoull(value,&used,0); unsigned bits=0,protocol=0,pulse=0;
    if(used!=value.size() || !(input >> bits >> protocol >> pulse) || code>UINT32_MAX || code==0 || bits==0 || bits>32 || protocol==0 || protocol>12 || pulse==0 || pulse>UINT16_MAX) return {"invalid RF fixture"};
    if(NativeHal::radioRx.size()>=16) return {"RF input backpressure"};
    NativeHal::radioRx.push_back({static_cast<uint32_t>(code),static_cast<uint16_t>(pulse),static_cast<uint8_t>(bits),static_cast<uint8_t>(protocol)});
  } else return {"unsupported hardware input; type help"};
  return {"fixture input applied"};
}
}
