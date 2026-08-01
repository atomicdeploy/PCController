#pragma once

#include "virtual_board/hardware.hpp"
#include "virtual_board/protocol.hpp"

#include <array>
#include <chrono>
#include <cstddef>
#include <cstdint>
#include <mutex>
#include <string>
#include <vector>

namespace pccontroller::virtual_board {

struct Settings {
  std::uint8_t flags = 0x02;
  std::uint8_t illuminationMode = 1;
  std::uint8_t illuminationOnBrightness = 128;
  std::uint8_t illuminationOffBrightness = 0;
  std::uint8_t displayBrightness = 5;
  std::uint8_t statusBrightness = 128;
  std::uint8_t pwmBootMode = 2;
  std::uint16_t streamPeriodMs = 500;
  std::uint8_t defaultMenuPage = 0;
  std::uint8_t menuFlags = 0;
};

struct ConsoleResult {
  std::string message;
  bool stopRequested = false;
};

class VirtualBoard {
public:
  VirtualBoard(ISensors &sensors, IRelays &relays, IPwm &pwm,
               IAddressableLeds &addressableLeds, IDisplays &displays,
               IEeprom &eeprom);

  std::vector<wire::Frame> connectedFrames();
  std::vector<wire::Frame> handle(const wire::Frame &request);
  std::vector<wire::Frame> tick();
  ConsoleResult console(const std::string &line);
  std::string describe() const;
  void noteProtocolErrors(std::size_t framing, std::size_t crc);

private:
  using Clock = std::chrono::steady_clock;
  using TimePoint = Clock::time_point;

  struct RemoteEntry {
    bool used = false;
    std::uint8_t id = 0;
    std::uint32_t code = 0;
    std::uint8_t bits = 24;
    std::uint8_t protocol = 1;
    std::uint16_t pulseUs = 350;
    std::uint8_t actionKind = 1;
    std::uint8_t actionValue = 1;
    std::uint8_t behavior = 0;
  };

  struct HostMenuEntry {
    std::uint8_t id = 0xFF;
    std::uint8_t parent = 0xFF;
    std::uint8_t flags = 0;
  };

  wire::Frame helloFrame(std::uint8_t sequence) const;
  wire::Frame statusFrame(std::uint8_t sequence, TimePoint now) const;
  wire::Frame settingsFrame(std::uint8_t sequence) const;
  wire::Frame pwmFrame(std::uint8_t sequence) const;
  wire::Frame temperaturesFrame(std::uint8_t sequence) const;
  wire::Frame i2cFrame(std::uint8_t sequence) const;
  wire::Frame remotesFrame(std::uint8_t sequence,
                           std::uint8_t cursor) const;
  wire::Frame macroStatusFrame(std::uint8_t opcode,
                               std::uint8_t sequence) const;
  wire::Frame menuLayoutFrame(std::uint8_t sequence) const;
  wire::Frame hostMenuStateFrame(std::uint8_t sequence) const;
  wire::Frame ackFrame(std::uint8_t sequence, std::uint8_t opcode,
                       TimePoint now) const;
  wire::Frame errorFrame(std::uint8_t sequence, std::uint8_t opcode,
                         wire::Error error, TimePoint now) const;
  std::uint32_t deviceMicros(TimePoint now) const;

  bool applySettings(const std::vector<std::uint8_t> &payload);
  bool applyMenuLayout(const std::vector<std::uint8_t> &payload);
  bool applyHostMenuDirectory(const std::vector<std::uint8_t> &payload);
  bool applyHostMenuContent(const std::vector<std::uint8_t> &payload);
  void loadSettings();
  void saveSettings();
  void resetSettings();
  void recordReset(std::uint8_t cause, bool emitEvent);
  void resetRuntime(TimePoint now);
  void setMenuPage(std::uint8_t page);
  void updateMenuDisplay();
  void cancelMacro(bool keepOutputs, bool emitEvent);
  bool executeQueuedCommand(std::uint8_t opcode,
                            const std::vector<std::uint8_t> &payload,
                            TimePoint now);
  void serviceMacro(TimePoint now, std::vector<wire::Frame> &output);
  bool macroRecordReady() const;
  void queueMacroEvent();
  void queueEvent(std::initializer_list<std::uint8_t> payload);
  void queueEvent(std::vector<std::uint8_t> payload);
  void requestHostMenuContent(std::uint8_t id, std::uint8_t reason,
                              TimePoint now);
  void serviceAutomation(TimePoint now);
  std::string describeLocked() const;

  ISensors &sensors_;
  IRelays &relays_;
  IPwm &pwm_;
  IAddressableLeds &addressableLeds_;
  IDisplays &displays_;
  IEeprom &eeprom_;

  mutable std::mutex mutex_;
  Settings settings_;
  TimePoint startedAt_;
  TimePoint lastStreamAt_;
  TimePoint lastPwmStepAt_;
  TimePoint lastFadeAt_;
  TimePoint lastRelayTestAt_;
  TimePoint learningDeadline_;
  TimePoint segmentDeadline_;
  TimePoint lcdDeadline_;

  std::vector<wire::Frame> pendingEvents_;
  std::array<RemoteEntry, 8> remotes_{};
  std::uint16_t framingErrors_ = 0;
  std::uint16_t crcErrors_ = 0;
  std::uint32_t resetCount_ = 0;
  std::uint8_t menuPage_ = 0;
  std::uint8_t resetCause_ = 0;
  std::uint8_t activeKeys_ = 0;
  std::uint8_t pwmErrors_ = 0;
  std::uint8_t relayTestIndex_ = 0;
  std::uint16_t relayTestPeriodMs_ = 0;
  bool relayTestOn_ = false;
  bool pwmRising_ = true;
  bool learningActive_ = false;
  std::uint8_t macroState_ = 0;
  std::uint8_t macroId_ = 0;
  std::uint8_t macroOptions_ = 0;
  std::uint16_t macroTotalSteps_ = 0;
  std::uint16_t macroAcceptedSteps_ = 0;
  std::uint16_t macroExecutedSteps_ = 0;
  std::uint16_t macroAcceptedBytes_ = 0;
  std::uint8_t macroUnderruns_ = 0;
  std::uint8_t macroDispatchErrors_ = 0;
  std::uint32_t macroStartedAtUs_ = 0;
  TimePoint macroStartedAt_;
  TimePoint macroLastHostActivity_;
  std::vector<std::uint8_t> macroQueue_;
  std::uint8_t enclosureBrightness_ = 0;

  std::uint16_t menuVisibleMask_ = 0x7FFF;
  std::array<std::uint8_t, 15> menuOrder_{{0, 1, 2, 3, 4, 5, 6, 7,
                                           8, 9, 10, 11, 12, 13, 14}};
  std::array<HostMenuEntry, 8> hostMenuDirectory_{};
  std::uint8_t hostMenuCount_ = 0;
  std::uint8_t hostMenuGeneration_ = 0;
  std::uint8_t hostMenuActiveId_ = 0xFF;
  std::uint8_t hostMenuPhase_ = 0;
  std::uint8_t hostMenuAttempt_ = 0;
  std::uint8_t hostMenuRevision_ = 0;
  TimePoint hostMenuRequestedAt_;
  bool hostMenuRequestActive_ = false;
  bool segmentDeadlineActive_ = false;
  bool lcdDeadlineActive_ = false;
};

} // namespace pccontroller::virtual_board
