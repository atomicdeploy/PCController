package ir.digitalogic.pccontroller.mobile

import android.app.Activity
import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioTrack
import android.os.Build
import android.os.SystemClock
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors
import kotlin.math.PI
import kotlin.math.min
import kotlin.math.sin

/** PCM at the reported Hz, finite monotonic deadlines and an independent 6s
 * receiver lease. No ToneGenerator approximation, audio file or melody replay.
 * Timing/cancellation policy adapted from PCController buzzer_mirror.go. */
class ToneMirrorChannel(private val activity: Activity, messenger: BinaryMessenger) {
    private data class Note(val hz: Int, val end: Long, val duration: Long)
    private val channel = MethodChannel(messenger, "ir.digitalogic.pccontroller/tone-mirror")
    private val audio = activity.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private val executor = Executors.newSingleThreadExecutor()
    private val guard = Any()
    private val attributes = AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA)
        .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION).build()
    @Volatile private var session: String? = null
    @Volatile private var deadline = 0L
    @Volatile private var note = Note(0, 0, 0)
    private var track: AudioTrack? = null
    private var closed = false
    private var focus: AudioFocusRequest? = null
    private val focusListener = AudioManager.OnAudioFocusChangeListener {
        if (it != AudioManager.AUDIOFOCUS_GAIN) stopAll()
    }

    init {
        channel.setMethodCallHandler { call, result ->
            try {
                val owner = call.argument<String>("session") ?: error("Session required")
                require(owner.matches(Regex("[0-9a-f]{32}")))
                when (call.method) {
                    "open" -> {
                        check(!closed)
                        val volume = call.argument<Double>("volume") ?: error("Volume required")
                        require(volume.isFinite() && volume in 0.0..1.0)
                        stopAll()
                        val granted = if (Build.VERSION.SDK_INT >= 26) {
                            val request = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT_MAY_DUCK)
                                .setAudioAttributes(attributes).setOnAudioFocusChangeListener(focusListener).build()
                            focus = request
                            audio.requestAudioFocus(request)
                        } else {
                            @Suppress("DEPRECATION")
                            audio.requestAudioFocus(focusListener, AudioManager.STREAM_MUSIC,
                                AudioManager.AUDIOFOCUS_GAIN_TRANSIENT_MAY_DUCK)
                        }
                        check(granted == AudioManager.AUDIOFOCUS_REQUEST_GRANTED) { "Audio focus denied" }
                        session = owner; deadline = SystemClock.elapsedRealtimeNanos() + 6_000_000_000L
                        executor.execute { render(owner, volume.toFloat(), result) }
                    }
                    "lease" -> {
                        check(session == owner) { "Mirror stopped; enable again explicitly" }
                        deadline = SystemClock.elapsedRealtimeNanos() + 6_000_000_000L
                        result.success(null)
                    }
                    "note" -> {
                        check(session == owner && SystemClock.elapsedRealtimeNanos() < deadline)
                        val hz = call.argument<Int>("frequency") ?: error("Hz required")
                        val ms = call.argument<Int>("duration_ms") ?: error("Duration required")
                        require(ms in 0..65535 && (hz == 0 || (hz in 20..20000 && ms > 0)))
                        synchronized(guard) {
                            // Flush previous PCM before replacement/pause/Stop; never concatenate
                            // delayed jobs or leave a tone latched after Dart disappears.
                            track?.pause(); track?.flush()
                            val duration = ms * 1_000_000L
                            note = Note(hz, SystemClock.elapsedRealtimeNanos() + duration, duration)
                        }
                        result.success(null)
                    }
                    "stop" -> { if (session == owner) stopAll(); result.success(null) }
                    else -> result.notImplemented()
                }
            } catch (error: Exception) {
                result.error("tone_unavailable", error.message ?: "PCM mirror unavailable", null)
            }
        }
    }

    private fun render(owner: String, volume: Float, result: MethodChannel.Result) {
        var output: AudioTrack? = null
        var acknowledged = false
        try {
            val rate = 48000
            val minimum = AudioTrack.getMinBufferSize(rate, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT)
            check(minimum > 0)
            output = AudioTrack.Builder().setAudioAttributes(attributes)
                .setAudioFormat(AudioFormat.Builder().setSampleRate(rate)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO).setEncoding(AudioFormat.ENCODING_PCM_16BIT).build())
                .setTransferMode(AudioTrack.MODE_STREAM).setBufferSizeInBytes(maxOf(minimum, 1920)).build()
            check(output.state == AudioTrack.STATE_INITIALIZED)
            val pcm = ShortArray(240) // 5ms bounded chunks; no per-note files or large allocations.
            synchronized(guard) { check(session == owner); track = output }
            activity.runOnUiThread { result.success(null) }; acknowledged = true
            var previous: Note? = null
            var frame = 0L
            while (session == owner && SystemClock.elapsedRealtimeNanos() < deadline) {
                val current = note
                if (current !== previous) { previous = current; frame = 0 }
                val now = SystemClock.elapsedRealtimeNanos()
                if (current.hz == 0 || now >= current.end) {
                    synchronized(guard) { if (session == owner) { output.pause(); output.flush() } }
                    Thread.sleep(5); continue
                }
                val count = min(pcm.size, ((current.end - now) * rate / 1_000_000_000L).toInt())
                if (count <= 0) continue
                for (i in 0 until count) {
                    val elapsed = (frame + i) * 1_000_000_000.0 / rate
                    val fade = min(1.0, min(elapsed / 3_000_000.0,
                        (current.end - now - i * 1_000_000_000.0 / rate) / 3_000_000.0)).coerceAtLeast(0.0)
                    pcm[i] = (sin(2.0 * PI * current.hz * (frame + i) / rate) * 32767 * volume * fade).toInt().toShort()
                }
                val written = synchronized(guard) {
                    if (session != owner || note !== current) 0 else {
                        output.play()
                        output.write(pcm, 0, count, AudioTrack.WRITE_NON_BLOCKING)
                    }
                }
                check(written >= 0) { "AudioTrack write failed" }
                frame += written
                if (written == 0) Thread.sleep(2)
            }
        } catch (error: Exception) {
            if (!acknowledged) activity.runOnUiThread {
                result.error("tone_unavailable", error.message ?: "AudioTrack unavailable", null)
            }
        } finally {
            synchronized(guard) {
                if (track === output) track = null
                runCatching { output?.pause(); output?.flush(); output?.release() }
            }
            activity.runOnUiThread { if (session == owner) stopAll() }
        }
    }

    fun stopAll() {
        synchronized(guard) {
            session = null; note = Note(0, 0, 0)
            runCatching { track?.pause(); track?.flush() }
        }
        if (Build.VERSION.SDK_INT >= 26) focus?.let { audio.abandonAudioFocusRequest(it) }
        else { @Suppress("DEPRECATION") audio.abandonAudioFocus(focusListener) }
        focus = null
    }
    fun close() { closed = true; stopAll(); channel.setMethodCallHandler(null); executor.shutdownNow() }
}
