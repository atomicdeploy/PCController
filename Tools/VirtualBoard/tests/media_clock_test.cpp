#include "../../../Project/Core/MediaClock.h"
#include <stdexcept>
#include <iostream>
void check(bool v) { if (!v) throw std::runtime_error("media clock assertion failed"); }
int main() {
  MediaClock clock;
  uint8_t p[14] = {3,3,0xe8,3,0,0,0,1,0xb8,0x0b,0x3f,0xbf,0x6d,0x7d};
  check(clock.update(p,14,100)); check(clock.segments()[2]==0x6d);
  check(clock.active(3099)); check(!clock.active(3100));
  p[1]=1; check(clock.update(p,14,100)); check(clock.segments()[2]==0x6d);
  p[1]=3;p[6]=0;p[7]=2;check(clock.update(p,14,100));check(clock.segments()[2]==0x6d);
  check(clock.update(p,14,0xffffff00U));check(clock.active(0x100U));check(!clock.active(0x1000U));
  p[1]=1;p[2]=0x10;p[3]=0xdd;p[4]=0;p[5]=0;
  check(clock.update(p,14,0));uint8_t s[4];clock.segments(0,s);
  check(s[0]==0x3f && s[1]==(0x3f|0x80) && s[2]==0x6d && s[3]==0x7d);
  p[0]=1;check(!clock.update(p,14,0));p[0]=3;p[1]=8;check(!clock.update(p,14,0));
  std::cout << "Media clock: sample retention, expiry, wrap and segments passed\n";
}
