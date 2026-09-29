// Implementation fragment compiled once; owns 433 MHz learn/map/send flow.
// -----------------------------------------------------------------------------
// Learned 433 MHz remotes and RC-switch
// -----------------------------------------------------------------------------

// Mirrors direct user-MOSFET output changes into the EEPROM-backed last state.
void storeUserPwmValue(uint8_t channel, uint16_t value) {
  if (channel >= PwmChannels::UserLightCount) {
    return;
  }
  const uint8_t stored =
      static_cast<uint8_t>(value >= 4080 ? 255 : (value + 8) / 16);
  if (settingsStore.values().userPwm[channel] != stored) {
    settingsStore.values().userPwm[channel] = stored;
    settingsStore.markDirty(now);
  }
}

// Returns the timer countdown maintained by serviceLearningTimer(), or zero for
// indefinite mode. The service path advances at most one second per pass, so a
// delayed loop catches up without a 32-bit division or an event burst.
#if PCCONTROLLER_ENABLE_RF_LEARNING
uint8_t learningRemainingSeconds() {
  return learningTotalSeconds == 0 ? 0 : learningReportedRemaining;
}

__attribute__((noinline)) void reportLearning(uint8_t state) {
  appEvents.rfLearning(state, learnedRemotes.count(),
                       learningTotalSeconds == 0 ? RF_LEARN_INDEFINITE
                                                 : RF_LEARN_TIMER,
                       learningTotalSeconds, learningReportedRemaining);
}

// Starts the default indefinite/multi mode or the explicit bounded timer mode.
void beginLearning(uint8_t, uint8_t timeoutSeconds) {
  buzzer.stop();
  const ProgramMode currentMode = modeManager.current();
  if (currentMode <= MODE_RF) {
    modeBeforeLearning = currentMode;
  } else {
    modeBeforeLearning = MODE_RF;
  }
  learningActive = true;
  learningTotalSeconds = timeoutSeconds;
  learningReportedRemaining = timeoutSeconds;
  learningLastSecondAt = static_cast<uint16_t>(now);
  modeManager.transitionTo(MODE_RF_LEARNING);
  reportLearning(3);
}

// Ends learning, restores its prior page, emits state, and plays final feedback.
void endLearning(uint8_t state) {
  if (!learningActive) {
    return;
  }
  learningActive = false;
  if (modeManager.current() == MODE_RF_LEARNING) {
    modeManager.transitionTo(modeBeforeLearning);
  }
  reportLearning(state);
}

// Emits one MCU-timed timer update per changed second and closes at zero.
void serviceLearningTimer(uint32_t at) {
  if (!learningActive || learningTotalSeconds == 0) {
    return;
  }
  if (static_cast<uint16_t>(at - learningLastSecondAt) < 1000U) {
    return;
  }
  learningLastSecondAt = static_cast<uint16_t>(learningLastSecondAt + 1000U);
  --learningReportedRemaining;
  if (learningReportedRemaining == 0) {
    endLearning(0);
  } else {
    reportLearning(4);
  }
}
#else
uint8_t learningRemainingSeconds() { return 0; }
void beginLearning(uint8_t, uint8_t) {}
void endLearning(uint8_t) {}
void serviceLearningTimer(uint32_t) {}
#endif

// Deactivates the output held by the current RF momentary mapping.
void stopRemoteMomentary(uint32_t at) {
  switch (remoteMomentaryKind) {
    case RemoteActionKind::Relay:
      relays.requestRelayForTest(
          static_cast<uint8_t>(remoteMomentaryValue + 1), false, at);
      break;
    case RemoteActionKind::Side:
      relays.stopSide(static_cast<::RelaySide>(remoteMomentaryValue), at);
      break;
    case RemoteActionKind::Pwm:
      pwm.setChannel(remoteMomentaryValue, at);
      pwm.setValue(0, at);
      storeUserPwmValue(remoteMomentaryValue, 0);
      break;
    default:
      break;
  }
  remoteMomentaryKind = RemoteActionKind::None;
  remoteMomentaryEndsAt = 0;
}

// Expires momentary RF outputs locally if their repeat stream stops.
void serviceRemoteMomentary(uint32_t at) {
  if (remoteMomentaryKind != RemoteActionKind::None &&
      timeReached(at, remoteMomentaryEndsAt)) {
    stopRemoteMomentary(at);
  }
}

