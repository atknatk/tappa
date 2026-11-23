package mt.taptime.app.staff

import mt.taptime.app.activation.DeviceKeys
import mt.taptime.app.activation.DeviceState
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.ClaimsReply
import mt.taptime.app.wire.Outcome

// "Missing times" (M16-44): the list and one send, signed by this phone's key. Nothing here is
// kept: the list is read when the screen opens and the answer is shown, never stored (§7).
class StaffFlow(private val client: AppClient, private val keys: DeviceKeys, private val state: DeviceState) {
    sealed interface Answer<out T> {
        data class Ok<T>(val value: T) : Answer<T>
        data object SignedOut : Answer<Nothing>
        data object Network : Answer<Nothing>
        /** The server refused the context (stale_context): the list is read again. */
        data object Stale : Answer<Nothing>
        data object Server : Answer<Nothing>
    }

    fun claims(): Answer<ClaimsReply> {
        val device = state.active() ?: return Answer.SignedOut
        val key = keys.load(device.keyAlias) ?: return Answer.SignedOut
        return answer(client.claims(device.deviceId, key))
    }

    /** One claim: the server's result word ("sent", "already_sent", "outside_window", …). */
    fun send(context: String, at: String): Answer<String> {
        val device = state.active() ?: return Answer.SignedOut
        val key = keys.load(device.keyAlias) ?: return Answer.SignedOut
        return answer(client.claimSend(device.deviceId, key, context, at))
    }

    private fun <T> answer(r: Outcome<T>): Answer<T> = when (r) {
        is Outcome.Ok -> Answer.Ok(r.value)
        Outcome.Unauthenticated -> Answer.SignedOut
        is Outcome.Failed -> Answer.Network
        is Outcome.Refused -> if (r.code == mt.taptime.app.wire.AppErrorCode.STALE_CONTEXT) Answer.Stale else Answer.Server
    }
}
