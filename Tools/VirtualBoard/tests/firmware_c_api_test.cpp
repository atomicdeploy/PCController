#include "pccontroller_firmware.h"
#include "virtual_board/protocol.hpp"
#include <iostream>
#include <stdexcept>
void require(bool ok,const char *message) { if(!ok) throw std::runtime_error(message); }
int main() {
  try {
    pcc_firmware_handle handle=nullptr,extra=nullptr;
    require(pcc_firmware_create(nullptr,1,&handle)==PCC_FW_OK,"library firmware creation");
    require(pcc_firmware_create(nullptr,1,&extra)==PCC_FW_ERROR && extra==nullptr,"explicit singleton ownership");
    require(pcc_firmware_write(handle,nullptr,1)==PCC_FW_ERROR,"null input rejected across C ABI");
    const auto command=pccontroller::wire::encode({pccontroller::wire::GetStatus,99,{}});
    require(pcc_firmware_write(handle,command.data(),command.size())==PCC_FW_OK,"binary length-aware input");
    pccontroller::wire::StreamDecoder decoder;
    bool status=false;
    for(int i=0;i<100;++i) {
      uint8_t bytes[7]; size_t written=0;
      require(pcc_firmware_poll(handle,1000,bytes,sizeof(bytes),&written)==PCC_FW_OK,"partial binary drain");
      for(const auto &f:decoder.feed(bytes,written).frames) if(f.opcode==pccontroller::wire::StatusResponse && f.sequence==99 && f.payload.size()==48) status=true;
    }
    require(status,"library must run production dispatcher and preserve partial outputs");
    require(pcc_firmware_close(handle)==PCC_FW_OK,"library flush/close");
    require(pcc_firmware_write(handle,command.data(),command.size())==PCC_FW_ERROR,"closed handle rejected");
    std::cout << "production firmware C ABI passed\n";
  } catch(const std::exception &e) { std::cerr << e.what() << ": " << pcc_firmware_error() << '\n'; return 1; }
}
