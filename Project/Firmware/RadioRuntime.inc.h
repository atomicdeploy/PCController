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

// Returns ceil(remaining milliseconds / 1000), or zero for indefinite mode.
uint8_t learningRemainingSeconds(uint32_t at) {
  if (learningMode != RF_LEARN_TIMER || learningEndsAt == 0 ||
      timeReached(at, learningEndsAt)) {
    return 0;
  }
  return static_cast<uint8_t>((learningEndsAt - at + 999UL) / 1000UL);
}

// Starts the default indefinite/multi mode or the explicit bounded timer mode.
void beginLearning(uint8_t mode, uint8_t timeoutSeconds) {
  if (mode == RF_LEARN_TIMER) {
    if (timeoutSeconds == 0) {
      timeoutSeconds = DEFAULT_LEARNING_SECONDS;
    } else if (timeoutSeconds > MAX_LEARNING_SECONDS) {
      timeoutSeconds = MAX_LEARNING_SECONDS;
    }
  } else {
    mode = RF_LEARN_INDEFINITE;
    timeoutSeconds = 0;
  }

  buzzer.stop();
  const ProgramMode currentMode = modeManager.current();
  if (currentMode <= MODE_RF) {
    modeBeforeLearning = currentMode;
  } else {
    modeBeforeLearning = MODE_RF;
  }
  learningActive = true;
  learningMode = mode;
  learningTotalSeconds = timeoutSeconds;
  learningReportedRemaining = timeoutSeconds;
  learningEndsAt = mode == RF_LEARN_TIMER
                       ? now + static_cast<uint32_t>(timeoutSeconds) * 1000UL
                       : 0;
  modeManager.transitionTo(MODE_RF_LEARNING);
  appEvents.rfLearning(3, learnedRemotes.count(), learningMode,
                       learningTotalSeconds, learningReportedRemaining);
}

// Ends learning, restores its prior page, emits state, and plays final feedback.
void endLearning(uint8_t state, int8_t feedback) {
  if (!learningActive) {
    return;
  }
  const uint8_t remaining = learningRemainingSeconds(now);
  learningActive = false;
  learningEndsAt = 0;
  if (modeManager.current() == MODE_RF_LEARNING) {
    modeManager.transitionTo(modeBeforeLearning);
  }
  appEvents.rfLearning(state, learnedRemotes.count(), learningMode,
                       learningTotalSeconds, remaining);
  if (feedback > 0) {
    buzzer.success();
  } else if (feedback < 0) {
    buzzer.error();
  }
}

// Emits one MCU-timed timer update per changed second and closes at zero.
void serviceLearningTimer(uint32_t at) {
  if (!learningActive || learningMode != RF_LEARN_TIMER) {
    return;
  }
  const uint8_t remaining = learningRemainingSeconds(at);
  if (remaining == 0) {
    endLearning(0, 1);
  } else if (remaining != learningReportedRemaining) {
    learningReportedRemaining = remaining;
    appEvents.rfLearning(4, learnedRemotes.count(), learningMode,
                         learningTotalSeconds, remaining);
  }
}

