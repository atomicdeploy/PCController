#include "Arduino.h"
#include "../../../../LocalLib/RCSwitch.h"
#include <iostream>
unsigned long fakeMicros=0;
bool inInterrupt=false;
void (*edgeHandler)()=nullptr;
void edge(unsigned int duration) {
    fakeMicros+=duration; assert(edgeHandler); inInterrupt=true;
    edgeHandler(); inInterrupt=false;
}
void packet(unsigned long code, int bits=8) {
    for(int bit=bits-1;bit>=0;--bit) {
        edge((code&(1UL<<bit))?1050:350);
        edge((code&(1UL<<bit))?350:1050);
    }
    edge(350); edge(10850);
}
void frame(unsigned int code) { edge(10850); packet(code); }
int main() {
    RCSwitch radio; radio.enableReceive(0);
    frame(0x55);
    assert(radio.getReceivedValue()==0); // capture alone cannot decode
    for(int i=0;i<10;++i) edge(200); // pending frame must remain intact
    assert(radio.available());
    assert(radio.getReceivedValue()==0x55 && radio.getReceivedBitlength()==8);
    assert(radio.getReceivedProtocol()==1 && radio.getReceivedDelay()==350);
    radio.resetAvailable(); assert(!radio.available());
    frame(0xA6); packet(0xA6); assert(radio.available()); assert(radio.getReceivedValue()==0xA6);
    radio.resetAvailable();
    frame(0x33); radio.disableReceive(); radio.enableReceive(0);
    assert(!radio.available()); // rearming drops obsolete pending captures
    edge(10850); packet(0xA65A1234UL,32); packet(0xA65A1234UL,32);
    assert(radio.available()); // longest supported identity fits byte indices
    assert(radio.getReceivedValue()==0xA65A1234UL);
    assert(radio.getReceivedBitlength()==32);
    radio.resetAvailable();
    for(int i=0;i<150;++i) edge(200); // overflow must discard, not wrap indices
    assert(!radio.available());
    frame(0x5A); packet(0x5A);
    assert(radio.available() && radio.getReceivedValue()==0x5A);
    std::cout << "RF deferred decoding: ISR ownership, pending buffer, decode and rearm passed\n";
}
