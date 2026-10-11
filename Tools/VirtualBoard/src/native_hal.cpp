#include <Arduino.h>
#include <EEPROM.h>
#include <algorithm>
#include <chrono>
#include <fstream>
#include <cstdlib>
#include <thread>
#ifdef _WIN32
#define NOUSER
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#else
#include <fcntl.h>
#include <sys/file.h>
#include <unistd.h>
#endif

namespace NativeHal {
namespace {
bool deterministic=false;
uint64_t timeUs=0;
auto epoch=std::chrono::steady_clock::now();
std::array<uint8_t,256> pwmRegisters{};
uint8_t address=0, cursor=0;
bool firstByte=true;
}
uint64_t clockMicros() {
  return deterministic ? timeUs : static_cast<uint64_t>(std::chrono::duration_cast<std::chrono::microseconds>(std::chrono::steady_clock::now()-epoch).count());
}
void configureClock(bool d) { deterministic=d; timeUs=0; epoch=std::chrono::steady_clock::now(); }
uint8_t resetCause() { return std::getenv("PCCONTROLLER_NATIVE_RESET") ? 8 : 1; }
void advance(uint64_t us) {
  if(deterministic) timeUs+=us;
  else if(us>=1000) std::this_thread::sleep_for(std::chrono::microseconds(us));
  else { const auto until=std::chrono::steady_clock::now()+std::chrono::microseconds(us); while(std::chrono::steady_clock::now()<until) {} }
}
[[noreturn]] void requestReset() { throw ResetRequested{}; }
uint8_t i2cBegin(uint8_t a) { address=a; firstByte=true; return a==0x41 && pwmPresent ? 0 : 2; }
bool i2cWrite(uint8_t v) {
  if(address!=0x41 || !pwmPresent) return false;
  if(firstByte) { cursor=v; firstByte=false; } else pwmRegisters[cursor++]=v;
  return true;
}
uint8_t i2cEnd() { return address==0x41 && pwmPresent ? 0 : 2; }
uint8_t i2cRead(uint8_t a,uint8_t *v,uint8_t n) {
  if(a!=0x41 || !pwmPresent) return 0;
  for(uint8_t i=0;i<n;++i) v[i]=pwmRegisters[cursor++];
  return n;
}
void showStrip(const uint8_t *v,uint16_t n) { strip.assign(v,v+n); }
}

void EEPROMClass::open(const std::filesystem::path &path) {
  path_=path;
  if(path.empty()) {
    if(const auto *hex=std::getenv("PCCONTROLLER_NATIVE_EEPROM")) {
      const std::string image(hex);
      if(image.size()!=bytes_.size()*2) throw std::runtime_error("invalid memory EEPROM restart image");
      for(size_t i=0;i<bytes_.size();++i) bytes_[i]=static_cast<uint8_t>(std::stoul(image.substr(i*2,2),nullptr,16));
    }
    return;
  }
  auto lockPath=path; lockPath += ".lock";
#ifdef _WIN32
  auto handle=CreateFileW(lockPath.c_str(),GENERIC_READ|GENERIC_WRITE,0,nullptr,OPEN_ALWAYS,FILE_ATTRIBUTE_NORMAL,nullptr);
  if(handle==INVALID_HANDLE_VALUE) throw std::runtime_error("EEPROM image is already owned or lock path is inaccessible");
  lockHandle_=handle;
#else
  lockFd_=::open(lockPath.c_str(),O_RDWR|O_CREAT|O_CLOEXEC,0600);
  if(lockFd_<0 || flock(lockFd_,LOCK_EX|LOCK_NB)!=0) throw std::runtime_error("EEPROM image is already owned or lock path is inaccessible");
#endif
  if(!std::filesystem::exists(path)) return;
  std::ifstream in(path,std::ios::binary);
  if(!in || std::filesystem::file_size(path)!=bytes_.size() || !in.read(reinterpret_cast<char *>(bytes_.data()),bytes_.size()))
    throw std::runtime_error("EEPROM image must be exactly 1024 bytes; refusing to overwrite");
}
void EEPROMClass::flush() {
  if(!dirty_ || path_.empty()) return;
  auto pending=path_; pending += ".pending";
  { std::ofstream out(pending,std::ios::binary|std::ios::trunc);
    if(!out || !out.write(reinterpret_cast<const char *>(bytes_.data()),bytes_.size()) || !out.flush()) throw std::runtime_error("EEPROM write failed"); }
#ifdef _WIN32
  if(!MoveFileExW(pending.c_str(),path_.c_str(),MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)) throw std::runtime_error("EEPROM replacement failed");
#else
  std::filesystem::rename(pending,path_);
#endif
  dirty_=false;
}
EEPROMClass::~EEPROMClass() { unlock(); }
void EEPROMClass::unlock() {
#ifdef _WIN32
  if(lockHandle_) { CloseHandle(lockHandle_); lockHandle_=nullptr; }
#else
  if(lockFd_>=0) { close(lockFd_); lockFd_=-1; }
#endif
}
std::string EEPROMClass::snapshotHex() const {
  static const char digits[]="0123456789abcdef";
  std::string hex; hex.reserve(bytes_.size()*2);
  for(auto b:bytes_) { hex.push_back(digits[b>>4]); hex.push_back(digits[b&15]); }
  return hex;
}
