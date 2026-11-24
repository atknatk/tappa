package mt.taptime.app.staff

import mt.taptime.app.activation.DeviceKeys
import mt.taptime.app.activation.DeviceState
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.ClaimsReply
import mt.taptime.app.wire.HoursReply
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
        /** M16-31: the business has My hours turned off (not_found). */
        data object Off : Answer<Nothing>
        /**
         * "Show more" refused (400): the cursor is no longer this person's and week's (an older
         * shape, another walk). [value] is page one of the same week, read again: it replaces the
         * list, and no network problem is said.
         */
        data class Restarted<T>(val value: T) : Answer<T>
    }

    /**
     * Signs this phone out (POST /app/v1/revoke): the server revokes the device, then the phone
     * forgets it -- the key leaves the Keystore and the stored device is cleared. A device the
     * server no longer knows (401) is forgotten as well; a network failure forgets nothing.
     */
    fun signOut(): Answer<Unit> = forget { id, key -> client.revoke(id, key) }

    /**
     * "Delete my account" (M16-37): the server signs the person out everywhere and tells the
     * employer; the phone then forgets this device as signOut does. No record is deleted.
     */
    fun deleteAccount(): Answer<Unit> = forget { id, key -> client.deleteAccount(id, key) }

    private fun forget(call: (String, mt.taptime.app.wire.Signer) -> Outcome<Unit>): Answer<Unit> {
        val device = state.active() ?: return Answer.SignedOut
        val key = keys.load(device.keyAlias)
        if (key != null) {
            when (call(device.deviceId, key)) {
                is Outcome.Ok, Outcome.Unauthenticated -> Unit
                is Outcome.Failed -> return Answer.Network
                is Outcome.Refused -> return Answer.Server
            }
        }
        keys.delete(device.keyAlias)
        state.clear()
        return Answer.Ok(Unit)
    }

    /** My hours (M16-31): one page of a week; [week] null is this week, [after] the next page's cursor. */
    fun hours(week: String?, after: String?): Answer<HoursReply> {
        val device = state.active() ?: return Answer.SignedOut
        val key = keys.load(device.keyAlias) ?: return Answer.SignedOut
        return when (val r = client.hours(device.deviceId, key, week, after)) {
            is Outcome.Refused -> when {
                r.code == mt.taptime.app.wire.AppErrorCode.NOT_FOUND -> Answer.Off
                // Only a cursor is retried: page one refused is no cursor's fault, and once more
                // would be the same 400.
                after != null && r.status == 400 -> when (val first = hours(week, null)) {
                    is Answer.Ok -> Answer.Restarted(first.value)
                    else -> first
                }
                else -> answer(r)
            }
            else -> answer(r)
        }
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
