// This is the production sketch, not a copy of its implementation.
#include "../../../PCController.ino"
#include <EEPROM.h>
#include "virtual_board/virtual_board.hpp"
#include "virtual_board/sensor_sources.hpp"
#include <atomic>
#include <sstream>
#include <stdexcept>

namespace pccontroller::virtual_board {
namespace { std::atomic<bool> claimed{false}; }
VirtualBoard::VirtualBoard(const std::filesystem::path &eeprom,bool deterministic) {
  if(claimed.exchange(true)) throw std::runtime_error("one firmware instance per process; use an isolated process/EEPROM for each board");
  NativeHal::configureClock(deterministic);
  NativeHal::refreshSensorSources(0, true);
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
    if (NativeHal::refreshSensorSources(NativeHal::clockMicros())) {
      ina219Available = NativeHal::powerAvailable;
      if (!ina219Available) {
        sensors.supplyMilliVolts = sensors.busMilliVolts = INVALID_I32;
        sensors.currentMilliAmps = sensors.powerMilliWatts = INVALID_I32;
      }
    }
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
  clearVirtualKeyHolds();
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
#if !PCCONTROLLER_USE_WS2812B
  s.stripGreenOffset=2;s.stripBlueOffset=0;
#endif
  return s;
}
std::string VirtualBoard::describe() const {
  const auto s=outputSnapshot();
  std::ostringstream out;
  out << "production firmware; relays=" << unsigned(s.relayMask) << "; TM1637=" << (s.tm1637Detected ? "present" : "absent") << "; buzzer=" << s.buzzerHz << " Hz; strip=" << s.strip.size()/3 << " pixels; sensors=native inputs (explicit mappings/fixtures only)";
  return out.str();
}
ConsoleResult VirtualBoard::console(const std::string &line) {
  if(line.size()>256) return {"input exceeds 256 bytes"};
  if(line=="status") return {describe()};
  if(line=="help") return {"status | door open/closed | raw 0..255 | input 1..8 on/off | gpio 0..63 0/1/none | tm1637 on/off | temp 0/1 centiC/none | power milliV milliA/none | rf code bits protocol pulseUs | quit. Aliases: I1:ON/OFF, T1:centiC, GPIO17:0/1. No text relay/PWM actuation; use the controller protocol."};
  if(line=="quit" || line=="exit") return {"",true};
  std::lock_guard<std::mutex> lock(mutex_);
  std::string normalized=line;
  const auto colon=line.find(':');
  if(colon!=std::string::npos) {
    const auto key=line.substr(0,colon), reading=line.substr(colon+1);
    if(key=="T1" || key=="T2") normalized=std::string("temp ")+(key=="T1"?"0 ":"1 ")+reading;
    else if(key.size()==2 && key[0]=='I' && key[1]>='1' && key[1]<='8' && (reading=="ON" || reading=="OFF"))
      normalized="input "+key.substr(1)+" "+(reading=="ON"?"on":"off");
    else if(key.rfind("GPIO",0)==0) normalized="gpio "+key.substr(4)+" "+reading;
  }
  std::istringstream input(normalized); std::string command,value;
  input >> command >> value;
  auto finished=[&]() { input >> std::ws; return input.eof(); };
  auto number=[](const std::string &text,int64_t low,int64_t high) {
    if(text.empty()) throw std::invalid_argument("missing numeric input");
    size_t used=0; const auto n=std::stoll(text,&used,0);
    if(used!=text.size() || n<low || n>high) throw std::invalid_argument("numeric input out of range");
    return n;
  };
  // Validate complete input before mutation; no partial/trailing-command admission.
  try {
  if(command=="input") {
    const auto index=static_cast<unsigned>(number(value,1,8)-1);
    if(hostInputMask_ & (1U<<index)) return {"input is owned by its assigned host GPIO source"};
    std::string state; input >> state;
    if(!finished() || (state!="on" && state!="off")) return {"input requires 1..8 on/off"};
    const auto mask=static_cast<uint8_t>(1U<<index);
    if(state=="on") NativeHal::shiftInput &= static_cast<uint8_t>(~mask);
    else NativeHal::shiftInput |= mask;
  } else if(command=="gpio") {
    const auto pin=static_cast<size_t>(number(value,0,63));
    std::string state; input >> state;
    if(!finished() || (state!="0" && state!="1" && state!="none")) return {"gpio requires 0..63 0/1/none"};
    NativeHal::inputLevels[pin]=static_cast<int8_t>(state=="none"?-1:state=="1"?1:0);
  } else if(command=="power") {
    if(NativeHal::powerSourceMapped()) return {"power is owned by configured sensor files"};
    if(value=="none") {
      if(!finished()) return {"power none takes no other arguments"};
      NativeHal::powerAvailable=false;
      ina219Available=false;
      sensors.supplyMilliVolts=sensors.busMilliVolts=INVALID_I32;
      sensors.currentMilliAmps=sensors.powerMilliWatts=INVALID_I32;
    } else {
      std::string current; input >> current;
      const auto voltage=number(value,0,100000), milliA=number(current,-100000,100000);
      if(!finished()) return {"power requires milliV milliA or none"};
      NativeHal::supplyMilliVolts=static_cast<int32_t>(voltage);
      NativeHal::currentMilliAmps=static_cast<int32_t>(milliA);
      NativeHal::powerMilliWatts=static_cast<int32_t>(voltage*milliA/1000);
      NativeHal::powerAvailable=true; ina219Available=true;
    }
  } else
  if(command=="door" && (value=="open" || value=="closed")) {
    if(hostInputMask_&0x80) return {"door is owned by its assigned host GPIO source"};
    if(!finished()) return {"unexpected trailing input"};
    if(value=="open") NativeHal::shiftInput |= 0x80;
    else NativeHal::shiftInput &= 0x7F;
  } else if(command=="tm1637" && (value=="on" || value=="off")) {
    if(!finished()) return {"unexpected trailing input"};
    NativeHal::tm1637Present=value=="on";
  } else if(command=="raw") {
    if(hostInputMask_) return {"raw input conflicts with assigned host GPIO sources"};
    size_t used=0; const auto v=std::stoul(value,&used,0);
    if(used!=value.size() || v>255 || !finished()) return {"raw input must be 0..255"};
    NativeHal::shiftInput=static_cast<uint8_t>(v);
  } else if(command=="temp" && (value=="0" || value=="1")) {
    if(NativeHal::temperatureSourceMapped(value=="1")) return {"temperature is owned by a configured sensor file"};
    std::string reading; input >> reading;
    if(!finished()) return {"unexpected trailing input"};
    const auto parsed=reading=="none" ? INT16_MIN : number(reading,-5500,12500);
    NativeHal::temperatureConfigured[0]=true;
    if(value=="1") NativeHal::temperatureConfigured[1]=true;
    NativeHal::temperature[value=="1"]=static_cast<int16_t>(parsed);
    temperatureBus.begin(); discoverTemperatureSensors();
  } else if(command=="rf") {
    size_t used=0; const auto code=std::stoull(value,&used,0); unsigned bits=0,protocol=0,pulse=0;
    if(used!=value.size() || !(input >> bits >> protocol >> pulse) || code>UINT32_MAX || code==0 || bits==0 || bits>32 || protocol==0 || protocol>12 || pulse==0 || pulse>UINT16_MAX || !finished()) return {"invalid RF fixture"};
    if(NativeHal::radioRx.size()>=16) return {"RF input backpressure"};
    NativeHal::radioRx.push_back({static_cast<uint32_t>(code),static_cast<uint16_t>(pulse),static_cast<uint8_t>(bits),static_cast<uint8_t>(protocol)});
  } else return {"unsupported hardware input; type help"};
  } catch(const std::exception &) { return {"invalid or out-of-range hardware input"}; }
  return {"fixture input applied"};
}
void VirtualBoard::bindHostInput(uint8_t channel) {
  if(channel<1 || channel>8)throw std::invalid_argument("host input must be 1..8");
  std::lock_guard<std::mutex> lock(mutex_);
  const auto mask=static_cast<uint8_t>(1U<<(channel-1));
  if(hostInputMask_&mask)throw std::invalid_argument("duplicate host input owner");
  hostInputMask_|=mask;
}
void VirtualBoard::observeHostInput(uint8_t channel,bool active) {
  if(channel<1 || channel>8)throw std::invalid_argument("host input must be 1..8");
  std::lock_guard<std::mutex> lock(mutex_);
  const auto mask=static_cast<uint8_t>(1U<<(channel-1));
  if(!(hostInputMask_&mask))throw std::invalid_argument("host input is not assigned");
  if(active)NativeHal::shiftInput&=static_cast<uint8_t>(~mask);
  else NativeHal::shiftInput|=mask;
}
}
