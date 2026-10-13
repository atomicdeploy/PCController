package ir.digitalogic.pccontroller.mobile

import kotlin.math.min

/** Platform-free, bounded frame timeline. Call under the receiver guard. */
internal class BuzzerPcm(val rate: Int = 48000) {
    private data class Note(val hz: Int, val start: Long, val end: Long)
    private val pending = ArrayList<Note>()
    private var active: Note? = null
    var cursor = 0L
        private set
    var revision = 0L
        private set
    private var phase = 0.0
    private var shift = 0L
    val pendingCount get() = pending.size
    fun framesForMicros(micros: Int) = micros.toLong() * rate / 1_000_000L

    fun enqueue(hz: Int, ms: Int, start: Long) {
        require(ms in 0..65535 && (hz == 0 || (hz in 20..20000 && ms > 0)))
        check(pending.size < 32) { "Buzzer queue full" }
        // Rebase followers by the same amount if fresh notes reach a receiver
        // that has already generated their slot. Preserve short-note cadence.
        if (start + shift < cursor) shift = cursor - start
        val first = start + shift
        pending.add(Note(hz, first, first + ms.toLong() * rate / 1000L))
        pending.sortBy { it.start }
    }

    fun clear() {
        pending.clear(); active = null; cursor = 0; phase = 0.0; shift = 0
        ++revision
    }

    fun render(pcm: ShortArray, count: Int, volume: Double) {
        require(count in 0..pcm.size && volume.isFinite() && volume in 0.0..1.0)
        for (i in 0 until count) {
            while (pending.isNotEmpty() && pending.first().start <= cursor) {
                active = pending.removeAt(0)
            }
            val note = active
            val end = if (note == null) 0 else min(note.end, pending.firstOrNull()?.start ?: Long.MAX_VALUE)
            var sample = 0.0
            if (note != null && note.hz > 0 && cursor < end) {
                val step = note.hz.toDouble() / rate
                // PolyBLEP removes square-wave discontinuity aliasing. The
                // 0.25ms edge ramp cannot swallow an entire 1-3ms short note.
                val square = (if (phase < 0.5) 1.0 else -1.0) +
                    blep(phase, step) - blep((phase + 0.5) % 1.0, step)
                val ramp = min(rate / 4000L, maxOf(1, (end - note.start) / 4))
                val envelope = min(1.0, min((cursor - note.start).toDouble() / ramp,
                    (end - cursor - 1).toDouble() / ramp)).coerceAtLeast(0.0)
                sample = square * envelope * volume * 0.7
                phase = (phase + step) % 1.0
            } else {
                active = null
            }
            pcm[i] = (sample.coerceIn(-1.0, 1.0) * 32767).toInt().toShort()
            ++cursor
        }
    }

    private fun blep(t: Double, dt: Double): Double = when {
        t < dt -> { val x = t / dt; x + x - x * x - 1.0 }
        t > 1.0 - dt -> { val x = (t - 1.0) / dt; x * x + x + x + 1.0 }
        else -> 0.0
    }
}

/** Retain partial transfers. Only an explicit synth.clear discards PCM. */
internal class BuzzerStreamBuffer(private val synth: BuzzerPcm, size: Int = 240) {
    private val pcm = ShortArray(size)
    private var offset = size
    private var revision = -1L
    fun write(volume: Double, writer: (ShortArray, Int, Int) -> Int): Int {
        if (revision != synth.revision) { revision = synth.revision; offset = pcm.size }
        if (offset == pcm.size) { synth.render(pcm, pcm.size, volume); offset = 0 }
        val written = writer(pcm, offset, pcm.size - offset)
        check(written in 0..(pcm.size - offset)) { "AudioTrack write failed ($written)" }
        offset += written
        return written
    }
}