// Applies one persisted mapping through the same safe relay/PWM/menu APIs.
void executeLearnedRemote(const LearnedRemote &remote, uint32_t at) {
  const RemoteActionKind kind =
      static_cast<RemoteActionKind>(remote.actionKind);
  const RemoteBehavior behavior =
      static_cast<RemoteBehavior>(remote.behavior);
  if (remoteMomentaryKind != RemoteActionKind::None &&
      (remoteMomentaryKind != kind ||
       remoteMomentaryValue != remote.actionValue)) {
    stopRemoteMomentary(at);
  }
  switch (kind) {
    case RemoteActionKind::Key:
      // An accepted RF frame is the wireless Down edge: publish that same
      // immediate semantic before dispatching the binding in this service
      // pass. It must never masquerade as the later Click classification.
      appEvents.key(remote.actionValue,
                    static_cast<uint8_t>(KeyEvent::Down),
                    InputEventSource::Radio, remote.id);
      handleMenuAction(remote.actionValue, true);
      return;
    case RemoteActionKind::Menu:
      handleMenuAction(remote.actionValue, true);
      return;
    case RemoteActionKind::Relay: {
      const uint8_t mask = static_cast<uint8_t>(_BV(remote.actionValue));
      const bool active = (relays.activeRelayMask() & mask) != 0;
      const bool next = behavior == RemoteBehavior::Toggle ||
                                behavior == RemoteBehavior::Press
                            ? !active
                            : true;
      const bool accepted = relays.requestRelayForTest(
          static_cast<uint8_t>(remote.actionValue + 1), next, at);
      if (accepted && behavior == RemoteBehavior::Momentary) {
        remoteMomentaryKind = kind;
        remoteMomentaryValue = remote.actionValue;
        remoteMomentaryEndsAt = at + 350;
      }
      return;
    }
    case RemoteActionKind::Side:
      if (behavior != RemoteBehavior::Stop &&
          !relays.motionAllowed()) {
        return;
      }
      if (behavior == RemoteBehavior::Stop) {
        relays.stopSide(static_cast<::RelaySide>(remote.actionValue), at);
      } else {
        const RelayDirection direction =
            behavior == RemoteBehavior::Down ? RelayDirection::Reverse
                                               : RelayDirection::Forward;
        if (relays.requestSide(static_cast<::RelaySide>(remote.actionValue),
                               direction, true, at)) {
          remoteMomentaryKind = kind;
          remoteMomentaryValue = remote.actionValue;
          remoteMomentaryEndsAt = at + 350;
        }
      }
      return;
    case RemoteActionKind::Pwm: {
      pwm.setChannel(remote.actionValue, at);
      const bool active = pwm.logicalValue(remote.actionValue) != 0;
      const uint16_t value = behavior == RemoteBehavior::Momentary
                                 ? 4095
                                 : (active ? 0 : 4095);
      pwm.setValue(value, at);
      storeUserPwmValue(remote.actionValue, value);
      if (behavior == RemoteBehavior::Momentary) {
        remoteMomentaryKind = kind;
        remoteMomentaryValue = remote.actionValue;
        remoteMomentaryEndsAt = at + 350;
      }
      return;
    }
    case RemoteActionKind::None:
      return;
  }
}

