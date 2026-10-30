#include "virtual_board/virtual_board.hpp"
#include "virtual_board/protocol.hpp"
#include "../../../Project/ControllerEvents.h"
#include <iostream>
#include <stdexcept>
#include <algorithm>

using namespace pccontroller;
void require(bool ok,const char *message) { if(!ok) throw std::runtime_error(message); }
uint32_t u32(const std::vector<uint8_t> &v,size_t i) { return uint32_t(v.at(i)) | uint32_t(v.at(i+1))<<8 | uint32_t(v.at(i+2))<<16 | uint32_t(v.at(i+3))<<24; }
int main() {
  try {
    for (unsigned discriminator=0;discriminator<256;++discriminator) {
      if(discriminator==wire::kReservedEnvelopeByte)continue;
      std::vector<uint8_t> raw{wire::kMagic,static_cast<uint8_t>(discriminator),wire::PwmSet,7,1,4};
      raw.push_back(wire::crc8(raw.data(),raw.size()));
      const auto encoded=wire::cobsEncode(raw.data(),raw.size());
      require(!wire::decode(encoded.data(),encoded.size()),"trace decoder accepted foreign audio/PWM namespace");
    }
    virtual_board::VirtualBoard board({},true);
    wire::StreamDecoder decoder;
    auto receive=[&](uint64_t us=1000) { const auto bytes=board.tick(us); return decoder.feed(bytes.data(),bytes.size()).frames; };
    auto request=[&](uint8_t op,uint8_t sequence,const std::vector<uint8_t> &payload=std::vector<uint8_t>{}) {
      const auto bytes=wire::encode({op,sequence,payload});
      // Every command goes through production UART framing, not direct dispatch.
      for(auto b:bytes) board.feed(&b,1);
      return receive();
    };
    auto startup=receive();
    require(std::any_of(startup.begin(),startup.end(),[](const auto &f) {return f.opcode==wire::HelloResponse;}),"production startup HELLO missing");
    require(std::any_of(startup.begin(),startup.end(),[](const auto &f) {return f.opcode==wire::HelloResponse && f.payload.size()==16 && f.payload[0]==4 && (f.payload[15]&0x40)!=0;}),"volatile menu capability missing");
    auto status=request(wire::GetStatus,1);
    auto found=std::find_if(status.begin(),status.end(),[](const auto &f) {return f.opcode==wire::StatusResponse && f.sequence==1;});
    require(found!=status.end() && found->payload.size()==48,"production telemetry ABI");
    require(u32(found->payload,4)==0x80000000U && found->payload[20]==0 && found->payload[21]==0x80,"missing sensors must be unavailable");
    require(board.outputSnapshot().strip.size()==300,"native must use production 100-pixel strip workspace");
    auto settings=request(wire::GetSettings,2);
    auto sf=std::find_if(settings.begin(),settings.end(),[](const auto &f) {return f.opcode==wire::SettingsResponse;});
    require(sf!=settings.end(),"shared EEPROM settings response");
    const auto originalSettings=sf->payload;
    auto volatileMenu=request(wire::MenuSetPage,38,{0x81});
    require(std::any_of(volatileMenu.begin(),volatileMenu.end(),[](const auto &f) {return f.opcode==wire::Ack && f.sequence==38;}),"volatile menu request rejected");
    receive(1500000);
    auto afterMenu=request(wire::GetSettings,39);
    auto settingsAfter=std::find_if(afterMenu.begin(),afterMenu.end(),[](const auto &f) {return f.opcode==wire::SettingsResponse;});
    require(settingsAfter!=afterMenu.end() && settingsAfter->payload==originalSettings,"volatile mirrored navigation changed EEPROM settings");
    auto invalidMenu=request(wire::MenuSetPage,40,{1,2});
    require(std::any_of(invalidMenu.begin(),invalidMenu.end(),[](const auto &f) {return f.opcode==wire::ErrorResponse;}),"unknown menu request flags accepted");
    auto malformedSettings=request(wire::SetSettings,20,{3,0});
    require(std::any_of(malformedSettings.begin(),malformedSettings.end(),[](const auto &f){return f.opcode==wire::ErrorResponse && f.payload[1]==wire::BadPayload;}),"truncated settings rejected");
    // Production schema/CRC parser must recover from oversized and corrupt
    // traffic while preserving the next complete valid frame.
    std::vector<uint8_t> noise(100,0xFF); noise.push_back(0); board.feed(noise.data(),noise.size());
    receive(); receive();
    auto corrupt=wire::encode({wire::GetStatus,21,{}}); corrupt[corrupt.size()-2]^=0x40;
    board.feed(corrupt.data(),corrupt.size()); receive();
    auto counters=request(wire::GetStatus,22);
    auto cf=std::find_if(counters.begin(),counters.end(),[](const auto &f){return f.opcode==wire::StatusResponse && f.sequence==22;});
    require(cf!=counters.end() && (cf->payload[39]!=0 || cf->payload[40]!=0) && (cf->payload[41]!=0 || cf->payload[42]!=0),"production framing/CRC counters and recovery");
    request(wire::PwmSet,3,{0,0x34,0x02});
    require(board.outputSnapshot().pwm[0]==0x234,"production PWM must use native I2C adapter");
    request(wire::RelaySet,4,{4,1});
    require((board.outputSnapshot().relayMask&16)!=0,"production relay output adapter");
    request(wire::RelayAllOff,5);
    require(board.outputSnapshot().relayMask==0,"shared relay all-off safety");
    board.console("door closed"); receive(30000); receive(30000);
    request(wire::MacroStart,23,{42,0,2,0});
    request(wire::MacroStep,24,{0,0,0,2,0,0,0,0,0,wire::RelaySet,2,5,1,0x40,0x42,0x0F,0,wire::RelaySet,2,5,0});
    request(wire::MacroStep,25,{1});
    receive();
    require((board.outputSnapshot().relayMask&32)!=0,"same dispatcher must execute queued macro action");
    request(wire::MacroCancel,26,{0}); receive();
    require(board.outputSnapshot().relayMask==0,"production macro cancellation safe-stop");
    request(wire::MacroStep,33,{3,50}); // retain a production capture, then try strip output
    const auto stripResult=request(wire::AddressableLed,34,{0xFF,1,2,3,255});
#if PCCONTROLLER_TEST_PROFILE != 0
    require(std::any_of(stripResult.begin(),stripResult.end(),[](const auto &f){return f.opcode==wire::Ack && f.sequence==34;}),"expanded profile must not trade retained capture for strip RAM");
#else
    require(std::any_of(stripResult.begin(),stripResult.end(),[](const auto &f){return f.opcode==wire::ErrorResponse && f.payload[1]==wire::Busy;}),"constrained workspace arbitration preserved");
#endif
    request(wire::MacroStep,35,{4}); request(wire::MacroStep,36,{7});
    request(wire::AddressableLed,37,{0xFF,255,0,0,128});
#if PCCONTROLLER_TEST_PROFILE != 0
    require(board.outputSnapshot().strip.at(1)==128,"richer strip brightness must be retained in the same production driver");
#else
    require(board.outputSnapshot().strip.at(1)==255,"constrained fixed-brightness profile preserved");
#endif
    request(wire::RadioLearnStart,27,{0,0});
    board.console("rf 0xABCDEF 24 1 350"); receive();
    board.console("rf 0xABCDEF 24 1 350");
    auto repeatedRf=receive(100000);
    require(std::any_of(repeatedRf.begin(),repeatedRf.end(),[](const auto &f){return f.opcode==wire::Event && !f.payload.empty() && (f.payload[0]&0x7F)==static_cast<uint8_t>(ControllerEventType::RfReceived);}),"held RF must remain visible while learning");
    require(std::none_of(repeatedRf.begin(),repeatedRf.end(),[](const auto &f){return f.opcode==wire::Event && !f.payload.empty() && (f.payload[0]&0x7F)==static_cast<uint8_t>(ControllerEventType::RfLearned);}),"RF repeat must not learn twice");
    request(wire::RadioLearnCancel,28);
    auto learned=request(wire::RadioLearnList,29,{0});
    require(std::any_of(learned.begin(),learned.end(),[](const auto &f){return f.opcode==wire::RadioLearnListResponse && f.payload.size()>5;}),"RF platform injection must use shared learning/EEPROM code");
    const auto beforeUnknown=board.outputSnapshot();
    board.console("rf 0x123456 24 1 350");
    auto unknownRf=receive(100000);
    require(std::any_of(unknownRf.begin(),unknownRf.end(),[](const auto &f){return f.opcode==wire::Event && !f.payload.empty() && (f.payload[0]&0x7F)==static_cast<uint8_t>(ControllerEventType::RfReceived);}),"unlearned RF must reach host outside learning");
    const auto afterUnknown=board.outputSnapshot();
    require(beforeUnknown.relayMask==afterUnknown.relayMask,"unlearned reception actuated a relay");
    for(size_t channel=0;channel<11;++channel)require(beforeUnknown.pwm[channel]==afterUnknown.pwm[channel],"unlearned reception actuated a user PWM output");
    const auto textResult=request(wire::DisplayText,30,{0,0,0,4,'T','E','S','T'});
    require(std::any_of(textResult.begin(),textResult.end(),[](const auto &f){return f.opcode==wire::Ack && f.sequence==30;}),"shared display text acknowledged");
    auto panel=request(wire::FrontPanelGet,31);
    require(std::any_of(panel.begin(),panel.end(),[](const auto &f){return f.opcode==wire::FrontPanelResponse && f.payload.size()==47;}),"production front-panel wire shape");
    auto unconnected=request(wire::I2cTransfer,32,{0x55,0,0,0});
    require(std::any_of(unconnected.begin(),unconnected.end(),[](const auto &f){return f.opcode==wire::I2cTransferResponse && f.payload[0]!=0;}),"unconnected I2C must not synthesize successful reads");
    auto missingRead=request(wire::I2cTransfer,33,{0x55,0,0,3});
    require(std::any_of(missingRead.begin(),missingRead.end(),[](const auto &f){return f.opcode==wire::I2cTransferResponse && f.payload[0]==4 && f.payload[2]==0;}),"unavailable I2C read must report failure and exact byte count");
    auto bufferedRead=request(wire::I2cTransfer,34,{0x41,0,1,3,0});
    require(std::any_of(bufferedRead.begin(),bufferedRead.end(),[](const auto &f){return f.opcode==wire::I2cTransferResponse && f.payload[0]==0 && f.payload[2]==3 && f.payload.size()==6;}),"bounded I2C bulk read must return exactly the received bytes");
    board.console("tm1637 off"); receive(1100000);
    require(!board.outputSnapshot().tm1637Detected,"missing TM1637 ACK detection");
    board.console("tm1637 on"); receive(1100000);
    require(board.outputSnapshot().tm1637Detected,"TM1637 recovery without reset");
    board.console("temp 0 2600"); receive(1000000); receive(400000);
    auto ts=request(wire::GetStatus,6);
    auto tf=std::find_if(ts.begin(),ts.end(),[](const auto &f) {return f.opcode==wire::StatusResponse && f.sequence==6;});
    require(tf!=ts.end() && tf->payload[20]==0x28 && tf->payload[21]==0x0A,"explicit temperature fixture must traverse production conversion");
    auto bad=request(0x7F,7);
    require(std::any_of(bad.begin(),bad.end(),[](const auto &f) {return f.opcode==wire::ErrorResponse && f.sequence==7 && f.payload[1]==wire::Unsupported;}),"unknown production opcode rejected");
    const uint8_t fragmented[]={3,0xA5}; board.feed(fragmented,2); board.disconnected();
    require(!request(wire::GetStatus,8).empty(),"reconnect clears partial packet");
    request(wire::Reset,9,{0}); receive(250000);
    require(board.resetRequested() && board.outputSnapshot().relayMask==0,"native reset follows production cue/all-off and requests real memory boundary");
    std::cout << "same-source production firmware tests passed\n";
    return 0;
  } catch(const std::exception &e) { std::cerr << e.what() << '\n'; return 1; }
}
