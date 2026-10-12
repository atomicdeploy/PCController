package ir.digitalogic.pccontroller.mobile

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream
import java.util.UUID
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    private var discovery: DiscoveryChannel? = null
    private var toneMirror: ToneMirrorChannel? = null
    private var automation: MethodChannel? = null
    private val io = Executors.newSingleThreadExecutor()
    private var pending: MethodChannel.Result? = null
    private var maximum = 0
    private var closed = false
    private var writing = false
    private val destinations = mutableMapOf<String, Uri>()
    private val pickCode = 3011
    private val createCode = 3012

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        discovery = DiscoveryChannel(this, flutterEngine.dartExecutor.binaryMessenger)
        toneMirror = ToneMirrorChannel(this, flutterEngine.dartExecutor.binaryMessenger)
        automation = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "ir.digitalogic.pccontroller/automation")
        AutomationBridge.channel = automation
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "ir.digitalogic.pccontroller/documents")
            .setMethodCallHandler { call, result ->
                try {
                    when (call.method) {
                        "pick", "create" -> {
                            check(pending == null && destinations.isEmpty() && !writing) { "Finish the current document operation first" }
                            if (call.method == "pick") {
                                maximum = call.argument<Int>("maximum") ?: 0
                                require(maximum in 1..2097152) { "Invalid document size bound" }
                                val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                                    addCategory(Intent.CATEGORY_OPENABLE)
                                    type = "*/*" // Some providers mislabel .bin MIME; Dart checks extension and bytes.
                                    putExtra(Intent.EXTRA_ALLOW_MULTIPLE, false)
                                }
                                pending = result
                                startActivityForResult(intent, pickCode)
                            } else {
                                val name = call.argument<String>("name") ?: ""
                                require(name.matches(Regex("[A-Za-z0-9][A-Za-z0-9_.-]{0,119}\\.bin"))) { "Invalid backup name" }
                                val intent = Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                                    addCategory(Intent.CATEGORY_OPENABLE)
                                    type = "application/octet-stream"
                                    putExtra(Intent.EXTRA_TITLE, name)
                                }
                                pending = result
                                startActivityForResult(intent, createCode)
                            }
                        }
                        "abandon" -> {
                            destinations.remove(call.argument<String>("token"))
                            result.success(null) // Do not delete user-selected files or request broader permissions.
                        }
                        "write" -> {
                            val uri = destinations.remove(call.argument<String>("token"))
                                ?: error("Unknown or already consumed destination")
                            val bytes = call.argument<ByteArray>("bytes") ?: error("Missing backup bytes")
                            require(bytes.size in 1..2097152) { "Invalid firmware export bound" }
                            writing = true
                            io.execute {
                                try {
                                    contentResolver.openOutputStream(uri, "w")?.use { it.write(bytes); it.flush() }
                                        ?: error("Document provider cannot write this new document")
                                    runOnUiThread { writing = false; if (!closed) result.success(null) }
                                } catch (_: Exception) {
                                    runOnUiThread { writing = false; if (!closed) result.error("write_failed", "Export failed; an incomplete new document may remain. It was not retried.", null) }
                                }
                            }
                        }
                        else -> result.notImplemented()
                    }
                } catch (_: Exception) {
                    if (pending === result) pending = null
                    result.error("document_failed", "Document operation unavailable or invalid; nothing was replayed", null)
                }
            }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != pickCode && requestCode != createCode) return
        val result = pending ?: return
        val uri = data?.data
        if (resultCode != Activity.RESULT_OK || uri == null) {
            pending = null
            result.success(null)
            return
        }
        if (requestCode == createCode) {
            val token = UUID.randomUUID().toString()
            destinations[token] = uri
            pending = null
            result.success(token)
            return
        }
        val limit = maximum
        io.execute {
            try {
                val name = contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
                    if (cursor.moveToFirst()) cursor.getString(0) else null
                } ?: error("Document has no display name")
                val bytes = contentResolver.openInputStream(uri)?.use { input ->
                    val output = ByteArrayOutputStream()
                    val buffer = ByteArray(8192)
                    while (true) {
                        val count = input.read(buffer)
                        if (count < 0) break
                        require(output.size() + count <= limit) { "Document exceeds its bound" }
                        output.write(buffer, 0, count)
                    }
                    output.toByteArray()
                } ?: error("Document cannot be read")
                runOnUiThread {
                    if (!closed) { pending = null; result.success(mapOf("name" to name, "bytes" to bytes)) }
                }
            } catch (_: Exception) {
                runOnUiThread { if (!closed) { pending = null; result.error("read_failed", "Selected document cannot be read or exceeds its size limit", null) } }
            }
        }
    }

    override fun onPause() {
        toneMirror?.stopAll() // Native cutoff also works if Dart stalls or loses lifecycle delivery.
        super.onPause()
    }

    override fun onDestroy() {
        if (AutomationBridge.channel === automation) AutomationBridge.channel = null
        automation = null
        toneMirror?.close(); toneMirror = null
        discovery?.close(); discovery = null
        closed = true
        pending?.error("closed", "Document picker closed; no operation will be reopened automatically", null)
        pending = null
        destinations.clear()
        io.shutdownNow()
        super.onDestroy()
    }
}
