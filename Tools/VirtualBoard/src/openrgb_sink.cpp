// OpenRGB SDK protocol 1/2, negotiated explicitly. No device rescan, profile
// writes, motherboard SMBus probing, automatic reconnect or index reassignment.
#include "virtual_board/openrgb_sink.hpp"
#include <algorithm>
#include <array>
#include <chrono>
#include <condition_variable>
#include <iostream>
#include <mutex>
#include <stdexcept>
#include <thread>
#include <cstring>
#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <fcntl.h>
#include <sys/socket.h>
#include <sys/select.h>
#include <unistd.h>
#endif
namespace pccontroller::virtual_board {
namespace {
#ifdef _WIN32
using Socket=SOCKET;
constexpr Socket Invalid=INVALID_SOCKET;
void closeSocket(Socket s){if(s!=Invalid)closesocket(s);}
#else
using Socket=int;
constexpr Socket Invalid=-1;
void closeSocket(Socket s){if(s!=Invalid)::close(s);}
#endif
using Bytes=std::vector<uint8_t>;
void put32(Bytes &b,uint32_t value){for(unsigned i=0;i<4;++i)b.push_back(static_cast<uint8_t>(value>>(8*i)));}
uint32_t get32(const uint8_t *b){return uint32_t(b[0])|(uint32_t(b[1])<<8)|(uint32_t(b[2])<<16)|(uint32_t(b[3])<<24);}
struct Reader {
  const Bytes &bytes;size_t at=0;
  void skip(size_t n){if(n>bytes.size()-at)throw std::runtime_error("truncated OpenRGB metadata");at+=n;}
  uint16_t u16(){skip(2);return static_cast<uint16_t>(bytes[at-2]|(uint16_t(bytes[at-1])<<8));}
  uint32_t u32(){skip(4);return get32(bytes.data()+at-4);}
  std::string text(){const auto n=u16();if(n==0 || n>4096)throw std::runtime_error("invalid OpenRGB string");skip(n);if(bytes[at-1]!=0)throw std::runtime_error("unterminated OpenRGB string");return std::string(reinterpret_cast<const char*>(bytes.data()+at-n),n-1);}
};
bool ready(Socket socket,bool writing,int milliseconds){
  fd_set set;FD_ZERO(&set);FD_SET(socket,&set);timeval wait{milliseconds/1000,(milliseconds%1000)*1000};
#ifdef _WIN32
  const int limit=0;
#else
  const int limit=socket+1;
#endif
  return ::select(limit,writing?nullptr:&set,writing?&set:nullptr,nullptr,&wait)>0;
}
void transfer(Socket socket,uint8_t *buffer,size_t size,bool writing){
  const auto deadline=std::chrono::steady_clock::now()+std::chrono::milliseconds(500);
  while(size){
    const auto left=std::chrono::duration_cast<std::chrono::milliseconds>(deadline-std::chrono::steady_clock::now()).count();
    if(left<=0 || !ready(socket,writing,static_cast<int>(left)))throw std::runtime_error("OpenRGB transport timed out");
#ifdef MSG_NOSIGNAL
    constexpr int flags=MSG_NOSIGNAL;
#else
    constexpr int flags=0;
#endif
    const auto n=writing?::send(socket,reinterpret_cast<const char*>(buffer),static_cast<int>(size),flags)
                        : ::recv(socket,reinterpret_cast<char*>(buffer),static_cast<int>(size),0);
    if(n<=0)throw std::runtime_error("OpenRGB disconnected or rejected transfer");
    buffer+=n;size-=static_cast<size_t>(n);
  }
}
}
struct OpenRgbSink::Impl {
  uint32_t device=0;std::string ip;uint16_t port=0;
  Socket socket=Invalid;
  std::mutex mutex;std::condition_variable wake;bool stopping=false,dirty=false,failed=false;
  Bytes latest,previous;std::thread worker;
  explicit Impl(const std::string &assignment){
    const auto at=assignment.find('@'),colon=assignment.rfind(':');
    if(at==std::string::npos || colon==std::string::npos || at>=colon)throw std::invalid_argument("OpenRGB assignment requires device@IPv4:port");
    auto number=[](const std::string &s,uint32_t maximum){size_t used=0;auto n=std::stoul(s,&used);if(used!=s.size()||n>maximum)throw std::invalid_argument("invalid OpenRGB assignment number");return static_cast<uint32_t>(n);};
    device=number(assignment.substr(0,at),4095);port=static_cast<uint16_t>(number(assignment.substr(colon+1),65535));
    ip=assignment.substr(at+1,colon-at-1);if(port==0 || ip.empty() || ip.size()>15)throw std::invalid_argument("invalid OpenRGB endpoint");
    worker=std::thread([this]{run();});
  }
  ~Impl(){ {std::lock_guard<std::mutex> lock(mutex);stopping=true;}wake.notify_one();if(worker.joinable())worker.join(); }
  void send(uint32_t id,const Bytes &payload={}){
    Bytes packet{'O','R','G','B'};put32(packet,device);put32(packet,id);put32(packet,static_cast<uint32_t>(payload.size()));packet.insert(packet.end(),payload.begin(),payload.end());
    transfer(socket,packet.data(),packet.size(),true);
  }
  std::pair<uint32_t,Bytes> receive(){
    std::array<uint8_t,16> header{};transfer(socket,header.data(),header.size(),false);
    if(std::memcmp(header.data(),"ORGB",4)!=0)throw std::runtime_error("invalid OpenRGB magic");
    const auto id=get32(header.data()+8),size=get32(header.data()+12);
    if(id==1 && get32(header.data()+4)!=device)throw std::runtime_error("OpenRGB metadata belongs to another device");
    if(id==100)throw std::runtime_error("OpenRGB device list changed; explicit reassignment/restart required");
    if(size>1048576)throw std::runtime_error("OpenRGB packet exceeds bound");
    Bytes payload(size);if(size)transfer(socket,payload.data(),payload.size(),false);
    return {id,std::move(payload)};
  }
  Bytes response(uint32_t expected){for(unsigned i=0;i<16;++i){auto packet=receive();if(packet.first==expected)return packet.second;}throw std::runtime_error("OpenRGB response flood");}
  void connect(){
    socket=::socket(AF_INET,SOCK_STREAM,0);if(socket==Invalid)throw std::runtime_error("OpenRGB socket unavailable");
#ifdef _WIN32
    u_long enabled=1;if(ioctlsocket(socket,FIONBIO,&enabled)!=0)throw std::runtime_error("OpenRGB nonblocking mode failed");
#else
    if(fcntl(socket,F_SETFL,fcntl(socket,F_GETFL)|O_NONBLOCK)<0)throw std::runtime_error("OpenRGB nonblocking mode failed");
#endif
    sockaddr_in address{};address.sin_family=AF_INET;address.sin_port=htons(port);
    if(inet_pton(AF_INET,ip.c_str(),&address.sin_addr)!=1)throw std::invalid_argument("OpenRGB requires an IPv4 literal");
    (void)::connect(socket,reinterpret_cast<sockaddr*>(&address),sizeof(address));
    if(!ready(socket,true,500))throw std::runtime_error("OpenRGB connect timed out");
    int error=0;
#ifdef _WIN32
    int length=sizeof(error);
#else
    socklen_t length=sizeof(error);
#endif
    if(getsockopt(socket,SOL_SOCKET,SO_ERROR,reinterpret_cast<char*>(&error),&length)<0 || error)throw std::runtime_error("OpenRGB connect failed");
  }
  uint16_t commission(){
    Bytes version;put32(version,2);send(40,version);const auto advertised=response(40);
    if(advertised.size()!=4 || get32(advertised.data())==0)throw std::runtime_error("OpenRGB SDK protocol 1+ required");
    const auto negotiated=std::min(2U,get32(advertised.data()));
    send(0);const auto count=response(0);
    if(count.size()!=4 || device>=get32(count.data()))throw std::runtime_error("assigned OpenRGB device is absent");
    Bytes request;put32(request,negotiated);send(1,request);const auto data=response(1);
    Reader reader{data};if(reader.u32()!=data.size())throw std::runtime_error("invalid OpenRGB metadata size");
    reader.skip(4);auto name=reader.text();reader.text(); // name/vendor, protocol >=1
    for(char &c:name)if(static_cast<unsigned char>(c)<32 || c==127)c=' ';
    for(unsigned i=0;i<4;++i)reader.text(); // description/version/serial/location
    const auto modes=reader.u16();if(modes>256)throw std::runtime_error("too many OpenRGB modes");reader.skip(4);
    for(unsigned i=0;i<modes;++i){reader.text();reader.skip(36);reader.skip(size_t(reader.u16())*4);}
    const auto zones=reader.u16();if(zones>1024)throw std::runtime_error("too many OpenRGB zones");
    for(unsigned i=0;i<zones;++i){reader.text();reader.skip(16);reader.skip(reader.u16());}
    const auto leds=reader.u16();if(leds==0 || leds>4096)throw std::runtime_error("OpenRGB LED count out of bounds");
    for(unsigned i=0;i<leds;++i){reader.text();reader.skip(4);}
    const auto colors=reader.u16();reader.skip(size_t(colors)*4);
    if(colors!=leds || reader.at!=data.size())throw std::runtime_error("OpenRGB metadata/count mismatch");
    send(50,{'P','C','C','o','n','t','r','o','l','l','e','r',0});send(1100);
    std::cerr<<"peripheral: OpenRGB assigned device "<<device<<" ("<<name<<"), "<<leds<<" LEDs; direct mode\n";
    return leds;
  }
  void run(){
#ifdef _WIN32
    WSADATA wsa{};const bool initialized=WSAStartup(MAKEWORD(2,2),&wsa)==0;
    if(!initialized){std::lock_guard<std::mutex> lock(mutex);failed=true;std::cerr<<"peripheral: OpenRGB Winsock unavailable\n";return;}
#endif
    try {
      connect();const auto leds=commission();
      for(;;){
        Bytes rgb;
        {std::unique_lock<std::mutex> lock(mutex);wake.wait_for(lock,std::chrono::milliseconds(25),[&]{return stopping||dirty;});if(stopping)break;if(dirty){rgb=latest;dirty=false;}}
        // Stop on device topology change instead of writing a new occupant of
        // an old index. Never retry an unknown delivery or reconnect silently.
        for(unsigned i=0;i<16 && ready(socket,false,0);++i)(void)receive();
        if(rgb.empty())continue;
        Bytes frame;put32(frame,6+uint32_t(leds)*4);frame.push_back(static_cast<uint8_t>(leds));frame.push_back(static_cast<uint8_t>(leds>>8));
        for(size_t i=0;i<leds;++i){for(size_t c=0;c<3;++c)frame.push_back(i*3+c<rgb.size()?rgb[i*3+c]:0);frame.push_back(0);}
        send(1050,frame);
      }
    }catch(const std::exception &e){std::cerr<<"peripheral: "<<e.what()<<"; OpenRGB disabled until restart\n";}
    closeSocket(socket);socket=Invalid;
    {std::lock_guard<std::mutex> lock(mutex);failed=true;latest.clear();}
#ifdef _WIN32
    WSACleanup();
#endif
  }
};
OpenRgbSink::OpenRgbSink(const std::string &assignment):impl_(new Impl(assignment)){}
OpenRgbSink::~OpenRgbSink()=default;
void OpenRgbSink::observe(const std::vector<uint8_t> &rgb){
  if(rgb.size()>300 || rgb.size()%3)throw std::invalid_argument("invalid native RGB frame");
  std::lock_guard<std::mutex> lock(impl_->mutex);
  if(impl_->failed || rgb==impl_->previous)return;
  impl_->previous=rgb;impl_->latest=rgb;impl_->dirty=true;impl_->wake.notify_one();
}
}
