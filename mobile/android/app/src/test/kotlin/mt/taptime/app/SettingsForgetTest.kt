package mt.taptime.app

import java.util.UUID
import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.staff.StaffFlow
import mt.taptime.app.staff.StaffFlow.Answer
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.HttpUrlTransport
import mt.taptime.app.wire.ServerClock
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * [G38-a] (M16-38): after "Sign this phone out" and after "Delete my account" the phone holds no
 * key and no device -- the Keystore alias is deleted and the stored device cleared -- over a socket
 * (StaffFlow -> AppClient -> HttpUrlTransport -> FakeAppServer, which verifies the nonce'd
 * signature). A device the server no longer knows (401) is forgotten as well; a refusal or no
 * answer forgets nothing, so the person can try again.
 */
class SettingsForgetTest {
    private val server = FakeAppServer("unused-code")
    private val keys = JdkKeys()
    private val state = MemoryState()
    private val flow = StaffFlow(AppClient(HttpUrlTransport(server.base), ServerClock(), MiniJson), keys, state)

    @After
    fun stop() = server.close()

    private fun activated(): String {
        val key = keys.generate()
        val id = UUID.randomUUID().toString()
        server.devices[id] = key.signer.publicPoint
        state.device = ActiveDevice(id, key.alias, KeyBacking.TEE, "Maria Borg")
        return key.alias
    }

    private fun assertForgotten(alias: String) {
        assertTrue("no key left in the store: ${keys.live.keys}", keys.live.isEmpty())
        assertEquals(listOf(alias), keys.deleted)
        assertNull("no stored device", state.active())
    }

    @Test
    fun signingThisPhoneOutLeavesNoKeyAndNoDevice() {
        val alias = activated()
        assertEquals(Answer.Ok(Unit), flow.signOut())
        assertEquals(listOf("/app/v1/revoke"), server.forgets)
        assertForgotten(alias)
    }

    @Test
    fun theDeletionRequestLeavesNoKeyAndNoDevice() {
        val alias = activated()
        assertEquals(Answer.Ok(Unit), flow.deleteAccount())
        assertEquals(listOf("/app/v1/account/delete"), server.forgets)
        assertForgotten(alias)
    }

    @Test
    fun aDeviceTheServerNoLongerKnowsIsForgottenToo() {
        val alias = activated()
        server.devices.clear() // the panel signed the phones out first: every signed call is 401
        assertEquals(Answer.Ok(Unit), flow.deleteAccount())
        assertEquals(emptyList<String>(), server.forgets)
        assertForgotten(alias)
    }

    @Test
    fun aRefusedRequestForgetsNothing() {
        val alias = activated()
        server.forgetFailsOnce = 503 to "server_error"
        assertEquals(Answer.Server, flow.signOut())
        assertNotNull("the key stays for another try", keys.live[alias])
        assertNotNull(state.active())
    }
}
