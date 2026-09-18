package mt.taptime.relay

import org.json.JSONException
import org.json.JSONObject

/**
 * The server, as the relay loop sees it — the three routes of
 * internal/handler/plaqueencode.go and nothing else:
 *
 *     POST /admin/plaques/encode          (empty body)
 *     POST /admin/plaques/encode/step     session=<handle>&rapdu=<hex>
 *     POST /admin/plaques/encode/abort    session=<handle>
 *
 * Declared at the consumer (CLAUDE.md §7). [HttpRelayServer] is the real one; the
 * tests run the loop against an in-process HTTP server so that the headers the
 * real client sends are what gets asserted.
 */
interface RelayServer {
    fun begin(): Reply
    fun step(session: String, rapdu: ByteArray): Reply
    fun abort(session: String): Reply
}

/**
 * One answer from the server, already classified. The shapes mirror the endpoint's
 * own contract: one success body, one fault body, and the panel chain's non-JSON
 * refusals of which only the status code is ever read.
 */
sealed class Reply {
    /**
     * A 200 with the four-field body. [command] is empty exactly when the round is
     * over. [trailingFault] carries a `fault` word that arrived in the SAME body as
     * `done: true` — see [parse] for why that combination is a completion, not a
     * failure.
     *
     * [session] is a bearer credential (encodeReply.Session's own comment). It is
     * held here because the loop cannot continue without it, and it is kept out of
     * [toString] so that no log line, no error message and no test failure message
     * can print it by accident.
     */
    class Progress(
        val session: String,
        val command: ByteArray,
        val step: String,
        val done: Boolean,
        val trailingFault: String? = null,
    ) : Reply() {
        override fun toString(): String =
            "Progress(step=$step, done=$done, command=${command.size}B" +
                (if (trailingFault != null) ", trailingFault=$trailingFault" else "") + ")"
    }

    /** A JSON `{"fault": "..."}` body with its HTTP status. The round is over on the server. */
    data class Fault(val status: Int, val fault: String) : Reply()

    /**
     * A non-JSON answer from the panel's chain ahead of the endpoint — a 303 from
     * sameOriginGate or requireAdmin, a 429 HTML page from floodGate or sessionGate.
     * plaqueencode.go: "THE RELAY READS THE STATUS CODE, NEVER THE BODY, for anything
     * other than 200." Only the status is carried.
     */
    data class Refused(val status: Int) : Reply()

    /** A body that fits none of the shapes above — a server this client does not understand. */
    data class Malformed(val status: Int, val why: String) : Reply()

    /** The request never completed: no route, timeout, TLS failure. */
    data class Unreachable(val why: String) : Reply()

    companion object {
        /**
         * Classifies one HTTP answer.
         *
         * THE `done` RULE, MECHANISED. encode.Progress.Done says: "READ Done BEFORE
         * READING THE ERROR." A round can complete on silicon and still fail after
         * it (the chip is personalised; marking the row failed), and such a round
         * must NEVER be re-run — the chip's secret has changed and a second round
         * dies at the row insert in a way that reads like stale inventory. So `done`
         * is read FIRST here, before `fault` and before the status code: a body
         * carrying `"done": true` is a completion whatever else it carries.
         *
         * ⚠️ WHAT THE WIRE CARRIES TODAY: nothing more than `done`. plaqueencode.go's
         * p.Done arm answers with the plain four-field reply and keeps the failure
         * in its log, so [Progress.trailingFault] is never filled by the current
         * server. The parser reads the shape the contract describes; that the server
         * does not yet put the word on the wire is a server-side gap, recorded by the
         * orchestrator, not a reason to read `done` any later.
         */
        fun parse(status: Int, contentType: String?, body: String): Reply {
            if (!isJson(contentType)) return Refused(status)
            val obj = try {
                JSONObject(body)
            } catch (e: JSONException) {
                return Malformed(status, "body is not a JSON object")
            }
            if (obj.optBoolean("done", false)) {
                return Progress(
                    session = obj.optString("session", ""),
                    command = ByteArray(0),
                    step = obj.optString("step", ""),
                    done = true,
                    trailingFault = obj.optString("fault", "").ifEmpty { null },
                )
            }
            if (obj.has("fault")) {
                val fault = obj.optString("fault", "")
                if (fault.isEmpty()) return Malformed(status, "empty fault word")
                return Fault(status, fault)
            }
            if (status != 200) return Malformed(status, "non-200 JSON without a fault word")
            if (!obj.has("session") || !obj.has("command") || !obj.has("step")) {
                return Malformed(status, "200 body without the four fields")
            }
            val command = try {
                Hex.decode(obj.getString("command"))
            } catch (e: IllegalArgumentException) {
                return Malformed(status, "command is not hex")
            }
            return Progress(
                session = obj.getString("session"),
                command = command,
                step = obj.getString("step"),
                done = false,
            )
        }

        private fun isJson(contentType: String?): Boolean {
            val ct = contentType?.trim()?.lowercase() ?: return false
            return ct.startsWith("application/json")
        }
    }
}
