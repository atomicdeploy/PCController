package ir.digitalogic.pccontroller.mobile

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import io.flutter.plugin.common.MethodChannel
import org.json.JSONObject

internal object AutomationBridge { var channel: MethodChannel? = null }

// Explicit, bounded ordered-broadcast adapter. Authorization and advertised
// target/action admission live in the same Dart router as HTTP/WS/NDJSON.
// It does not launch a background engine, execute a shell, or require root.
class AutomationReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != "ir.digitalogic.pccontroller.mobile.RPC") return
        val request = intent.getStringExtra("rpc") ?: ""
        val auth = intent.getStringExtra("auth") ?: ""
        if (request.toByteArray(Charsets.UTF_8).size > 32768 || auth.length > 128) {
            resultCode = -1; resultData = "{\"error\":\"request_too_large\"}"; return
        }
        val channel = AutomationBridge.channel
        if (channel == null) {
            resultCode = -1; resultData = "{\"error\":\"app_not_running\"}"; return
        }
        val pending = goAsync()
        val handler = Handler(Looper.getMainLooper())
        var finished = false
        fun finish(text: String, code: Int) {
            if (finished) return
            finished = true
            pending.setResultCode(code)
            pending.setResultData(if (text.length <= 8192) text else "{\"error\":\"response_too_large_use_http\"}")
            pending.finish()
        }
        val timeout = Runnable { finish("{\"error\":\"outcome_unknown_not_retried\"}", -1) }
        handler.postDelayed(timeout, 8000)
        channel.invokeMethod("request", mapOf("rpc" to request, "auth" to auth), object : MethodChannel.Result {
            override fun success(result: Any?) {
                handler.removeCallbacks(timeout)
                val text = if (result is Map<*, *>) JSONObject(result).toString() else "null"
                finish(text, if (result is Map<*, *> && result.containsKey("error")) -1 else 0)
            }
            override fun error(code: String, message: String?, details: Any?) {
                handler.removeCallbacks(timeout); finish("{\"error\":\"request_failed\"}", -1)
            }
            override fun notImplemented() {
                handler.removeCallbacks(timeout); finish("{\"error\":\"app_not_ready\"}", -1)
            }
        })
    }
}
