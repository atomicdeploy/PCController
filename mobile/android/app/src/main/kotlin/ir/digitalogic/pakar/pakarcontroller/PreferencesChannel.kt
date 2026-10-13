package ir.digitalogic.pccontroller.mobile

import android.app.Activity
import android.app.ActivityManager
import android.graphics.BitmapFactory
import android.content.ComponentName
import android.content.pm.PackageManager
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

/** Branded mini-app entry points. Keep at least one enabled and enable the
 * destination before disabling another; never kill the running Flutter app. */
class PreferencesChannel(private val activity: Activity, messenger: BinaryMessenger) {
    private val channel = MethodChannel(messenger, "ir.digitalogic.pccontroller/preferences")
    private val aliases = mapOf("pccontroller" to ".ControllerLauncher", "pealayer" to ".PealayerLauncher")
    private fun component(key: String) = ComponentName(activity.packageName, activity.packageName + aliases.getValue(key))
    private fun enabled(key: String): Boolean {
        val state = activity.packageManager.getComponentEnabledSetting(component(key))
        return state == PackageManager.COMPONENT_ENABLED_STATE_ENABLED ||
            (state == PackageManager.COMPONENT_ENABLED_STATE_DEFAULT && key == "pccontroller")
    }
    fun section(): String = if (activity.intent?.component?.className?.endsWith("PealayerLauncher") == true) "pealayer" else "pccontroller"
    fun launch() = channel.invokeMethod("launch", section())
    init {
        channel.setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "initialSection" -> result.success(section())
                    "setTask" -> {
                        val title = call.argument<String>("title") ?: error("Title required")
                        require(title == "PCController" || title == "Pealayer")
                        val bytes = call.argument<ByteArray>("icon") ?: error("Icon required")
                        require(bytes.size in 1..1048576)
                        val bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size) ?: error("Invalid icon")
                        require(bitmap.width <= 1024 && bitmap.height <= 1024)
                        val color = (call.argument<Number>("color") ?: error("Color required")).toLong().toInt() or 0xff000000.toInt()
                        @Suppress("DEPRECATION")
                        val description = ActivityManager.TaskDescription(title, bitmap, color)
                        activity.setTaskDescription(description)
                        result.success(null)
                    }
                    "launchers" -> result.success(aliases.keys.filter { enabled(it) })
                    "setLaunchers" -> {
                        val selected = call.argument<List<String>>("values")?.toSet() ?: error("Selection required")
                        require(selected.isNotEmpty() && aliases.keys.containsAll(selected))
                        for (key in selected) activity.packageManager.setComponentEnabledSetting(component(key),
                            PackageManager.COMPONENT_ENABLED_STATE_ENABLED, PackageManager.DONT_KILL_APP)
                        for (key in aliases.keys - selected) activity.packageManager.setComponentEnabledSetting(component(key),
                            PackageManager.COMPONENT_ENABLED_STATE_DISABLED, PackageManager.DONT_KILL_APP)
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            } catch (_: Exception) { result.error("preferences_failed", "Android appearance settings unavailable", null) }
        }
    }
    fun close() = channel.setMethodCallHandler(null)
}
