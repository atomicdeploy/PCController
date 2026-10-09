export type HoldActionOperation = () => void | Promise<unknown>

// HoldActionSession serializes start/stop so a release that races an async
// start still sends its stop afterwards. Every gesture termination can call
// release safely; duplicate releases never duplicate the command.
export class HoldActionSession {
  private active = false
  private stopIssued = false
  private startPromise: Promise<unknown> = Promise.resolve()

  constructor(
    private readonly start: HoldActionOperation,
    private readonly stop: HoldActionOperation,
    private readonly onError: (error: unknown) => void,
    private readonly onHoldingChange: (holding: boolean) => void,
  ) {}

  begin(): boolean {
    if (this.active) return false
    this.active = true
    this.stopIssued = false
    this.onHoldingChange(true)
    const startPromise = Promise.resolve().then(this.start)
    this.startPromise = startPromise
    void startPromise.catch((error) => {
      this.onError(error)
      this.release()
    })
    return true
  }

  release(update = true): boolean {
    if (!this.active || this.stopIssued) return false
    this.active = false
    this.stopIssued = true
    if (update) this.onHoldingChange(false)
    void this.startPromise
      .catch(() => undefined)
      .then(this.stop)
      .catch(this.onError)
    return true
  }

  isHolding(): boolean {
    return this.active
  }
}
