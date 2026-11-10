#include "virtual_board/host_peripherals.hpp"
#include "virtual_board/openrgb_sink.hpp"
#include <algorithm>
#include <array>
#include <chrono>
#include <condition_variable>
#include <deque>
#include <iostream>
#include <mutex>
#include <stdexcept>
#include <thread>
#include <functional>
#include <filesystem>
#include <sstream>
#include <map>
#include <set>
#ifdef __linux__
#include <fcntl.h>
#include <linux/gpio.h>
#include <linux/ppdev.h>
#include <linux/parport.h>
#include <linux/cdrom.h>
#include <sys/ioctl.h>
#include <unistd.h>
#include <sys/stat.h>
#endif
#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#include <winioctl.h>
#include <ntddkbd.h>
#endif
namespace pccontroller::virtual_board {
namespace {
using Clock=std::chrono::steady_clock;
unsigned bounded(const std::string &text,unsigned low,unsigned high) {
  size_t used=0; const auto number=std::stoul(text,&used);
  if(used!=text.size() || number<low || number>high) throw std::invalid_argument("peripheral assignment number out of range");
  return static_cast<unsigned>(number);
}
struct Mapping { unsigned channel; std::string path; unsigned bit=0; };
Mapping mapping(const std::string &text,bool bit) {
  const auto equal=text.find('=');
  if(equal==std::string::npos) throw std::invalid_argument("mapping requires channel=device[:line]");
  Mapping value{bounded(text.substr(0,equal),1,8),text.substr(equal+1),0};
  if(value.path.empty() || value.path.size()>512 || value.path.find('\0')!=std::string::npos || value.path.find("..")!=std::string::npos)
    throw std::invalid_argument("invalid peripheral device path");
  if(bit) {
    const auto colon=value.path.rfind(':');
    if(colon==std::string::npos) throw std::invalid_argument("mapping requires device:line");
    value.bit=bounded(value.path.substr(colon+1),0,1023); value.path.resize(colon);
  }
  return value;
}
struct Sink { unsigned channel; bool failed=false; std::function<void(bool)> write; std::function<void()> close; };
#ifdef __linux__
int device(const std::string &path,const std::string &prefix,bool block=false) {
  if(path.rfind(prefix,0)!=0) throw std::invalid_argument("peripheral device path has wrong class");
  const int fd=::open(path.c_str(),O_RDWR|O_CLOEXEC|O_NOFOLLOW|O_NONBLOCK);
  if(fd<0) throw std::runtime_error("cannot open assigned peripheral "+path);
  struct stat status{};
  if(::fstat(fd,&status)<0 || (block?!S_ISBLK(status.st_mode):!S_ISCHR(status.st_mode))){::close(fd);throw std::runtime_error("assigned peripheral has the wrong OS device class");}
  return fd;
}
int gpio(const Mapping &value,bool output) {
  const int chip=device(value.path,"/dev/gpiochip");
  gpio_v2_line_request request{};
  request.offsets[0]=value.bit; request.num_lines=1;
  request.config.flags=output?GPIO_V2_LINE_FLAG_OUTPUT:GPIO_V2_LINE_FLAG_INPUT;
  const std::string consumer="PCController R"+std::to_string(value.channel);
  std::copy(consumer.begin(),consumer.end(),request.consumer);
  const int result=::ioctl(chip,GPIO_V2_GET_LINE_IOCTL,&request);
  ::close(chip);
  if(result<0) throw std::runtime_error("GPIO line unavailable, reserved or permission denied");
  return request.fd;
}
void gpioWrite(int fd,bool on) {
  gpio_v2_line_values values{}; values.mask=1; values.bits=on?1:0;
  if(::ioctl(fd,GPIO_V2_LINE_SET_VALUES_IOCTL,&values)<0) throw std::runtime_error("GPIO output failed");
}
struct ParallelPort {
  int fd;unsigned char original=0;
  explicit ParallelPort(const std::string &path):fd(device(path,"/dev/parport")) {
    if(::ioctl(fd,PPCLAIM)<0){::close(fd);throw std::runtime_error("parallel port is owned or unavailable");}
    int direction=0;
    if(::ioctl(fd,PPRDATA,&original)<0 || ::ioctl(fd,PPDATADIR,&direction)<0){::ioctl(fd,PPRELEASE);::close(fd);throw std::runtime_error("parallel port is not usable");}
  }
  ~ParallelPort(){(void)::ioctl(fd,PPWDATA,&original);(void)::ioctl(fd,PPRELEASE);::close(fd);}
};
#endif
}
struct HostPeripherals::Impl {
  VirtualBoard &board;
  std::vector<Sink> sinks;
  struct Input { unsigned channel; int fd; int previous=-1; };
  std::vector<Input> inputs;
  std::mutex mutex;
  std::condition_variable wake;
  struct Edge { uint8_t relays; uint16_t hz; Clock::time_point time; };
  std::deque<Edge> queue;
  bool stopping=false, overflow=false, have=false;
  uint8_t lastRelays=0;
  uint16_t lastHz=0;
  std::thread worker;
  int speaker=-1;
  std::unique_ptr<OpenRgbSink> rgb;
#ifdef _WIN32
  HANDLE winSpeaker=INVALID_HANDLE_VALUE;
  bool portWrite(uint32_t port,uint8_t value) {
    const uint32_t data[2]={port,value}; DWORD returned=0;
    return DeviceIoControl(winSpeaker,(40000UL<<16)|(2UL<<14)|(0x836UL<<2),const_cast<uint32_t*>(data),sizeof(data),nullptr,0,&returned,nullptr)!=0;
  }
  bool portRead(uint32_t port,uint8_t &value) {
    uint32_t output=0; DWORD returned=0;
    uint16_t input=static_cast<uint16_t>(port);
    if(!DeviceIoControl(winSpeaker,(40000UL<<16)|(1UL<<14)|(0x833UL<<2),&input,sizeof(input),&output,sizeof(output),&returned,nullptr) || returned<1) return false;
    value=static_cast<uint8_t>(output); return true;
  }
#endif
  explicit Impl(VirtualBoard &b,const HostPeripheralOptions &options):board(b) {
    try {
      std::array<bool,8> assigned{};
      std::set<std::string> physicalOutputs;
      auto reservePhysical=[&](const std::string &key){if(!physicalOutputs.insert(key).second)throw std::invalid_argument("physical peripheral is already assigned to another relay");};
#ifdef __linux__
      std::map<std::string,std::shared_ptr<ParallelPort>> parallelPorts;
#endif
      auto reserve=[&](unsigned channel) { if(assigned[channel-1]) throw std::invalid_argument("one physical output sink per relay assignment"); assigned[channel-1]=true; };
      for(const auto &spec:options.relayGpio) {
        const auto value=mapping(spec,true); reserve(value.channel);
#ifdef __linux__
        const int fd=gpio(value,true);
        sinks.push_back({value.channel,false,[fd](bool on){gpioWrite(fd,on);},[fd]{try{gpioWrite(fd,false);}catch(...){} ::close(fd);}});
#else
        throw std::runtime_error("GPIO character-device mappings require Linux");
#endif
      }
      for(const auto &spec:options.inputGpio) {
        const auto value=mapping(spec,true);
        if(std::any_of(inputs.begin(),inputs.end(),[&](const Input &i){return i.channel==value.channel;})) throw std::invalid_argument("duplicate input mapping");
#ifdef __linux__
        inputs.push_back({value.channel,gpio(value,false),-1});
        board.bindHostInput(static_cast<uint8_t>(value.channel));
#else
        throw std::runtime_error("GPIO character-device mappings require Linux");
#endif
      }
      for(const auto &spec:options.relayLed) {
        const auto value=mapping(spec,false); reserve(value.channel);
#ifdef __linux__
        const auto path=std::filesystem::weakly_canonical(value.path);
        if(value.path.rfind("/sys/class/leds/",0)!=0 || path.filename()!="brightness" || path.string().rfind("/sys/devices/",0)!=0)
          throw std::invalid_argument("LED mapping requires /sys/class/leds/<device>/brightness");
        reservePhysical("led:"+path.string());
        const int fd=::open(path.c_str(),O_RDWR|O_CLOEXEC|O_NOFOLLOW);
        if(fd<0) throw std::runtime_error("cannot open assigned LED brightness");
        char original[16]{}; const auto n=::pread(fd,original,sizeof(original)-1,0);
        if(n<=0) {::close(fd);throw std::runtime_error("cannot preserve LED brightness");}
        const std::string restore(original,static_cast<size_t>(n));
        sinks.push_back({value.channel,false,[fd](bool on){const char c=on?'1':'0';if(::pwrite(fd,&c,1,0)!=1)throw std::runtime_error("LED update failed");},[fd,restore]{(void)::pwrite(fd,restore.data(),restore.size(),0);::close(fd);}});
#else
        throw std::runtime_error("LED-class mappings require Linux; no keyboard key events are injected");
#endif
      }
      for(const auto &spec:options.relayParallel) {
        const auto value=mapping(spec,true); reserve(value.channel);
#ifdef __linux__
        if(value.bit>7) throw std::invalid_argument("parallel data pin bit must be 0..7 (DB25 pins 2..9)");
        reservePhysical("parallel:"+value.path+":"+std::to_string(value.bit));
        auto &port=parallelPorts[value.path];if(!port)port=std::make_shared<ParallelPort>(value.path);
        const auto mask=static_cast<unsigned char>(1U<<value.bit);
        sinks.push_back({value.channel,false,[port,mask](bool on){unsigned char data=0;if(::ioctl(port->fd,PPRDATA,&data)<0)throw std::runtime_error("parallel read failed");data=on?static_cast<unsigned char>(data|mask):static_cast<unsigned char>(data&~mask);if(::ioctl(port->fd,PPWDATA,&data)<0)throw std::runtime_error("parallel write failed");},[]{}});
#else
        throw std::runtime_error("parallel-port assignments require Linux ppdev; raw x86 I/O is not used");
#endif
      }
      for(const auto &spec:options.relayOptical) {
        const auto value=mapping(spec,false); reserve(value.channel);
#ifdef __linux__
        reservePhysical("optical:"+value.path);
        const int fd=device(value.path,"/dev/sr",true);
        sinks.push_back({value.channel,false,[fd](bool on){if(::ioctl(fd,on?CDROMEJECT:CDROMCLOSETRAY)<0)throw std::runtime_error("optical tray operation refused (possibly busy/mounted)");},[fd]{::close(fd);}});
#elif defined(_WIN32)
        if(value.path.size()!=2 || value.path[1]!=':' || value.path[0]<'A' || value.path[0]>'Z') throw std::invalid_argument("optical mapping requires an explicit drive such as D:");
        reservePhysical("optical:"+value.path);
        if(GetDriveTypeA((value.path+"\\").c_str())!=DRIVE_CDROM)throw std::invalid_argument("assigned drive is not an optical drive");
        const std::string path="\\\\.\\"+value.path;
        HANDLE handle=CreateFileA(path.c_str(),GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,0,nullptr);
        if(handle==INVALID_HANDLE_VALUE)throw std::runtime_error("cannot open assigned optical drive");
        sinks.push_back({value.channel,false,[handle](bool on){DWORD returned=0;if(!DeviceIoControl(handle,on?IOCTL_STORAGE_EJECT_MEDIA:IOCTL_STORAGE_LOAD_MEDIA,nullptr,0,nullptr,0,&returned,nullptr))throw std::runtime_error("optical tray operation refused");},[handle]{CloseHandle(handle);}});
#else
        throw std::runtime_error("optical tray mapping is not implemented on this platform");
#endif
      }
      for(const auto &spec:options.relayScrollLock) {
        const auto channel=bounded(spec,1,8);reserve(channel);
#ifdef _WIN32
        reservePhysical("keyboard:scroll-lock");
        HANDLE handle=CreateFileW(L"\\\\?\\GLOBALROOT\\Device\\KeyboardClass0",GENERIC_READ|GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,0,nullptr);
        if(handle==INVALID_HANDLE_VALUE)throw std::runtime_error("keyboard indicator driver access unavailable; no key injection fallback");
        KEYBOARD_INDICATOR_PARAMETERS original{}; DWORD returned=0;
        if(!DeviceIoControl(handle,IOCTL_KEYBOARD_QUERY_INDICATORS,nullptr,0,&original,sizeof(original),&returned,nullptr)) {CloseHandle(handle);throw std::runtime_error("cannot read/preserve keyboard indicators");}
        sinks.push_back({channel,false,[handle](bool on){KEYBOARD_INDICATOR_PARAMETERS state{};DWORD size=0;if(!DeviceIoControl(handle,IOCTL_KEYBOARD_QUERY_INDICATORS,nullptr,0,&state,sizeof(state),&size,nullptr))throw std::runtime_error("cannot read keyboard indicator state");state.LedFlags=on?static_cast<USHORT>(state.LedFlags|KEYBOARD_SCROLL_LOCK_ON):static_cast<USHORT>(state.LedFlags&~KEYBOARD_SCROLL_LOCK_ON);if(!DeviceIoControl(handle,IOCTL_KEYBOARD_SET_INDICATORS,&state,sizeof(state),nullptr,0,&size,nullptr))throw std::runtime_error("cannot set keyboard indicator");},[handle,original]{KEYBOARD_INDICATOR_PARAMETERS state{};DWORD size=0;if(DeviceIoControl(handle,IOCTL_KEYBOARD_QUERY_INDICATORS,nullptr,0,&state,sizeof(state),&size,nullptr)){state.LedFlags=static_cast<USHORT>((state.LedFlags&~KEYBOARD_SCROLL_LOCK_ON)|(original.LedFlags&KEYBOARD_SCROLL_LOCK_ON));(void)DeviceIoControl(handle,IOCTL_KEYBOARD_SET_INDICATORS,&state,sizeof(state),nullptr,0,&size,nullptr);}CloseHandle(handle);}});
#else
        throw std::runtime_error("use --relay-led with an explicit LED-class Scroll Lock device on Linux");
#endif
      }
      if(options.speaker) {
#ifdef __linux__
        for(const char *path:{"/dev/console","/dev/tty0"}) {
          speaker=::open(path,O_WRONLY|O_CLOEXEC|O_NOFOLLOW);
          if(speaker>=0 && ::ioctl(speaker,0x4B30,0UL)==0)break;
          if(speaker>=0)::close(speaker);
          speaker=-1;
        }
#elif defined(_WIN32)
        // Reuse an already-installed/loaded native driver only. Never install,
        // elevate, or substitute a sound-card Beep for a physical PC speaker.
        winSpeaker=CreateFileW(L"\\\\.\\WinRing0_1_2_0",GENERIC_READ|GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,0,nullptr);
#endif
      }
      if(!options.stripOpenrgb.empty())rgb=std::make_unique<OpenRgbSink>(options.stripOpenrgb);
      worker=std::thread([this]{run();});
    } catch(...) {cleanup();throw;}
  }
  void tone(uint16_t hz) {
#ifdef __linux__
    if(speaker<0)return;
    const unsigned divisor=hz?1193182U/hz:0;
    const unsigned argument=hz?((250U<<16)|std::clamp(divisor,1U,65535U)):0;
    if(::ioctl(speaker,0x4B30,argument)<0) {std::cerr<<"peripheral: PC speaker failed; disabled\n";::close(speaker);speaker=-1;}
#elif defined(_WIN32)
    if(winSpeaker==INVALID_HANDLE_VALUE)return;
    uint8_t gate=0;
    bool ok=portRead(0x61,gate);
    if(hz) {
      const auto divisor=std::clamp(1193182U/hz,1U,65535U);
      ok=ok && portWrite(0x43,0xB6) && portWrite(0x42,static_cast<uint8_t>(divisor)) && portWrite(0x42,static_cast<uint8_t>(divisor>>8));
    }
    ok=ok && portWrite(0x61,hz?static_cast<uint8_t>(gate|3):static_cast<uint8_t>(gate&~3));
    if(!ok){std::cerr<<"peripheral: native PC speaker failed; disabled\n";CloseHandle(winSpeaker);winSpeaker=INVALID_HANDLE_VALUE;}
#else
    (void)hz;
#endif
  }
  void cleanup() {
    tone(0);
    for(auto &sink:sinks) if(sink.close)sink.close();
    sinks.clear();
#ifdef __linux__
    for(const auto &input:inputs)::close(input.fd);
    if(speaker>=0){::close(speaker);speaker=-1;}
#endif
    inputs.clear();
#ifdef _WIN32
    if(winSpeaker!=INVALID_HANDLE_VALUE){CloseHandle(winSpeaker);winSpeaker=INVALID_HANDLE_VALUE;}
#endif
  }
  ~Impl() {
    {std::lock_guard<std::mutex> lock(mutex);stopping=true;queue.clear();}
    wake.notify_one(); if(worker.joinable())worker.join(); cleanup();
  }
  void run() {
    uint16_t hz=0; uint8_t relays=0; bool baseline=false;
    auto renewed=Clock::now();
    for(;;) {
      Edge edge{}; bool ready=false, overrun=false;
      {std::unique_lock<std::mutex> lock(mutex);wake.wait_for(lock,std::chrono::milliseconds(5),[&]{return stopping||overflow||!queue.empty();});if(stopping)return;overrun=overflow;overflow=false;if(!queue.empty()){edge=queue.front();queue.pop_front();ready=true;}}
      if(overrun) {hz=0;tone(0);for(auto &sink:sinks){sink.failed=true;try{sink.close();}catch(...){}sink.close={};sink.write={};}std::cerr<<"peripheral: output backpressure; mapped outputs disabled until restart\n";}
      if(ready) {
        // Do not replay stale queued actuation after a slow device operation.
        if(Clock::now()-edge.time>std::chrono::milliseconds(250)){hz=0;tone(0);continue;}
        for(auto &sink:sinks) if(!sink.failed && baseline && ((relays^edge.relays)&(1U<<(sink.channel-1)))) {
          try{sink.write((edge.relays&(1U<<(sink.channel-1)))!=0);}catch(const std::exception &e){sink.failed=true;std::cerr<<"peripheral R"<<sink.channel<<": "<<e.what()<<"; no retry\n";}
        }
        relays=edge.relays;baseline=true;
        if(edge.hz!=hz){hz=edge.hz;tone(hz);renewed=Clock::now();}
      }
      if(hz && Clock::now()-renewed>=std::chrono::milliseconds(100)){tone(hz);renewed=Clock::now();}
#ifdef __linux__
      for(auto &input:inputs) if(input.fd>=0) {
        gpio_v2_line_values value{};value.mask=1;
        if(::ioctl(input.fd,GPIO_V2_LINE_GET_VALUES_IOCTL,&value)<0){std::cerr<<"peripheral: GPIO input lost; releasing mapped input\n";board.observeHostInput(static_cast<uint8_t>(input.channel),false);::close(input.fd);input.fd=-1;continue;}
        const int active=(value.bits&1)?0:1;
        if(active!=input.previous){board.observeHostInput(static_cast<uint8_t>(input.channel),active!=0);input.previous=active;}
      }
#endif
    }
  }
};
HostPeripherals::HostPeripherals(VirtualBoard &board,const HostPeripheralOptions &options):impl_(new Impl(board,options)){}
HostPeripherals::~HostPeripherals()=default;
void HostPeripherals::observe(const OutputSnapshot &s) {
  if(impl_->rgb) {
    std::vector<uint8_t> rgb;rgb.reserve(s.strip.size());
    for(size_t i=0;i+2<s.strip.size();i+=3){rgb.push_back(s.strip[i+s.stripRedOffset]);rgb.push_back(s.strip[i+s.stripGreenOffset]);rgb.push_back(s.strip[i+s.stripBlueOffset]);}
    impl_->rgb->observe(rgb);
  }
  std::lock_guard<std::mutex> lock(impl_->mutex);
  if(impl_->have && s.relayMask==impl_->lastRelays && s.buzzerHz==impl_->lastHz)return;
  impl_->have=true;impl_->lastRelays=s.relayMask;impl_->lastHz=s.buzzerHz;
  if(impl_->queue.size()==64){impl_->queue.clear();impl_->overflow=true;}
  impl_->queue.push_back({s.relayMask,s.buzzerHz,Clock::now()});impl_->wake.notify_one();
}
std::string peripheralCandidates() {
  std::ostringstream output;
  output<<"Dedicated peripherals: PC speaker=auto, Raspberry Pi PCA9685 I2C=auto\n";
#ifdef __linux__
  for(const auto &entry:std::filesystem::directory_iterator("/dev")) {
    const auto name=entry.path().filename().string();
    for(const char *prefix:{"gpiochip","i2c-","parport","sr"})
      if(name.rfind(prefix,0)==0)output<<"candidate\t"<<entry.path().string()<<'\n';
  }
  std::error_code error;
  if(std::filesystem::is_directory("/sys/class/leds",error))
    for(const auto &entry:std::filesystem::directory_iterator("/sys/class/leds"))output<<"LED candidate\t"<<(entry.path()/"brightness").string()<<'\n';
#elif defined(_WIN32)
  for(char drive='A';drive<='Z';++drive) {
    const std::string root=std::string(1,drive)+":\\";
    if(GetDriveTypeA(root.c_str())==DRIVE_CDROM)output<<"optical candidate\t"<<drive<<":\n";
  }
  output<<"PC speaker requires an already-loaded WinRing0 native driver; keyboard LED access requires the OS indicator driver.\n";
#else
  output<<"No dedicated native peripheral backend on this platform; explicit scalar sensor files remain available.\n";
#endif
  output<<"Candidates are not verified, assigned or actuated by this inventory. USB devices are supported only through a matching OS device class/driver.\n";
  return output.str();
}
}
