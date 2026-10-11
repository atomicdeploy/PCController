#include "virtual_board/protocol.hpp"
#include <array>
#include <iostream>
#include <stdexcept>
#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>
#endif

int main(int argc,char **argv) {
#ifdef _WIN32
  WSADATA data{}; if(WSAStartup(MAKEWORD(2,2),&data)!=0) return 1;
  SOCKET socketValue=INVALID_SOCKET;
#else
  int socketValue=-1;
#endif
  int result=1;
  try {
    const std::string host=argc>1 ? argv[1] : "127.0.0.1";
    size_t used=0; const std::string portText=argc>2 ? argv[2] : "8765";
    const auto port=std::stoul(portText,&used); if(used!=portText.size() || !port || port>65535) throw std::invalid_argument("invalid port");
    socketValue=socket(AF_INET,SOCK_STREAM,IPPROTO_TCP);
    sockaddr_in address{}; address.sin_family=AF_INET; address.sin_port=htons(static_cast<uint16_t>(port));
    if(inet_pton(AF_INET,host.c_str(),&address.sin_addr)!=1 || connect(socketValue,reinterpret_cast<sockaddr *>(&address),sizeof(address))!=0) throw std::runtime_error("connect failed");
#ifdef _WIN32
    DWORD timeout=3000; setsockopt(socketValue,SOL_SOCKET,SO_RCVTIMEO,reinterpret_cast<const char *>(&timeout),sizeof(timeout));
#else
    timeval timeout{3,0}; setsockopt(socketValue,SOL_SOCKET,SO_RCVTIMEO,&timeout,sizeof(timeout));
#endif
    auto request=[&](uint8_t opcode,uint8_t sequence,const std::vector<uint8_t> &payload=std::vector<uint8_t>{}) {
      const auto bytes=pccontroller::wire::encode({opcode,sequence,payload}); size_t at=0;
      while(at<bytes.size()) { const auto n=send(socketValue,reinterpret_cast<const char *>(bytes.data()+at),static_cast<int>(bytes.size()-at),0); if(n<=0) throw std::runtime_error("send failed"); at+=static_cast<size_t>(n); }
    };
    pccontroller::wire::StreamDecoder decoder;
    request(pccontroller::wire::Hello,42);
    bool hello=false,strip=false;
    for(bool done=false;!done;) {
      std::array<uint8_t,512> buffer{};
      const auto count=recv(socketValue,reinterpret_cast<char *>(buffer.data()),static_cast<int>(buffer.size()),0);
      if(count<=0) throw std::runtime_error("protocol response timeout/disconnect");
      for(const auto &frame:decoder.feed(buffer.data(),static_cast<size_t>(count)).frames) {
        if(frame.opcode==pccontroller::wire::HelloResponse && frame.sequence==42) {
          if(!((frame.payload.size()==14 && frame.payload[0]==3) || (frame.payload.size()==16 && frame.payload[0]==4)) || frame.payload[1]!=1) throw std::runtime_error("HELLO ABI");
          hello=true; request(pccontroller::wire::AddressableLed,43,{0xFF,1,2,3,255});
        } else if(frame.opcode==pccontroller::wire::Ack && frame.sequence==43) {
          if(!hello || frame.payload.size()!=6 || frame.payload[1]!=0) throw std::runtime_error("timestamped strip ACK ABI");
          strip=true; request(pccontroller::wire::GetStatus,44);
        } else if(frame.opcode==pccontroller::wire::StatusResponse && frame.sequence==44) {
          if(!strip || frame.payload.size()!=48) throw std::runtime_error("STATUS ABI");
          done=true; result=0; std::cout << "production HELLO, timestamped strip ACK and 48-byte STATUS passed; reset/reconnect is tested by transport_integration.test.mjs\n";
        }
      }
    }
  } catch(const std::exception &e) { std::cerr << e.what() << '\n'; }
#ifdef _WIN32
  if(socketValue!=INVALID_SOCKET) closesocket(socketValue); WSACleanup();
#else
  if(socketValue>=0) close(socketValue);
#endif
  return result;
}