// Consumes one RC-switch frame, emits it immediately, then learns or executes it.
void serviceRadio() {
  if (!radio.available()) {
    return;
  }

  const uint32_t code = radio.getReceivedValue();
  const uint8_t bits = radio.getReceivedBitlength();
  const uint8_t protocol = radio.getReceivedProtocol();
  const uint16_t pulseLength = radio.getReceivedDelay();
  radio.resetAvailable();

  if (code == 0 || bits == 0) {
    return;
  }

  radioState.lastCode = code;
  radioState.lastBitLength = bits;
  radioState.lastProtocol = protocol;
  radioState.lastPulseLength = pulseLength;

  const bool repeated =
      code == lastRemoteActionCode &&
      static_cast<uint32_t>(now - lastRemoteActionAt) < 400;
  lastRemoteActionCode = code;
  lastRemoteActionAt = now;

  LearnedRemote remote;
  bool learned;
#if PCCONTROLLER_ENABLE_RF_LEARNING
  const bool wasLearning = learningActive;
  if (wasLearning) {
    if (repeated) {
      return;
    }
    remote.id = 0;
    learned =
        learnedRemotes.learn(code, bits, protocol, pulseLength, remote.id);
    if (learned) {
      appEvents.rfLearned(remote.id);
    }
  } else
#endif
  {
    learned = learnedRemotes.find(code, bits, protocol, remote);
  }

  appEvents.rfReceived(code, bits, protocol, pulseLength,
                       learned ? remote.id : 0xFF);
  statusLeds.playCue(StatusLedCue::Radio,
#if PCCONTROLLER_ENABLE_RF_LEARNING
                     wasLearning ? 320 : 240,
#else
                     240,
#endif
                     now);
#if PCCONTROLLER_ENABLE_RF_LEARNING
  if (wasLearning) {
    if (!learned) {
      endLearning(2);
    } else if (learnedRemotes.count() >= RemoteLearningStore::Capacity) {
      endLearning(2);
    }
    return;
  }
#endif
  if (learned) {
#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
    automationExecutor.dispatch(AutomationEventKind::LearnedRf, remote.id,
                                now);
#endif
    const RemoteBehavior behavior =
        static_cast<RemoteBehavior>(remote.behavior);
    const bool refreshable =
        behavior == RemoteBehavior::Momentary ||
        behavior == RemoteBehavior::Up ||
        behavior == RemoteBehavior::Down;
    if (refreshable || !repeated) {
      executeLearnedRemote(remote, now);
    }
  }
}

// Temporarily releases INT0 receive timing while INT1 transmits one RF frame.
bool transmitRadio(uint32_t code, uint8_t bits, uint8_t protocol,
                   uint16_t pulseLength) {
  if (learningActive || code == 0 || bits == 0 || bits > 32 ||
      protocol == 0 || protocol > MAX_RC_PROTOCOL) {
    return false;
  }

  radio.disableReceive();
  radio.setProtocol(protocol);
  if (pulseLength != 0) {
    radio.setPulseLength(pulseLength);
  }
  radio.send(code, bits);
  radio.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));
  return true;
}

#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
// Executes a persisted action exclusively through existing safety-owning
// controller APIs. Silent mode remains owned by TonePlayer and motion starts
// remain owned by RelayController; this engine can stop but never start motion.
bool executeAutomationAction(const AutomationRecord &record, void *) {
  bool accepted = false;
  switch (static_cast<AutomationActionKind>(record.actionKind)) {
    case AutomationActionKind::SafeStop:
      safeStopMacroOutputs();
      accepted = true;
      break;
    case AutomationActionKind::MotionStop:
      if (record.actionTarget == 0xFF || record.actionTarget == 0) {
        relays.stopSide(::RelaySide::A, now);
      }
      if (record.actionTarget == 0xFF || record.actionTarget == 1) {
        relays.stopSide(::RelaySide::B, now);
      }
      accepted = true;
      break;
    case AutomationActionKind::Relay: {
      const uint8_t bit = static_cast<uint8_t>(_BV(record.actionTarget));
      const bool current = (relays.activeRelayMask() & bit) != 0;
      const bool requested = record.value == 2 ? !current : record.value != 0;
      accepted = relays.requestRelayForTest(
          static_cast<uint8_t>(record.actionTarget + 1), requested, now);
      break;
    }
    case AutomationActionKind::Pwm:
      accepted = pwm.setLogical(record.actionTarget, record.value);
      if (accepted) {
        storeUserPwmValue(record.actionTarget, record.value);
      }
      break;
    case AutomationActionKind::StatusCue:
      statusLeds.playCue(static_cast<StatusLedCue>(record.actionTarget),
                         record.value, now);
      accepted = true;
      break;
    case AutomationActionKind::Buzzer:
      accepted = buzzer.enqueue(record.value, record.extra);
      break;
    case AutomationActionKind::RfTransmit: {
      LearnedRemote remote;
      accepted = learnedRemotes.get(record.actionTarget, remote) &&
                 transmitRadio(remote.code, remote.bits, remote.protocol,
                               remote.pulseMicros);
      break;
    }
    case AutomationActionKind::HostMacroRequest:
      appEvents.automation(ControllerAutomationState::HostMacroRequested,
                           record.id, record.actionKind,
                           record.actionTarget);
      return true;
  }
  appEvents.automation(accepted ? ControllerAutomationState::Executed
                                : ControllerAutomationState::Rejected,
                       record.id, record.actionKind, record.actionTarget);
  return accepted;
}
#endif
