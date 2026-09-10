/** Separates reader movement from our own bottom/anchor corrections. Streaming
 * and layout changes must never turn following back on by themselves. */
export class ThreadScrollIntent {
  following = true;
  revision = 0;
  top = 0;
  inputDirection = 0;
  /** @type {number | null} */
  expected = null;
  /** @param {number} top @param {boolean} [following] */
  reset(top, following = true) {
    this.top = top;
    this.following = following;
    this.expected = null;
    this.inputDirection = 0;
    this.revision++;
  }
  /** Called before wheel/touch/key scrolling, so a queued render cannot win.
   * @param {number} direction @param {number} top @param {number} maximum */
  input(direction, top, maximum) {
    this.revision++;
    this.inputDirection = Math.sign(direction);
    if (direction < 0) this.following = false;
    else if (direction > 0 && maximum - top <= 1) this.following = true;
  }
  /** An explicit End key means follow the live end, even if it moves while the
   * browser is still animating its keyboard scroll. */
  toBottom() {
    this.inputDirection = 1;
    this.following = true;
    this.revision++;
  }
  /** Scrollbar dragging starts a new gesture whose direction comes from its
   * actual offsets, instead of an earlier wheel or keyboard gesture. */
  drag() {
    this.inputDirection = 0;
    this.revision++;
  }
  /** @param {number} top */
  wrote(top) {
    this.expected = top;
    this.top = top;
  }
  /** Returns true when the reader moved, invalidating captured render anchors.
   * @param {number} top @param {number} maximum */
  observe(top, maximum) {
    if (this.expected !== null && Math.abs(top - this.expected) < 0.5) {
      this.expected = null;
      this.top = top;
      return false;
    }
    this.expected = null;
    if (Math.abs(top - this.top) < 0.5) return false;
    const direction = top - this.top;
    const clampedAtEnd = this.top > maximum && Math.abs(maximum - top) <= 1;
    this.top = top;
    this.revision++;
    if (direction < 0) {
      if (!(this.following && clampedAtEnd)) this.following = false;
    } else if (this.inputDirection >= 0 && maximum - top <= 1)
      this.following = true;
    return true;
  }
}
