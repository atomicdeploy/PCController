package ir.digitalogic.pccontroller.mobile

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkRequest
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
    private var externalLock = false
    private var registration: NsdManager.RegistrationListener? = null
    private val connectivity = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
    private val networks = linkedMapOf<Network, String>()
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var networkChange: Runnable? = null
    private val listeners = mutableListOf<NsdManager.DiscoveryListener>()
    private val found = linkedMapOf<String, Map<String, String>>()
    private val queue = ArrayDeque<NsdServiceInfo>()
    private var resolving = false
    init {
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onLinkPropertiesChanged(network: Network, properties: LinkProperties) { main.post {
                val fingerprint = properties.linkAddresses.map { it.toString() }.sorted().joinToString(",")
                if (networks[network] != fingerprint) { networks[network] = fingerprint; networkChanged() }
            } }
            override fun onLost(network: Network) { main.post { if (networks.remove(network) != null) networkChanged() } }
        }
        try { connectivity.registerNetworkCallback(NetworkRequest.Builder().build(), callback); networkCallback = callback }
        catch (_: Exception) { /* Missing permission does not invent reachability. */ }
        channel.setMethodCallHandler { call, result ->
            if (call.method == "acquire") {
                try { acquireLock(); externalLock = true; result.success(null) }
                catch (_: Exception) { result.error("multicast_lock", "Wi-Fi multicast lock failed", null) }
            }
            else if (call.method == "advertise") {
                val port = call.argument<Int>("port") ?: 0
                if (port !in 1024..65535 || registration != null) { result.error("invalid_advertisement", "Invalid port or already advertised", null) }
                else {
                    acquireLock()
                    val service = NsdServiceInfo().apply {
                        serviceName = "Controller · ${android.os.Build.MODEL.take(40)}"
                        serviceType = "_digitalogic._tcp."
                        setPort(port); setAttribute("product", "controller-mobile")
                        setAttribute("health", "/healthz"); setAttribute("rpc", "/rpc")
                    }
                    val listener = object : NsdManager.RegistrationListener {
                        override fun onServiceRegistered(info: NsdServiceInfo) { main.post {
                            if (registration === this) result.success(mapOf("name" to info.serviceName, "port" to info.port))
                        } }
                        override fun onRegistrationFailed(info: NsdServiceInfo, code: Int) { main.post {
                            if (registration === this) { registration = null; result.error("registration_failed", "DNS-SD registration failed ($code)", null) }
                        } }
                        override fun onServiceUnregistered(info: NsdServiceInfo) {}
                        override fun onUnregistrationFailed(info: NsdServiceInfo, code: Int) {}
                    }
                    registration = listener
                    try { manager.registerService(service, NsdManager.PROTOCOL_DNS_SD, listener) }
                    catch (_: Exception) { registration = null; result.error("registration_failed", "DNS-SD registration failed", null) }
                }
            }
            else if (call.method == "unadvertise") { unadvertise(); result.success(null) }
            else if (call.method == "release") {
                externalLock = false; if (pending == null && registration == null) releaseLock(); result.success(null)
            }
            else if (call.method != "scan") { result.notImplemented() }
            else if (pending != null) { result.error("busy", "Discovery already running", null) }
            else {
                pending = result
                val epoch = ++generation
                found.clear(); queue.clear(); resolving = false
                try {
                    acquireLock()
                    for (type in listOf("_pakarcontroller._tcp.", "_http._tcp.", "_pccontroller._tcp.", "_pealayer._tcp.", "_digitalogic._tcp.")) {
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
                        val product = when {
                            s.serviceType.startsWith("_pccontroller.") -> "pccontroller"
                            s.serviceType.startsWith("_pealayer.") -> "pealayer"
                            else -> s.attributes["product"]?.toString(Charsets.UTF_8)?.take(48) ?: ""
                        }
                        found[endpoint] = mapOf("name" to s.serviceName.take(96), "endpoint" to endpoint, "product" to product)
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
        if (!externalLock && registration == null) releaseLock()
        if (error == null) result.success(found.values.toList()) else result.error("discovery_failed", error, null)
    }
    private fun acquireLock() {
        if (lock?.isHeld == true) return
        lock = wifi.createMulticastLock("digitalogic-discovery").apply { setReferenceCounted(false); acquire() }
    }
    private fun releaseLock() { lock?.let { if (it.isHeld) it.release() }; lock = null }
    private fun networkChanged() {
        networkChange?.let { main.removeCallbacks(it) }
        val action = Runnable { channel.invokeMethod("networkChanged", mapOf("networks" to networks.size)) }
        networkChange = action; main.postDelayed(action, 750)
    }
    private fun unadvertise() {
        registration?.let { try { manager.unregisterService(it) } catch (_: Exception) {} }; registration = null
        if (!externalLock && pending == null) releaseLock()
    }
    fun close() {
        networkCallback?.let { try { connectivity.unregisterNetworkCallback(it) } catch (_: Exception) {} }; networkCallback = null
        networkChange?.let { main.removeCallbacks(it) }; networkChange = null
        externalLock = false; finish("Discovery closed"); unadvertise(); releaseLock(); channel.setMethodCallHandler(null)
    }
}
