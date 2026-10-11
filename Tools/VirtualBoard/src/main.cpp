#ifndef _WIN32
#define _POSIX_C_SOURCE 200809L
#endif
#include "virtual_board/tcp_server.hpp"
#include <atomic>
#include <chrono>
#include <csignal>
#include <cstdlib>
#include <iostream>
#include <fstream>
#include <memory>
#include <thread>
#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#include <process.h>
#else
#include <unistd.h>
#endif

namespace {
std::atomic<bool> *stopFlag=nullptr;
void signalStop(int) { if(stopFlag) stopFlag->store(true); }
}
int main(int argc,char **argv) {
  try {
    pccontroller::virtual_board::TcpServerOptions options;
    std::filesystem::path eeprom;
    std::filesystem::path pidFile;
    bool stdinEnabled=true,stdioMode=false,memory=false;
    for(int i=1;i<argc;++i) {
      std::string arg=argv[i];
      auto value=[&]() -> std::string { if(++i==argc) throw std::invalid_argument(arg+" requires a value"); return argv[i]; };
      if(arg=="--bind") options.bindAddress=value();
      else if(arg=="--port") { const auto v=value(); size_t used=0; const auto p=std::stoul(v,&used); if(used!=v.size() || p==0 || p>65535) throw std::invalid_argument("port must be 1..65535"); options.port=static_cast<uint16_t>(p); }
      else if(arg=="--eeprom") eeprom=value();
      else if(arg=="--pid-file") pidFile=value();
      else if(arg=="--memory") memory=true;
      else if(arg=="--no-stdin") stdinEnabled=false;
      else if(arg=="--quiet") options.quiet=true;
      else if(arg=="--log-pwm") options.logPwm=true;
      else if(arg=="--stdio") stdioMode=true;
      else if(arg=="--help" || arg=="-h") { std::cout << "PCController production firmware, OS-native target\nvirtual_board [--bind IPv4] [--port 8765] [--eeprom FILE | --memory] [--no-stdin] [--quiet] [--log-pwm] [--stdio] [--pid-file FILE]\nTCP mode: relay ON:n/OFF:n on stdout; opt-in PWM logging. Console inputs are explicit fixtures.\nStdio mode: binary protocol only on stdin/stdout, diagnostics on stderr.\n"; return 0; }
      else throw std::invalid_argument("unknown option: "+arg);
    }
    if(memory && !eeprom.empty()) throw std::invalid_argument("--memory and --eeprom are exclusive");
    if(!memory && eeprom.empty()) eeprom="native-firmware-eeprom-"+std::to_string(options.port)+".bin";
    auto board=std::make_shared<pccontroller::virtual_board::VirtualBoard>(eeprom);
    if(!pidFile.empty()) {
      std::ofstream pid(pidFile);
#ifdef _WIN32
      pid << _getpid();
#else
      pid << getpid();
#endif
      if(!pid.flush()) throw std::runtime_error("PID file write failed");
    }
    auto stop=std::make_shared<std::atomic<bool>>(false);
    auto eof=std::make_shared<std::atomic<bool>>(false);
    stopFlag=stop.get(); std::signal(SIGINT,signalStop); std::signal(SIGTERM,signalStop);
    if(stdioMode) {
#ifdef _WIN32
      _setmode(_fileno(stdin),_O_BINARY); _setmode(_fileno(stdout),_O_BINARY);
#endif
      // Input thread owns shared state until process exit; no dangling stack
      // captures if a signal closes the runtime while stdin is blocked.
      std::thread([board,stop,eof]() {
        char c;
        try { while(!stop->load() && std::cin.get(c)) { auto b=static_cast<uint8_t>(c); board->feed(&b,1); } }
        catch(const std::exception &e) { std::cerr << e.what() << '\n'; stop->store(true); }
        eof->store(true);
      }).detach();
      for(int finalPasses=0;!stop->load() && !board->resetRequested();) {
        const auto bytes=board->tick();
        if(!bytes.empty()) { std::cout.write(reinterpret_cast<const char *>(bytes.data()),bytes.size()); std::cout.flush(); }
        if(eof->load() && ++finalPasses>1100) break; // bounded 64 KiB RX at 63 bytes/pass
        std::this_thread::sleep_for(std::chrono::milliseconds(1));
      }
    } else {
      if(stdinEnabled) std::thread([board,stop]() {
        for(std::string line; !stop->load() && std::getline(std::cin,line);) {
          try { auto result=board->console(line); if(!result.message.empty()) std::cout << result.message << '\n'; if(result.stopRequested) stop->store(true); }
          catch(const std::exception &e) { std::cerr << "input: " << e.what() << '\n'; }
        }
      }).detach();
      pccontroller::virtual_board::runTcpServer(*board,options,*stop);
    }
    board->flush();
    stopFlag=nullptr;
    if(board->resetRequested()) {
      pccontroller::virtual_board::releaseNativeEeprom();
      // An OS process is the MCU memory/reset boundary, including function
      // statics. Never pretend calling setup() clears all firmware state.
#ifdef _WIN32
      _putenv_s("PCCONTROLLER_NATIVE_RESET","1");
      if(memory) _putenv_s("PCCONTROLLER_NATIVE_EEPROM",pccontroller::virtual_board::nativeEepromSnapshot().c_str());
      _execv(argv[0],argv);
#else
      setenv("PCCONTROLLER_NATIVE_RESET","1",1);
      if(memory) setenv("PCCONTROLLER_NATIVE_EEPROM",pccontroller::virtual_board::nativeEepromSnapshot().c_str(),1);
      execv(argv[0],argv);
#endif
      throw std::runtime_error("native reset restart failed");
    }
    return 0;
  } catch(const std::exception &e) { std::cerr << "virtual_board: " << e.what() << '\n'; return 1; }
}
