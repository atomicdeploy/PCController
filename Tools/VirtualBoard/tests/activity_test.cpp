#include "virtual_board/activity.hpp"
#include <Arduino.h>
#include <algorithm>
#include <iostream>
#include <stdexcept>
using namespace pccontroller::virtual_board;
void require(bool value,const char *message){if(!value)throw std::runtime_error(message);}
int main(){try{
  OutputSnapshot before,after;
  after.relayMask=0x10;after.pwm[0]=4095;after.pwm[15]=255;
  auto lines=activityLines(before,after,true,false);
  require(lines==std::vector<std::string>({"R5:ON","P1:4095","P16:255"}),"channel format/range/one-based identity");
  require(activityLines(after,after,true,true).empty(),"unchanged values must not emit transitions");
  require(activityLines(before,after,false,false)==std::vector<std::string>({"R5:ON"}),"continuous values are explicit opt-in");
  require(activityLines(before,after,true,false,true)==std::vector<std::string>({"ON:5","PWM1:4095","PWM16:255"}),"explicit legacy compatibility");
  require(activityLines(after,before,false,false)==std::vector<std::string>({"R5:OFF"}),"OFF transition");
  after.buzzerHz=440;after.segments={1,2,3,4};after.strip={255,0,128};
  lines=activityLines(before,after,false,true);
  for(const auto &expected:{"BZ:440","SEG:01020304","STRIP:ff0080"})require(std::find(lines.begin(),lines.end(),expected)!=lines.end(),"peripheral observation missing");
  VirtualBoard board({},true);
  require(board.console("I1:ON").message=="fixture input applied","typed input alias");
  require((NativeHal::shiftInput&1)==0,"input must use active-low production shift register");
  board.console("input 1 off");require((NativeHal::shiftInput&1)!=0,"input release");
  const auto previous=NativeHal::shiftInput;
  board.console("input 1 on trailing");require(NativeHal::shiftInput==previous,"trailing input partially applied");
  board.console("GPIO42:0");require(digitalRead(42)==0,"GPIO observation not visible to input HAL");
  board.console("gpio 42 none");require(digitalRead(42)==1,"GPIO release must restore default pull-up");
  board.console("gpio 64 0");require(NativeHal::inputLevels[42]==-1,"out-of-range GPIO changed input");
  board.console("T2:2500");require(NativeHal::temperature[1]==2500 && NativeHal::temperatureConfigured[0] && NativeHal::temperatureConfigured[1],"sensor slot identity must not slide");
  board.console("power 12000 -250");require(NativeHal::powerAvailable && NativeHal::supplyMilliVolts==12000 && NativeHal::currentMilliAmps==-250 && NativeHal::powerMilliWatts==-3000,"power units/sign/derivation");
  board.console("power none extra");require(NativeHal::powerAvailable,"invalid power input partially applied");
  board.console("power none");require(!NativeHal::powerAvailable,"missing power must remain unavailable");
  const auto outputs=board.outputSnapshot().relayMask;
  board.console("R5:ON");require(board.outputSnapshot().relayMask==outputs,"observation text bypassed output dispatcher");
  require(board.console(std::string(257,'x')).message=="input exceeds 256 bytes","bounded stdin contract");
  board.bindHostInput(1);board.observeHostInput(1,true);
  board.console("input 1 off");require((NativeHal::shiftInput&1)==0,"stdin stole assigned GPIO ownership");
  board.console("raw 255");require((NativeHal::shiftInput&1)==0,"raw fixture stole assigned GPIO ownership");
  board.observeHostInput(1,false);require((NativeHal::shiftInput&1)!=0,"assigned GPIO release failed");
  std::cout<<"activity formatting, logical range, strict bounded input, GPIO, stable sensor slots and power observations passed\n";
}catch(const std::exception &e){std::cerr<<e.what()<<'\n';return 1;}}
