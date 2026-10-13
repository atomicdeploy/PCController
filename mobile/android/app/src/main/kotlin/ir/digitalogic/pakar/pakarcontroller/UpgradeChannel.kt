package ir.digitalogic.pccontroller.mobile

import android.app.Activity
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.io.RandomAccessFile
import java.security.MessageDigest
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import java.util.zip.ZipFile

/** Self-package only, bounded chunks, same signer, newer version, OS consent.
 * No shell/root installation, uninstall, third-party APKs or arbitrary paths.
 */
class UpgradeChannel(private val activity: Activity, messenger: BinaryMessenger) {
    companion object { var liveChannel: MethodChannel? = null }
    private val channel = MethodChannel(messenger, "ir.digitalogic.pccontroller/upgrade")
    private val io = Executors.newSingleThreadExecutor()
    private val waiting = AtomicInteger()
    private val root = File(activity.cacheDir, "peer-upgrades")
    @Volatile private var closed = false
    private var transfer: Transfer? = null
    private var cachedExport: Pair<String, Map<String, Any>>? = null
    private val maximum = 128L * 1024 * 1024
    private val chunkSize = 16384
    private data class Transfer(val id: String, val file: File, val size: Long,
        val hash: String, var offset: Long = 0, var ready: Boolean = false,
        var last: Long = System.currentTimeMillis())
    init {
        liveChannel = channel
        // Only this module's abandoned staging files, never installed APKs/data.
        root.listFiles()?.filter { it.name.matches(Regex("[a-f0-9-]{36}\\.apk")) &&
            System.currentTimeMillis() - it.lastModified() > 30 * 60 * 1000 }?.forEach { it.delete() }
        channel.setMethodCallHandler { call, result ->
            if (waiting.incrementAndGet() > 8) {
                waiting.decrementAndGet(); result.error("busy", "Upgrade queue full", null)
            } else io.execute {
                try {
                    check(!closed) { "Upgrade channel closed" }
                    transfer?.let { if (System.currentTimeMillis() - it.last > 10 * 60 * 1000) abort() }
                    val args = call.arguments as? Map<*, *> ?: emptyMap<String, Any>()
                    val value = when (call.method) {
                        "info" -> exportInfo()
                        "read" -> read(args)
                        "begin" -> begin(args)
                        "write" -> write(args)
                        "finish" -> finish(args)
                        "abort" -> { requireTransfer(args); abort(); mapOf("aborted" to true) }
                        "status" -> status()
                        "install" -> install(args)
                        "permission" -> {
                            activity.runOnUiThread { if (!closed && Build.VERSION.SDK_INT >= 26) {
                                activity.startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                                    Uri.parse("package:${activity.packageName}")))
                            } }
                            status() + mapOf("requested" to true)
                        }
                        else -> error("Unknown upgrade method")
                    }
                    activity.runOnUiThread { if (!closed) result.success(value) }
                } catch (error: Exception) {
                    activity.runOnUiThread { if (!closed) result.error("upgrade_rejected", error.message ?: "Upgrade rejected", null) }
                } finally { waiting.decrementAndGet() }
            }
        }
    }
    @Suppress("DEPRECATION")
    private fun info(path: String? = null): PackageInfo {
        val flags = if (Build.VERSION.SDK_INT >= 28) PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES
        return if (path == null) activity.packageManager.getPackageInfo(activity.packageName, flags)
        else activity.packageManager.getPackageArchiveInfo(path, flags) ?: error("Invalid APK")
    }
    @Suppress("DEPRECATION")
    private fun signers(value: PackageInfo): List<String> {
        val signatures = if (Build.VERSION.SDK_INT >= 28) value.signingInfo?.apkContentsSigners else value.signatures
        return (signatures ?: emptyArray()).map { hash(it.toByteArray()) }.sorted().also {
            require(it.isNotEmpty()) { "Package has no verified signer" }
        }
    }
    @Suppress("DEPRECATION")
    private fun version(value: PackageInfo): Long = if (Build.VERSION.SDK_INT >= 28) value.longVersionCode else value.versionCode.toLong()
    private fun hash(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
    private fun digest(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input -> val buffer = ByteArray(65536); while (true) {
            val n = input.read(buffer); if (n < 0) break; digest.update(buffer, 0, n)
        } }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }
    private fun installedApk(): File {
        require(activity.applicationInfo.splitSourceDirs.isNullOrEmpty()) { "Split installs cannot be exported as one APK" }
        return File(activity.applicationInfo.sourceDir).also { require(it.length() in 1..maximum) { "APK exceeds transfer bound" } }
    }
    private fun exportInfo(): Map<String, Any> {
        val file = installedApk(); val key = "${file.path}:${file.length()}:${file.lastModified()}"
        cachedExport?.let { if (it.first == key) return it.second }
        val current = info()
        return mapOf("package" to activity.packageName, "version_code" to version(current),
            "version_name" to (current.versionName ?: ""), "bytes" to file.length(),
            "sha256" to digest(file), "signers" to signers(current), "chunk_bytes" to chunkSize,
            "abis" to Build.SUPPORTED_ABIS.toList(), "sdk" to Build.VERSION.SDK_INT).also { cachedExport = Pair(key, it) }
    }
    private fun read(args: Map<*, *>): Map<String, Any> {
        val file = installedApk(); val offset = (args["offset"] as? Number)?.toLong() ?: -1
        val length = (args["length"] as? Number)?.toInt() ?: chunkSize
        require(offset in 0..file.length() && length in 1..chunkSize) { "Invalid APK read range" }
        val bytes = ByteArray(minOf(length.toLong(), file.length() - offset).toInt())
        RandomAccessFile(file, "r").use { it.seek(offset); it.readFully(bytes) }
        return mapOf("offset" to offset, "bytes" to bytes, "eof" to (offset + bytes.size == file.length()))
    }
    private fun begin(args: Map<*, *>): Map<String, Any> {
        require(transfer == null) { "Finish or abort the existing transfer" }
        val size = (args["bytes"] as? Number)?.toLong() ?: -1
        val hash = args["sha256"] as? String ?: ""
        require(size in 1..maximum && hash.matches(Regex("[a-f0-9]{64}"))) { "Invalid APK manifest" }
        // Before 0% or file creation; installation needs an additional OS copy.
        require(activity.cacheDir.usableSpace >= size * 2 + 64 * 1024 * 1024) { "Insufficient space for APK and Android installation" }
        require(root.exists() || root.mkdirs()) { "Cannot create private staging" }
        val retained = root.listFiles()?.filter { it.name.matches(Regex("[a-f0-9-]{36}\\.apk")) }?.sumOf { it.length() } ?: 0
        require(retained + size <= maximum) { "Interrupted APK cache reaches its 128 MiB budget; clear this app's cache or wait for expiry" }
        val id = UUID.randomUUID().toString(); val file = File(root, "$id.apk")
        require(file.createNewFile()) { "Cannot create private transfer" }
        transfer = Transfer(id, file, size, hash)
        return status()
    }
    private fun requireTransfer(args: Map<*, *>): Transfer = (transfer ?: error("No staged APK")).also {
        require(args["transfer_id"] == it.id) { "Transfer identity changed" }; it.last = System.currentTimeMillis()
    }
    private fun write(args: Map<*, *>): Map<String, Any> {
        val current = requireTransfer(args)
        val offset = (args["offset"] as? Number)?.toLong() ?: -1
        val bytes = args["bytes"] as? ByteArray ?: error("Missing APK bytes")
        require(!current.ready && offset == current.offset && bytes.size in 1..chunkSize &&
            offset + bytes.size <= current.size) { "Invalid or replayed APK chunk; query status, do not retry blindly" }
        RandomAccessFile(current.file, "rw").use { it.seek(offset); it.write(bytes) }
        current.offset += bytes.size
        return status()
    }
    private fun finish(args: Map<*, *>): Map<String, Any> {
        val current = requireTransfer(args)
        require(current.offset == current.size && current.file.length() == current.size) { "Incomplete APK" }
        try {
            require(digest(current.file) == current.hash) { "APK digest mismatch" }
            val archive = info(current.file.path); val installed = info()
            require(archive.packageName == activity.packageName) { "Wrong application package" }
            require(signers(archive) == signers(installed)) { "APK signer differs from installed app" }
            require(version(archive) > version(installed)) { "APK is not newer than installed app" }
            if (Build.VERSION.SDK_INT >= 24) require((archive.applicationInfo?.minSdkVersion ?: 0) <= Build.VERSION.SDK_INT) { "APK requires a newer Android version" }
            ZipFile(current.file).use { zip ->
                val abis = mutableSetOf<String>(); var expanded = 0L; var count = 0
                for (entry in zip.entries().asSequence()) {
                    require(++count <= 20000 && entry.size >= 0) { "Invalid APK archive" }
                    expanded += entry.size; require(expanded <= 512L * 1024 * 1024) { "APK expansion too large" }
                    if (entry.name.startsWith("lib/") && entry.name.endsWith(".so")) abis.add(entry.name.split('/').getOrElse(1) { "" })
                }
                require(abis.isEmpty() || Build.SUPPORTED_ABIS.any { it in abis }) { "APK does not support this CPU" }
            }
            current.ready = true
            return status() + mapOf("package" to archive.packageName, "version_code" to version(archive), "version_name" to (archive.versionName ?: ""))
        } catch (error: Exception) { abort(); throw error }
    }
    private fun install(args: Map<*, *>): Map<String, Any> {
        val current = requireTransfer(args); require(current.ready) { "APK has not been verified" }
        if (Build.VERSION.SDK_INT >= 26 && !activity.packageManager.canRequestPackageInstalls())
            return status() + mapOf("state" to "permission_required")
        val installer = activity.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(activity.packageName); setSize(current.size)
            if (Build.VERSION.SDK_INT >= 31) setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_REQUIRED)
        }
        val sessionId = installer.createSession(params)
        try {
            installer.openSession(sessionId).use { session ->
                session.openWrite("base.apk", 0, current.size).use { output ->
                    current.file.inputStream().use { it.copyTo(output, 65536) }; session.fsync(output)
                }
                val intent = Intent(activity, UpgradeResultReceiver::class.java).putExtra("session_id", sessionId)
                val flags = PendingIntent.FLAG_UPDATE_CURRENT or (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
                val callback = PendingIntent.getBroadcast(activity, sessionId, intent, flags)
                session.commit(callback.intentSender)
            }
        } catch (error: Exception) { installer.abandonSession(sessionId); throw error }
        abort() // Android now owns its one required installation copy.
        return mapOf("state" to "awaiting_android", "session_id" to sessionId)
    }
    private fun status(): Map<String, Any> {
        val current = transfer
        return if (current == null) mapOf("state" to "idle", "install_status" to (activity.getSharedPreferences("peer-upgrades", Context.MODE_PRIVATE).getString("receipt", "") ?: ""))
        else mapOf("state" to if (current.ready) "verified" else "receiving", "transfer_id" to current.id,
            "bytes" to current.size, "received" to current.offset, "sha256" to current.hash,
            "install_permission" to (Build.VERSION.SDK_INT < 26 || activity.packageManager.canRequestPackageInstalls()))
    }
    private fun abort() { transfer?.file?.delete(); transfer = null }
    fun close() { closed = true; if (liveChannel === channel) liveChannel = null; channel.setMethodCallHandler(null); io.execute { abort() }; io.shutdown() }
}

/** The platform verifies and asks the human; a committed session is not success. */
class UpgradeResultReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        val receipt = org.json.JSONObject().put("session_id", intent.getIntExtra("session_id", -1))
            .put("status", status).put("message", intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE) ?: "")
        context.getSharedPreferences("peer-upgrades", Context.MODE_PRIVATE).edit().putString("receipt", receipt.toString()).apply()
        Handler(Looper.getMainLooper()).post {
            UpgradeChannel.liveChannel?.invokeMethod("installStatus", mapOf("session_id" to intent.getIntExtra("session_id", -1), "status" to status,
                "message" to (intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE) ?: "")))
        }
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
            @Suppress("DEPRECATION") val confirmation = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
            try { confirmation?.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)?.let { context.startActivity(it) } }
            catch (_: Exception) { /* Receipt remains pending, never claim installed. */ }
        }
    }
}
