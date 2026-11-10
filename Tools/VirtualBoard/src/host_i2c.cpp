#include "virtual_board/host_i2c.hpp"
#include <array>
#include <fstream>
#include <iostream>
#include <stdexcept>
#ifdef __linux__
#include <fcntl.h>
#include <linux/i2c-dev.h>
#include <unistd.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#endif
namespace NativeHal {
namespace {
struct Bus {
  int fd=-1;
  bool selected=false;
  std::array<uint8_t,64> output{};
  size_t length=0;
  ~Bus() {
#ifdef __linux__
    if(fd>=0) ::close(fd);
#endif
  }
} bus;
}
void configureHostI2c(const std::string &device) {
  if(device=="off") return;
#ifdef __linux__
  const bool automatic=device=="auto";
  std::string path=device;
  if(automatic) {
    std::ifstream model("/proc/device-tree/model");
    std::string name; std::getline(model,name,'\0');
    if(name.find("Raspberry Pi")==std::string::npos) return;
    path="/dev/i2c-1";
  }
  // Device identity is explicit; never scan a motherboard SMBus or force a
  // kernel-bound address. Only the production PWM device at 0x41 is forwarded.
  if(path.rfind("/dev/i2c-",0)!=0 || path.find("..")!=std::string::npos || path.size()>128)
    throw std::invalid_argument("I2C device must be an explicit /dev/i2c-N adapter");
  const int fd=::open(path.c_str(),O_RDWR|O_CLOEXEC|O_NOFOLLOW);
  if(fd<0) {
    if(automatic) { std::cerr<<"peripheral: Raspberry Pi I2C unavailable; check i2c-dev/access permissions\n"; return; }
    throw std::runtime_error("cannot open selected I2C adapter");
  }
  struct stat status{};
  if(::fstat(fd,&status)<0 || !S_ISCHR(status.st_mode)){::close(fd);throw std::invalid_argument("I2C adapter must be a character device");}
  // Do not change adapter-wide timeout/retry settings for unrelated clients.
  // Kernel-driver transfer deadlines still govern physical I2C operations.
  const uint8_t modeRegister=0;
  uint8_t mode=0;
  const bool present=::ioctl(fd,I2C_SLAVE,0x41UL)==0 && ::write(fd,&modeRegister,1)==1 && ::read(fd,&mode,1)==1;
  if(!present) {
    ::close(fd);
    if(automatic) { std::cerr<<"peripheral: no unclaimed PCA9685 at 0x41; native PWM remains virtual\n"; return; }
    throw std::runtime_error("selected I2C adapter has no accessible PCA9685 at 0x41");
  }
  bus.fd=fd;
  std::cerr<<"peripheral: production PCA9685 uses "<<path<<" address 0x41\n";
#else
  if(device!="auto") throw std::runtime_error("i2c-dev adapters require Linux; no driver is installed automatically");
#endif
}
bool hostI2cEnabled() { return bus.fd>=0; }
uint8_t hostI2cBegin(uint8_t address) {
  bus.length=0; bus.selected=false;
#ifdef __linux__
  bus.selected=bus.fd>=0 && address==0x41 && ::ioctl(bus.fd,I2C_SLAVE,0x41UL)==0;
#else
  (void)address;
#endif
  return bus.selected?0:2;
}
bool hostI2cWrite(uint8_t value) {
  if(!bus.selected || bus.length==bus.output.size()) return false;
  bus.output[bus.length++]=value; return true;
}
uint8_t hostI2cEnd() {
#ifdef __linux__
  if(bus.selected && ::write(bus.fd,bus.output.data(),bus.length)==static_cast<ssize_t>(bus.length)) return 0;
#endif
  return 2;
}
uint8_t hostI2cRead(uint8_t address,uint8_t *values,uint8_t length) {
#ifdef __linux__
  if(bus.fd>=0 && address==0x41 && ::ioctl(bus.fd,I2C_SLAVE,0x41UL)==0 &&
     ::read(bus.fd,values,length)==length) return length;
#else
  (void)address; (void)values; (void)length;
#endif
  return 0;
}
}
