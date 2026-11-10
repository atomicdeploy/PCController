#pragma once
#include <cstdint>
#include <string>
namespace NativeHal {
// auto enables only the Raspberry Pi primary bus with an observed PCA9685.
// Explicit device paths also support USB adapters exported through i2c-dev.
void configureHostI2c(const std::string &device);
bool hostI2cEnabled();
uint8_t hostI2cBegin(uint8_t address);
bool hostI2cWrite(uint8_t value);
uint8_t hostI2cEnd();
uint8_t hostI2cRead(uint8_t address,uint8_t *values,uint8_t length);
}
