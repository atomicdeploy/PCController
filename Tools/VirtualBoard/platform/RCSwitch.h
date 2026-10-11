#pragma once
#include <Arduino.h>

// RF platform boundary only. The production RadioRuntime owns learning,
// repeat handling, validation, persistence and safe action dispatch.
class RCSwitch {
public:
  void enableTransmit(int) {}
  void enableReceive(int) { receiving_=true; }
  void disableReceive() { receiving_=false; }
  void setReceiveTolerance(int) {}
  bool available() const { return receiving_ && !NativeHal::radioRx.empty(); }
  uint32_t getReceivedValue() const { return NativeHal::radioRx.front().code; }
  uint8_t getReceivedBitlength() const { return NativeHal::radioRx.front().bits; }
  uint8_t getReceivedProtocol() const { return NativeHal::radioRx.front().protocol; }
  uint16_t getReceivedDelay() const { return NativeHal::radioRx.front().pulse; }
  void resetAvailable() { NativeHal::radioRx.pop_front(); }
  void setProtocol(uint8_t v) { protocol_=v; }
  void setPulseLength(uint16_t v) { pulse_=v; }
  void send(uint32_t code,uint8_t bits) { NativeHal::radioTx={code,pulse_,bits,protocol_}; }
private:
  bool receiving_=false;
  uint8_t protocol_=1;
  uint16_t pulse_=350;
};
