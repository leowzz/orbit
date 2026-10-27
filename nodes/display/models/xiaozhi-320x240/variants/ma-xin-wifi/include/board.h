#pragma once

#include <stdint.h>

namespace board {

// The panel is natively 240x320; MADCTL swaps the axes for a 320x240 view.
inline constexpr int kLcdWidth = 320;
inline constexpr int kLcdHeight = 240;
inline constexpr int kLcdSclk = 12;
inline constexpr int kLcdMosi = 13;
inline constexpr int kLcdCs = 11;
inline constexpr int kLcdDc = 10;
inline constexpr int kLcdReset = 14;
inline constexpr int kLcdBacklight = 18;

inline constexpr int kI2cSda = 7;
inline constexpr int kI2cScl = 6;
inline constexpr int kI2sMclk = 5;
inline constexpr int kI2sBclk = 4;
inline constexpr int kI2sWordSelect = 2;
inline constexpr int kI2sDataOut = 1;
inline constexpr int kI2sDataIn = 3;
inline constexpr int kAmplifierEnable = 17;
inline constexpr uint8_t kCodecAddress = 0x18;

inline constexpr int kButton = 0;
inline constexpr int kChargeStatus = 38;
inline constexpr int kBatteryAdc = 9;

}  // namespace board
