package mt.taptime.app.wire

import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL

/** One HTTP exchange, as the client sees it. */
class HttpReply(val status: Int, val date: String?, val body: ByteArray)

/** The client's only way out. Throws [IOException] when there is no answer at all. */
fun interface Transport {
    fun exchange(method: String, path: String, headers: Map<String, String>, body: ByteArray?): HttpReply
}

// java.net.HttpURLConnection, the platform's client (ADR 0045 §2.2, Q36: no network library).
// It is plain java.net, so the JVM tests drive THIS class against an in-process server rather
// than a stand-in behind the interface.
//
// What it does not do, on purpose: follow redirects (the /app/v1 routes never redirect, and a
// redirect would move a signed request to a path it was not signed for), cache (every answer is
// no-store anyway), or compress the request body (the server hashes the raw bytes it receives).
class HttpUrlTransport(
    private val baseUrl: String,
    private val connectTimeoutMillis: Int = 10_000,
    private val readTimeoutMillis: Int = 15_000,
) : Transport {
    override fun exchange(method: String, path: String, headers: Map<String, String>, body: ByteArray?): HttpReply {
        val conn = URL(baseUrl.trimEnd('/') + path).openConnection() as HttpURLConnection
        try {
            conn.instanceFollowRedirects = false
            conn.useCaches = false
            conn.connectTimeout = connectTimeoutMillis
            conn.readTimeout = readTimeoutMillis
            conn.requestMethod = method
            conn.setRequestProperty("Accept", "application/json")
            for ((k, v) in headers) conn.setRequestProperty(k, v)
            if (body != null) {
                conn.doOutput = true
                conn.setRequestProperty("Content-Type", "application/json")
                conn.setFixedLengthStreamingMode(body.size)
                conn.outputStream.use { it.write(body) }
            }
            val status = conn.responseCode
            val stream: InputStream? = if (status >= 400) conn.errorStream else conn.inputStream
            return HttpReply(status, conn.getHeaderField("Date"), stream?.use { readCapped(it) } ?: ByteArray(0))
        } finally {
            conn.disconnect()
        }
    }

    // Every /app/v1 answer is small (the largest is a lookup's names and brand); the cap keeps a
    // misbehaving middlebox from filling memory.
    private fun readCapped(input: InputStream): ByteArray {
        val out = ByteArrayOutputStream()
        val buf = ByteArray(4096)
        while (true) {
            val n = input.read(buf)
            if (n < 0) break
            out.write(buf, 0, n)
            if (out.size() > MAX_REPLY) throw IOException("reply larger than $MAX_REPLY bytes")
        }
        return out.toByteArray()
    }

    private companion object {
        const val MAX_REPLY = 64 * 1024
    }
}
