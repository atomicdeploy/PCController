#pragma once

// Small deterministic `mode`/`lastMode` state manager. Transitions are
// requested first and consumed by
// programService(), keeping entry logic separate from steady-state service.
template <typename Mode>
class ModeManager {
public:
  explicit ModeManager(Mode initial)
      : current_(initial), last_(initial), previous_(initial), pending_(true) {}

  void transitionTo(Mode next) {
    if (next == current_ && !pending_) {
      return;
    }
    current_ = next;
    pending_ = true;
  }

  bool consumeTransition(Mode &from, Mode &to) {
    if (!pending_) {
      return false;
    }
    from = last_;
    to = current_;
    previous_ = last_;
    last_ = current_;
    pending_ = false;
    return true;
  }

  Mode current() const { return current_; }
  Mode previous() const { return previous_; }
  bool transitionPending() const { return pending_; }

private:
  Mode current_;
  Mode last_;
  Mode previous_;
  bool pending_;
};
