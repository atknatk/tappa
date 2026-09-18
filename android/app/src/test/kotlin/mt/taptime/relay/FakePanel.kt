package mt.taptime.relay

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import java.net.URLDecoder

/**
 * An in-process stand-in for the panel's three encode routes, speaking the exact
 * wire shape of internal/handler/plaqueencode.go. It exists so that the tests drive
 * the REAL HttpRelayServer — headers, form encoding, redirect handling — rather than
 * a fake behind the RelayServer interface that could never notice a missing Origin.
 *
 * The default script is a complete ten-exchange round. A test replaces [script] to
 * inject a fault, a refusal, a malformed body or a never-ending round.
 */
class FakePanel : AutoCloseable {
    class Seen(val path: String, val headers: Map<String, List<String>>, val form: Map<String, String>)
    class Answer(val status: Int, val contentType: String?, val body: String)

    val seen = mutableListOf<Seen>()
    val handle = "0123456789abcdef0123456789abcdef"

    /** Ten distinct C-APDUs, one per exchange. Contents are arbitrary — the loop never reads them. */
    val commands: List<ByteArray> = (0 until STEPS.size).map { i -> byteArrayOf(0x90.toByte(), (0x10 + i).toByte(), 0x00, 0x00, i.toByte()) }

    private var stepIndex = 0

    /** The scripted round. Tests may wrap or replace it. */
    var script: (Seen) -> Answer = { s -> defaultRound(s) }

    private val server: HttpServer = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0).apply {
        createContext("/") { ex -> handle(ex) }
        start()
    }

    val base: String = "http://127.0.0.1:${server.address.port}"

    fun begins(): List<Seen> = seen.filter { it.path == HttpRelayServer.ENCODE_PATH }
    fun steps(): List<Seen> = seen.filter { it.path == HttpRelayServer.STEP_PATH }
    fun aborts(): List<Seen> = seen.filter { it.path == HttpRelayServer.ABORT_PATH }

    fun progress(command: ByteArray, step: String, done: Boolean = false, extra: String = ""): Answer =
        json(200, """{"session":"$handle","command":"${Hex.encode(command)}","step":"$step","done":$done$extra}""")

    fun fault(status: Int, word: String): Answer = json(status, """{"fault":"$word"}""")

    fun json(status: Int, body: String): Answer = Answer(status, "application/json; charset=utf-8", body)

    fun html(status: Int): Answer = Answer(status, "text/html; charset=utf-8", "<html>panel</html>")

    fun defaultRound(s: Seen): Answer = when (s.path) {
        HttpRelayServer.ENCODE_PATH -> {
            stepIndex = 0
            progress(commands[0], "begin")
        }
        HttpRelayServer.STEP_PATH -> {
            require(s.form["session"] == handle) { "step without the handle" }
            require(!s.form["rapdu"].isNullOrEmpty()) { "step without an rapdu" }
            val completed = STEPS[stepIndex]
            stepIndex++
            if (stepIndex == STEPS.size) {
                progress(ByteArray(0), completed, done = true)
            } else {
                progress(commands[stepIndex], completed)
            }
        }
        HttpRelayServer.ABORT_PATH -> json(200, """{"session":"","command":"","step":"abort","done":false}""")
        else -> html(404)
    }

    private fun handle(ex: HttpExchange) {
        val body = ex.requestBody.readBytes().toString(Charsets.UTF_8)
        val form = body.split('&').filter { it.isNotEmpty() }.associate { pair ->
            val (k, v) = pair.split('=', limit = 2).let { if (it.size == 2) it[0] to it[1] else it[0] to "" }
            URLDecoder.decode(k, "UTF-8") to URLDecoder.decode(v, "UTF-8")
        }
        val headers = ex.requestHeaders.entries.associate { (name, values) -> name.lowercase() to values.toList() }
        val s = Seen(ex.requestURI.path, headers, form)
        synchronized(seen) { seen += s }
        val a = script(s)
        val bytes = a.body.toByteArray(Charsets.UTF_8)
        if (a.contentType != null) ex.responseHeaders.add("Content-Type", a.contentType)
        if (a.status == 303) ex.responseHeaders.add("Location", "/admin/login")
        ex.sendResponseHeaders(a.status, if (bytes.isEmpty()) -1 else bytes.size.toLong())
        if (bytes.isNotEmpty()) ex.responseBody.use { it.write(bytes) }
        ex.close()
    }

    override fun close() = server.stop(0)

    companion object {
        /** The ten step names of internal/encode/driver.go's roundSteps, in order. */
        val STEPS = listOf(
            "select",
            "getversion.1", "getversion.2", "getversion.3",
            "authenticate.1", "authenticate.2",
            "getcarduid",
            "writedata",
            "changekey.sdmfileread",
            "changefilesettings",
        )
    }
}

/** A chip that answers every command with a fixed status word and remembers what it was sent. */
open class ScriptedChip : Chip {
    val received = mutableListOf<ByteArray>()
    override fun transceive(command: ByteArray): ByteArray {
        received += command.copyOf()
        return byteArrayOf(0x91.toByte(), 0x00)
    }
}
