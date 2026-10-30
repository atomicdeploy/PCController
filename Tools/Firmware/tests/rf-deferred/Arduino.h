#pragma once
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <cassert>
#define PROGMEM
#define HIGH 1
#define LOW 0
#define OUTPUT 1
#define CHANGE 1
extern unsigned long fakeMicros;
extern bool inInterrupt;
extern void (*edgeHandler)();
inline unsigned long micros() { return fakeMicros; }
inline void pinMode(int,int) {}
inline void digitalWrite(int,int) {}
inline void delayMicroseconds(unsigned int) {}
inline void attachInterrupt(int,void (*handler)(),int) { edgeHandler=handler; }
inline void detachInterrupt(int) { edgeHandler=nullptr; }
inline void* memcpy_P(void* destination,const void* source,size_t count) {
    assert(!inInterrupt && "RF protocol decoding must never run in the ISR");
    return std::memcpy(destination,source,count);
}