// Deactivates the output held by the current RF momentary mapping.
void stopRemoteMomentary(uint32_t at) {
  switch (remoteMomentaryKind) {
    case RemoteActionKind::Relay: {
      const bool stopped = relays.requestRelayForTest(
          static_cast<uint8_t>(remoteMomentaryValue + 1), false, at);
      if (stopped) {
        const uint8_t payload[] = {remoteMomentaryValue, 0};
        acceptedAction(InputEventSource::Radio,
                       ControllerProtocol::RelaySet, payload, sizeof(payload));
      }
      break;
    }
    case RemoteActionKind::Side: {
      relays.stopSide(static_cast<::RelaySide>(remoteMomentaryValue), at);
      const uint8_t payload[] = {remoteMomentaryValue, 0};
      acceptedAction(InputEventSource::Radio,
                     ControllerProtocol::RelaySide, payload, sizeof(payload));
      break;
    }
    case RemoteActionKind::Pwm: {
#if PCCONTROLLER_ENABLE_PCA9685
      pwm.setChannel(remoteMomentaryValue, at);
      pwm.setValue(0, at);
      storeUserPwmValue(remoteMomentaryValue, 0);
      if (pwm.available()) {
        const uint8_t payload[] = {remoteMomentaryValue, 0, 0};
        acceptedAction(InputEventSource::Radio, ControllerProtocol::PwmSet,
                       payload, sizeof(payload));
      }
#endif
      break;
    }
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
      applyKeyGesture(remote.actionValue, KeyEvent::Down,
                      InputEventSource::Radio, true);
      return;
    case RemoteActionKind::Menu:
      if (modeManager.current() == MODE_MOTION_CONTROL) {
        applyKeyGesture(remote.actionValue, KeyEvent::Down,
                        InputEventSource::Radio, true);
        return;
      }
      handleMenuAction(remote.actionValue, true);
      {
        const uint8_t payload[] = {remote.actionValue};
        acceptedAction(InputEventSource::Radio,
                       ControllerProtocol::MenuAction, payload,
                       sizeof(payload));
      }
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
      if (accepted) {
        const uint8_t payload[] = {
            remote.actionValue, static_cast<uint8_t>(next)};
        acceptedAction(InputEventSource::Radio,
                       ControllerProtocol::RelaySet, payload,
                       sizeof(payload));
      }
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
        const uint8_t payload[] = {remote.actionValue, 0};
        acceptedAction(InputEventSource::Radio,
                       ControllerProtocol::RelaySide, payload,
                       sizeof(payload));
      } else {
        const RelayDirection direction =
            behavior == RemoteBehavior::Down ? RelayDirection::Reverse
                                               : RelayDirection::Forward;
        if (relays.requestSide(static_cast<::RelaySide>(remote.actionValue),
                               direction, true, at)) {
          const uint8_t payload[] = {
              remote.actionValue,
              static_cast<uint8_t>(direction == RelayDirection::Forward ? 1
                                                                         : 2)};
          acceptedAction(InputEventSource::Radio,
                         ControllerProtocol::RelaySide, payload,
                         sizeof(payload));
          remoteMomentaryKind = kind;
          remoteMomentaryValue = remote.actionValue;
          remoteMomentaryEndsAt = at + 350;
        }
      }
      return;
    case RemoteActionKind::Pwm: {
#if PCCONTROLLER_ENABLE_PCA9685
      pwm.setChannel(remote.actionValue, at);
      const bool active = pwm.logicalValue(remote.actionValue) != 0;
      const uint16_t value = behavior == RemoteBehavior::Momentary
                                 ? 4095
                                 : (active ? 0 : 4095);
      pwm.setValue(value, at);
      storeUserPwmValue(remote.actionValue, value);
      if (pwm.available()) {
        const uint8_t payload[] = {
            remote.actionValue, static_cast<uint8_t>(value),
            static_cast<uint8_t>(value >> 8)};
        acceptedAction(InputEventSource::Radio, ControllerProtocol::PwmSet,
                       payload, sizeof(payload));
      }
      if (behavior == RemoteBehavior::Momentary) {
        remoteMomentaryKind = kind;
        remoteMomentaryValue = remote.actionValue;
        remoteMomentaryEndsAt = at + 350;
      }
#endif
      return;
    }
    case RemoteActionKind::None:
      return;
  }
}

// Consumes one RC-switch frame, emits it immediately, then learns or executes it.
void serviceRadio() {
  if (!radioReceiver.available()) {
    return;
  }

  const uint32_t code = radioReceiver.getReceivedValue();
  const uint8_t bits = radioReceiver.getReceivedBitlength();
  const uint8_t protocol = radioReceiver.getReceivedProtocol();
  const uint16_t pulseLength = radioReceiver.getReceivedDelay();
  radioReceiver.resetAvailable();

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

  if (learningActive) {
    if (repeated) {
      return;
    }
    uint8_t learnedId = 0;
    const bool learned =
        learnedRemotes.learn(code, bits, protocol, pulseLength, learnedId);
    if (learned) {
      appEvents.rfLearned(learnedId);
    }
    appEvents.rfReceived(code, bits, protocol, pulseLength,
                         learned ? learnedId : 0xFF);
    statusLeds.playCue(StatusLedCue::Radio, 320, now);
    if (!learned) {
      endLearning(2, -1);
    } else if (learnedRemotes.count() >= RemoteLearningStore::Capacity) {
      endLearning(2, 1);
    }
    return;
  }

  LearnedRemote remote;
  const bool learned = learnedRemotes.find(code, bits, protocol, remote);
  appEvents.rfReceived(code, bits, protocol, pulseLength,
                       learned ? remote.id : 0xFF);
  statusLeds.playCue(StatusLedCue::Radio, 240, now);
  if (learned) {
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

  radioReceiver.disableReceive();
  radioTransmitter.setProtocol(protocol);
  if (pulseLength != 0) {
    radioTransmitter.setPulseLength(pulseLength);
  }
  radioTransmitter.send(code, bits);
  radioReceiver.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));
  return true;
}
