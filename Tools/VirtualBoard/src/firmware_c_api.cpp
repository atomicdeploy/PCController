#include "pccontroller_firmware.h"
#include "virtual_board/virtual_board.hpp"
#include <algorithm>
#include <cstring>
#include <memory>
#include <mutex>
#include <stdexcept>

namespace {
std::mutex apiMutex;
std::unique_ptr<pccontroller::virtual_board::VirtualBoard> instance;
std::vector<uint8_t> pending;
bool closed=false;
thread_local std::string error;
void validate(pcc_firmware_handle handle) {
  if(!instance || closed || handle!=instance.get()) throw std::invalid_argument("invalid/closed firmware handle");
}
template<class F> int guarded(F action) {
  std::lock_guard<std::mutex> lock(apiMutex);
  try { error.clear(); return action(); }
  catch(const std::exception &e) { error=e.what(); return PCC_FW_ERROR; }
  catch(...) { error="unexpected firmware exception"; return PCC_FW_ERROR; }
}
}
extern "C" {
int pcc_firmware_create(const char *path,int deterministic,pcc_firmware_handle *out) {
  return guarded([&] {
    if(!out) throw std::invalid_argument("missing handle output");
    *out=nullptr;
    if(instance) throw std::runtime_error("firmware already commissioned in this process/module lifetime");
    instance=std::make_unique<pccontroller::virtual_board::VirtualBoard>(path ? path : "",deterministic!=0);
    *out=instance.get(); return PCC_FW_OK;
  });
}
int pcc_firmware_write(pcc_firmware_handle handle,const uint8_t *data,size_t length) {
  return guarded([&] { validate(handle); instance->feed(data,length); return PCC_FW_OK; });
}
int pcc_firmware_poll(pcc_firmware_handle handle,uint64_t us,uint8_t *out,size_t capacity,size_t *written) {
  return guarded([&] {
    validate(handle);
    if(!written || (capacity && !out)) throw std::invalid_argument("invalid output buffer");
    *written=0;
    const auto bytes=instance->tick(us);
    if(bytes.size()>65536-pending.size()) throw std::runtime_error("firmware output backpressure; drain more frequently");
    pending.insert(pending.end(),bytes.begin(),bytes.end());
    const auto count=std::min(capacity,pending.size());
    if(count) std::memcpy(out,pending.data(),count);
    pending.erase(pending.begin(),pending.begin()+static_cast<std::ptrdiff_t>(count)); *written=count;
    return instance->resetRequested() ? PCC_FW_RESET_REQUIRED : PCC_FW_OK;
  });
}
int pcc_firmware_close(pcc_firmware_handle handle) {
  return guarded([&] { validate(handle); instance->flush(); pending.clear(); closed=true; return PCC_FW_OK; });
}
const char *pcc_firmware_error() { return error.c_str(); }
}
