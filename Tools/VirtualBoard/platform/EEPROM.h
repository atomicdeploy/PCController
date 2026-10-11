#pragma once
#include <array>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <stdexcept>
#include <string>

class EEPROMClass {
public:
  EEPROMClass() { bytes_.fill(0xFF); }
  ~EEPROMClass();
  template<class T> T &get(int address,T &v) const {
    check(address,sizeof(T)); std::memcpy(&v,bytes_.data()+address,sizeof(T)); return v;
  }
  template<class T> void put(int address,const T &v) {
    check(address,sizeof(T)); const auto *p=reinterpret_cast<const uint8_t *>(&v);
    for(size_t i=0;i<sizeof(T);++i) update(address+static_cast<int>(i),p[i]);
  }
  uint8_t read(int address) const { check(address,1); return bytes_[address]; }
  void update(int address,uint8_t v) { check(address,1); if(bytes_[address]!=v) { bytes_[address]=v; dirty_=true; } }
  void open(const std::filesystem::path &path);
  void flush();
  void unlock();
  std::string snapshotHex() const;
private:
  void check(int a,size_t n) const { if(a<0 || n>bytes_.size() || static_cast<size_t>(a)>bytes_.size()-n) throw std::out_of_range("EEPROM bounds"); }
  std::array<uint8_t,1024> bytes_;
  std::filesystem::path path_;
  bool dirty_=false;
  void *lockHandle_=nullptr;
  int lockFd_=-1;
};
inline EEPROMClass EEPROM;
