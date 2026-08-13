#pragma once

#include "Project/Core/MacroRing.h"
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
  std::uint8_t flags = 0;
  std::uint8_t illuminationMode = 1;
  std::uint8_t illuminationOnBrightness = 128;
  std::uint8_t illuminationOffBrightness = 0;
  std::uint8_t displayBrightness = 5;
  std::uint8_t statusBrightness = 128;
  std::uint8_t outputPersistence = 0x06;
  std::uint16_t streamPeriodMs = 500;
  std::array<std::uint8_t, 8> userPwm{};
  std::uint8_t defaultMenuPage = 0;
  std::uint8_t menuFlags = 0;
  std::uint16_t visibleMenuMask = 0x3FFF;
  std::array<std::uint8_t, 7> menuOrder{
      {0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC}};
  std::uint8_t displayOptions = 0x10;
  std::uint8_t relayRestoreMask = 0;
  std::uint8_t motionBreakMs = 1;
  std::string boardName;
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
    std::uint8_t actionKind = 0;
    std::uint8_t actionValue = 0;
    std::uint8_t behavior = 0;
  };

  wire::Frame helloFrame(std::uint8_t sequence) const;
  wire::Frame statusFrame(std::uint8_t sequence, TimePoint now) const;
  wire::Frame settingsFrame(std::uint8_t sequence) const;
  wire::Frame pwmFrame(std::uint8_t sequence) const;
  wire::Frame temperaturesFrame(std::uint8_t sequence) const;
  wire::Frame frontPanelFrame(std::uint8_t sequence) const;
  wire::Frame menuListFrame(std::uint8_t sequence,
                            std::uint8_t cursor) const;
  wire::Frame i2cTransferFrame(
      std::uint8_t sequence, const std::vector<std::uint8_t> &request,
      TimePoint now);
  wire::Frame remotesFrame(std::uint8_t sequence,
                           std::uint8_t cursor) const;
  wire::Frame macroStatusFrame(std::uint8_t opcode,
                               std::uint8_t sequence);
  wire::Frame menuLayoutFrame(std::uint8_t sequence) const;
  wire::Frame ackFrame(std::uint8_t sequence, std::uint8_t opcode,
                       TimePoint now) const;
  wire::Frame errorFrame(std::uint8_t sequence, std::uint8_t opcode,
                         wire::Error error, TimePoint now) const;
  std::uint32_t deviceMicros(TimePoint now) const;
  // Dispatches a frame while the board mutex is already held. Macro playback
  // calls this same path so it inherits the ordinary peripheral safety rules.
  std::vector<wire::Frame> handleLocked(const wire::Frame &request,
                                        TimePoint now,
                                        bool hostRequest);

  bool applySettings(const std::vector<std::uint8_t> &payload);
  bool applyMenuLayout(const std::vector<std::uint8_t> &payload);
  bool applyDisplayText(const std::vector<std::uint8_t> &payload,
                        TimePoint now);
  bool validRemoteMapping(std::uint8_t kind, std::uint8_t value,
                          std::uint8_t behavior) const;
  void executeLearnedRemote(const RemoteEntry &remote, TimePoint now);
  void stopRemoteMomentary(TimePoint now);
  bool motionAllowed() const;
  void stopMotion();
  void applyStoredSettings();
  void restoreStoredOutputs();
  void storeUserPwmValue(std::uint8_t channel, std::uint16_t value);
  void captureRelayState();
  std::uint8_t learningRemainingSeconds(TimePoint now) const;
  void endLearning(std::uint8_t state);
  void releaseHostPanel();
  void loadSettings();
  void saveSettings();
  void resetSettings();
  void loadRemotes();
  void saveRemote(std::uint8_t id);
  void clearRemotes();
  void recordReset(std::uint8_t cause, bool emitEvent);
  void resetRuntime(TimePoint now);
  void setMenuPage(std::uint8_t page);
  void updateMenuDisplay();
  void cancelMacro(bool keepOutputs, bool emitEvent);
  // Console/RF adapters share the normal protocol dispatcher with macros.
  bool dispatchLocalCommand(std::uint8_t opcode,
                            const std::vector<std::uint8_t> &payload,
                            TimePoint now);
  void serviceMacro(TimePoint now, std::vector<wire::Frame> &output);
  void applyMacroSafeStop();
  void queueMacroEvent();
  void queueEvent(std::initializer_list<std::uint8_t> payload);
  void queueEvent(std::vector<std::uint8_t> payload);
  void queueMirrorChanges();
  bool applyStatusEffect(const std::vector<std::uint8_t> &payload,
                         TimePoint now);
  void renderStatusEffect();
  void finishStatusEffect();
  void serviceStatusEffect(TimePoint now);
  void setStatusRgb(std::uint8_t red, std::uint8_t green,
                    std::uint8_t blue, std::uint8_t brightness);
  bool statusProfile(std::uint8_t condition,
                     std::array<std::uint8_t, 12> &payload) const;
  bool setStatusProfile(std::uint8_t condition,
                        const std::uint8_t *payload, TimePoint now);
  void showScheduledSegmentWindow();
  void clearScheduledSegments(bool restoreMenu);
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
  TimePoint lastFadeAt_;
  TimePoint lastStatusEffectAt_;
  TimePoint lastRelayTestAt_;
  TimePoint lastHostActivityAt_;
  TimePoint learningDeadline_;
  TimePoint segmentDeadline_;
  TimePoint buzzerDeadline_;
  TimePoint i2cLeaseDeadline_;
  TimePoint lastRemoteActionAt_;
  TimePoint remoteMomentaryDeadline_;

  std::vector<wire::Frame> pendingEvents_;
  std::array<RemoteEntry, 20> remotes_{};
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
  bool learningActive_ = false;
  std::uint8_t learningMode_ = 0;
  std::uint8_t learningTotalSeconds_ = 0;
  std::uint8_t learningReportedRemaining_ = 0;
  std::uint32_t lastRadioCode_ = 0;
  std::uint32_t lastRemoteActionCode_ = 0;
  std::uint8_t remoteMomentaryKind_ = 0;
  std::uint8_t remoteMomentaryValue_ = 0;
  bool lastRemoteActionValid_ = false;
  bool hostSeen_ = false;
  bool programRunning_ = false;
  bool statusOverride_ = false;
  std::uint8_t statusEffect_ = 0;
  std::uint8_t statusCondition_ = 0;
  std::array<std::uint8_t, 3> statusEffectColor_{};
  std::array<std::uint8_t, 3> statusEffectAlternate_{};
  std::uint8_t statusEffectBrightness_ = 0;
  std::uint8_t statusEffectMinimum_ = 0;
  std::uint8_t statusEffectPhase_ = 0;
  std::uint8_t statusEffectRepeats_ = 0;
  std::uint16_t statusEffectStepMs_ = 20;
  bool hostPanelCaptured_ = false;
  std::uint16_t hostPanelMeta_ = 0;
  // Shared AVR/VirtualBoard macro queue and schema-2 status report.
  ControllerCore::MacroRing macroRing_{};
  TimePoint macroLastHostActivity_;
  std::uint8_t enclosureBrightness_ = 0;

  std::uint16_t menuVisibleMask_ = 0x3FFF;
  std::array<std::uint8_t, 14> menuOrder_{{0, 1, 2, 3, 4, 5, 6,
                                           7, 8, 9, 10, 11, 12, 13}};
  std::string scheduledSegmentText_;
  std::size_t scheduledSegmentIndex_ = 0;
  std::uint16_t scheduledSegmentStepMs_ = 260;
  std::uint16_t scheduledSegmentHoldMs_ = 5000;
  std::uint8_t scheduledSegmentOptions_ = 0;
  std::uint8_t scheduledSegmentIntervalSeconds_ = 30;
  bool scheduledSegmentActive_ = false;
  bool scheduledSegmentWaiting_ = false;
  bool segmentDeadlineActive_ = false;
  bool buzzerDeadlineActive_ = false;
  std::array<std::uint8_t, 4> lastPushedSegments_{{0, 0, 0, 0}};
  std::uint8_t lastPushedSegmentBrightness_ = 0;
  std::uint16_t lastPushedBuzzerFrequencyHz_ = 0;
  std::uint16_t lastPushedBuzzerDurationMs_ = 0;
  bool lastPushedBuzzerMuted_ = false;
  std::array<std::uint8_t, 6> lastPushedStatusLed_{{0, 0, 0, 0, 0, 0}};
  std::uint8_t i2cLeaseAddress_ = 0;
  std::array<std::uint8_t, 4> i2cRegisterPointers_{};
  std::array<std::array<std::uint8_t, 256>, 4> i2cRegisters_{};
};

} // namespace pccontroller::virtual_board
