package ir.digitalogic.pccontroller.mobile

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import android.os.Handler
import android.os.Looper
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

/** Android DNS-SD over mDNS: bounded scan, serialized resolution and cleanup.
 * Mirrors PCController metadata-only discovery; never fetches APIs/passwords.
 */
class DiscoveryChannel(context: Context, messenger: BinaryMessenger) {
    private val manager = context.getSystemService(Context.NSD_SERVICE) as NsdManager
    private val wifi = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
    private val main = Handler(Looper.getMainLooper())
    private val channel = MethodChannel(messenger, "ir.digitalogic.pccontroller/discovery")
    private var pending: MethodChannel.Result? = null
    private var generation = 0
    private var lock: WifiManager.MulticastLock? = null
    private val listeners = mutableListOf<NsdManager.DiscoveryListener>()
    private val found = linkedMapOf<String, Map<String, String>>()
    private val queue = ArrayDeque<NsdServiceInfo>()
    private var resolving = false
    init {
        channel.setMethodCallHandler { call, result ->
            if (call.method != "scan") { result.notImplemented() }
            else if (pending != null) { result.error("busy", "Discovery already running", null) }
            else {
                pending = result
                val epoch = ++generation
                found.clear(); queue.clear(); resolving = false
                try {
                    lock = wifi.createMulticastLock("pakar-discovery").apply { setReferenceCounted(false); acquire() }
                    for (type in listOf("_pakarcontroller._tcp.", "_http._tcp.", "_pccontroller._tcp.")) {
                        val listener = object: NsdManager.DiscoveryListener {
                            override fun onDiscoveryStarted(s: String) {}
                            override fun onDiscoveryStopped(s: String) {}
                            override fun onServiceLost(s: NsdServiceInfo) {}
                            override fun onStartDiscoveryFailed(s: String, code: Int) { main.post { listeners.remove(this) } }
                            override fun onStopDiscoveryFailed(s: String, code: Int) {}
                            override fun onServiceFound(s: NsdServiceInfo) { main.post {
                                if (epoch == generation && queue.size < 64 && found.size < 128) { queue.add(s); resolve(epoch) }
                            } }
                        }
                        listeners.add(listener)
                        manager.discoverServices(type, NsdManager.PROTOCOL_DNS_SD, listener)
                    }
                    main.postDelayed({ if (epoch == generation) finish() }, 4000)
                } catch (_: Exception) { finish("Discovery unavailable") }
            }
        }
    }
    @Suppress("DEPRECATION")
    private fun resolve(epoch: Int) {
        if (resolving || queue.isEmpty() || epoch != generation) return
        resolving = true
        val service = queue.removeFirst()
        try {
            manager.resolveService(service, object: NsdManager.ResolveListener {
                override fun onResolveFailed(s: NsdServiceInfo, code: Int) { main.post { if (epoch == generation) { resolving = false; resolve(epoch) } } }
                override fun onServiceResolved(s: NsdServiceInfo) { main.post {
                    if (epoch != generation) return@post
                    val address = s.host?.hostAddress
                    if (address != null && s.port in 1..65535 && !address.contains('%')) {
                        val host = if (address.contains(':')) "[$address]" else address
                        val endpoint = "http://$host:${s.port}"
                        found[endpoint] = mapOf("name" to s.serviceName.take(96), "endpoint" to endpoint)
                    }
                    resolving = false; resolve(epoch)
                } }
            })
        } catch (_: Exception) { resolving = false; resolve(epoch) }
    }
    private fun finish(error: String? = null) {
        val result = pending ?: return
        pending = null; ++generation
        for (listener in listeners.toList()) { try { manager.stopServiceDiscovery(listener) } catch (_: Exception) {} }
        listeners.clear(); queue.clear(); resolving = false
        lock?.let { if (it.isHeld) it.release() }; lock = null
        if (error == null) result.success(found.values.toList()) else result.error("discovery_failed", error, null)
    }
    fun close() { finish("Discovery closed"); channel.setMethodCallHandler(null) }
}
