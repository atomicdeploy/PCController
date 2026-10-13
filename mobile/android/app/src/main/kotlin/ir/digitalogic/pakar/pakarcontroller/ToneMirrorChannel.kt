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

/** Streaming, frame-counted notes. Flush only for explicit Stop, ownership/
 * focus/lifecycle loss, or the independent six-second receiver lease. */
class ToneMirrorChannel(private val activity: Activity, messenger: BinaryMessenger) {
    private val channel = MethodChannel(messenger, "ir.digitalogic.pccontroller/tone-mirror")
    private val audio = activity.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private val executor = Executors.newSingleThreadExecutor()
    private val guard = Any()
    private val synth = BuzzerPcm()
    private val attributes = AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA)
        .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION).build()
    @Volatile private var session: String? = null
    @Volatile private var deadline = 0L
    private var track: AudioTrack? = null
    private var closed = false
    private var focus: AudioFocusRequest? = null
    @Volatile private var duck = 1.0
    private val focusListener = AudioManager.OnAudioFocusChangeListener {
        when (it) {
            AudioManager.AUDIOFOCUS_GAIN -> duck = 1.0
            AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK -> duck = 0.2
            else -> stopAll()
        }
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
                        synchronized(guard) {
                            synth.clear(); duck = 1.0; session = owner
                            deadline = SystemClock.elapsedRealtimeNanos() + 6_000_000_000L
                        }
                        executor.execute { render(owner, volume, result) }
                    }
                    "lease" -> {
                        check(session == owner && SystemClock.elapsedRealtimeNanos() < deadline) {
                            "Mirror stopped; enable again explicitly"
                        }
                        deadline = SystemClock.elapsedRealtimeNanos() + 6_000_000_000L
                        result.success(null)
                    }
                    "note" -> {
                        val hz = call.argument<Int>("frequency") ?: error("Hz required")
                        val ms = call.argument<Int>("duration_ms") ?: error("Duration required")
                        val delay = call.argument<Int>("start_delay_us") ?: 0
                        require(ms in 0..65535 && (hz == 0 || (hz in 20..20000 && ms > 0)))
                        require(delay in 0..250000)
                        synchronized(guard) {
                            check(session == owner && SystemClock.elapsedRealtimeNanos() < deadline)
                            val output = checkNotNull(track) { "Audio receiver not ready" }
                            if (hz == 0 && ms == 0) {
                                output.pause(); output.flush(); synth.clear(); output.play()
                            } else {
                                val head = output.playbackHeadPosition.toLong() and 0xffffffffL
                                synth.enqueue(hz, ms, head + synth.framesForMicros(delay))
                            }
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

    private fun render(owner: String, volume: Double, result: MethodChannel.Result) {
        var output: AudioTrack? = null
        var acknowledged = false
        try {
            val rate = synth.rate
            val minimum = AudioTrack.getMinBufferSize(rate, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT)
            check(minimum > 0)
            val builder = AudioTrack.Builder().setAudioAttributes(attributes)
                .setAudioFormat(AudioFormat.Builder().setSampleRate(rate)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO).setEncoding(AudioFormat.ENCODING_PCM_16BIT).build())
                .setTransferMode(AudioTrack.MODE_STREAM).setBufferSizeInBytes(maxOf(minimum, 1920))
            if (Build.VERSION.SDK_INT >= 26) builder.setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY)
            output = builder.build()
            check(output.state == AudioTrack.STATE_INITIALIZED)
            val buffer = BuzzerStreamBuffer(synth)
            synchronized(guard) { check(session == owner); track = output; output.play() }
            activity.runOnUiThread { result.success(null) }; acknowledged = true
            while (session == owner && SystemClock.elapsedRealtimeNanos() < deadline) {
                val written = synchronized(guard) {
                    if (session != owner) return@synchronized -1
                    // Keep the unwritten tail of a short transfer, including
                    // its original phase and envelope. No regeneration/drop.
                    buffer.write(volume * duck) { pcm, offset, count ->
                        output.write(pcm, offset, count, AudioTrack.WRITE_NON_BLOCKING)
                    }
                }
                if (session != owner) break
                check(written >= 0) { "AudioTrack write failed ($written)" }
                if (written == 0) Thread.sleep(2)
            }
        } catch (error: Exception) {
            if (!acknowledged) activity.runOnUiThread {
                result.error("tone_unavailable", error.message ?: "AudioTrack unavailable", null)
            }
            // Post-open failure clears ownership; the next note/lease reports
            // it to Dart rather than silently retaining a dead receiver.
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
            session = null; synth.clear()
            runCatching { track?.pause(); track?.flush() }
        }
        if (Build.VERSION.SDK_INT >= 26) focus?.let { audio.abandonAudioFocusRequest(it) }
        else { @Suppress("DEPRECATION") audio.abandonAudioFocus(focusListener) }
        focus = null
    }
    fun close() { closed = true; stopAll(); channel.setMethodCallHandler(null); executor.shutdownNow() }
}
