package mt.taptime.app

import android.content.Intent
import android.net.Uri
import android.nfc.NfcAdapter
import android.nfc.Tag
import android.nfc.tech.Ndef
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import mt.taptime.app.activation.ActivationFlow
import mt.taptime.app.activation.InviteCode
import mt.taptime.app.activation.PendingActivation
import mt.taptime.app.activation.PlaqueRead
import mt.taptime.app.activation.Problem
import mt.taptime.app.device.KeystoreKeys
import mt.taptime.app.device.PrefsDeviceState
import mt.taptime.app.ui.ActivatedScreen
import mt.taptime.app.ui.CodeScreen
import mt.taptime.app.ui.LoadingScreen
import mt.taptime.app.ui.NfcState
import mt.taptime.app.ui.ReadyScreen
import mt.taptime.app.ui.SetUpScreen
import mt.taptime.app.ui.TapScreen
import mt.taptime.app.ui.TermsSheet
import mt.taptime.app.ui.WelcomeScreen
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.Brand
import mt.taptime.app.wire.HttpUrlTransport
import mt.taptime.app.wire.LookupReply
import mt.taptime.app.wire.OrgJsonDecoder
import mt.taptime.app.wire.ServerClock

// The staff app's one activity. M16-12: launch check and activation; the tap screen is M16-13's.
//
// STATE IS HELD HERE, IN MEMORY, AND NOWHERE ELSE. The manifest keeps this activity across
// configuration changes (android:configChanges), so a rotation does not rebuild it and the
// pending activation -- the invite code and the server's challenge -- never goes into a saved
// state Bundle, which the system may write to disk. If the process dies, the person starts over
// at the code screen; a second bind simply moves the binding (ADR 0026).
//
// NETWORK AND KEYSTORE WORK runs on one background thread (a plain executor: no coroutine
// library is declared, Q36); results come back on the UI thread.
//
// THE PLAQUE IS READ IN READER MODE, only while step 3 is on screen and the activity is in the
// foreground: the read's URL goes to activate/complete and nowhere else, and outside step 3 a
// plaque touch is the system's (the browser today; the app's own filter is M16-13's).
class MainActivity : ComponentActivity() {
    private sealed interface Screen {
        data object Loading : Screen
        data class Code(val problem: Problem?, val busy: Boolean = false) : Screen
        data class Welcome(
            val code: String, val facts: LookupReply,
            val problem: Problem? = null, val busy: Boolean = false, val terms: Boolean = false,
        ) : Screen
        data class Ready(val pending: PendingActivation) : Screen
        data class Tap(val pending: PendingActivation, val problem: Problem? = null, val busy: Boolean = false) : Screen
        data class Activated(val name: String, val secondDevice: Boolean, val brand: Brand) : Screen
        data class SetUp(val name: String) : Screen
    }

    private var screen by mutableStateOf<Screen>(Screen.Loading)
    private var nfcState by mutableStateOf(NfcState.NONE)

    private val worker: ExecutorService = Executors.newSingleThreadExecutor()
    private lateinit var flow: ActivationFlow
    private var nfc: NfcAdapter? = null
    private var readerOn = false
    private var resumed = false

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        val client = AppClient(HttpUrlTransport(BuildConfig.BASE_URL), ServerClock(), OrgJsonDecoder)
        flow = ActivationFlow(client, KeystoreKeys(applicationContext), PrefsDeviceState(applicationContext))
        nfc = NfcAdapter.getDefaultAdapter(this)
        setContent { Render() }
        work({ flow.home() }) { r ->
            screen = when (r) {
                ActivationFlow.HomeResult.NotActivated -> Screen.Code(null)
                ActivationFlow.HomeResult.SignedOut -> Screen.Code(Problem.SIGNED_OUT)
                is ActivationFlow.HomeResult.SetUp -> Screen.SetUp(r.device.employeeName)
            }
        }
    }

    override fun onResume() {
        super.onResume()
        resumed = true
        updateReader()
    }

    override fun onPause() {
        resumed = false
        stopReader()
        super.onPause()
    }

    override fun onDestroy() {
        worker.shutdownNow()
        super.onDestroy()
    }

    @androidx.compose.runtime.Composable
    private fun Render() {
        when (val s = screen) {
            Screen.Loading -> LoadingScreen()
            is Screen.Code -> CodeScreen(s.problem, s.busy, ::submitCode)
            is Screen.Welcome -> if (s.terms) {
                BackHandler { screen = s.copy(terms = false) }
                TermsSheet(s.facts.businessName, { open("/legal/privacy") }, { open("/legal/terms") }) {
                    screen = s.copy(terms = false)
                }
            } else {
                BackHandler(enabled = !s.busy) { screen = Screen.Code(null) }
                WelcomeScreen(s.facts, s.problem, s.busy,
                    onTerms = { screen = s.copy(terms = true) },
                    onAgree = { agree(s) },
                    onBack = { screen = Screen.Code(null) })
            }
            is Screen.Ready -> {
                BackHandler { back(s.pending) }
                ReadyScreen(s.pending.facts, onReady = { screen = Screen.Tap(s.pending); updateReader() }, onBack = { back(s.pending) })
            }
            is Screen.Tap -> {
                BackHandler(enabled = !s.busy) { screen = Screen.Ready(s.pending); updateReader() }
                TapScreen(s.pending.facts, nfcState, s.problem, s.busy) { screen = Screen.Ready(s.pending); updateReader() }
            }
            is Screen.Activated -> ActivatedScreen(s.name, s.secondDevice, s.brand)
            is Screen.SetUp -> SetUpScreen(s.name, null)
        }
    }

    private fun submitCode(pasted: String) {
        val code = InviteCode.from(pasted)
        if (code == null) {
            screen = Screen.Code(Problem.BAD_LINK)
            return
        }
        screen = Screen.Code(null, busy = true)
        work({ flow.lookup(code) }) { r ->
            screen = when (r) {
                is ActivationFlow.LookupResult.Facts -> Screen.Welcome(code, r.facts)
                is ActivationFlow.LookupResult.Refused -> Screen.Code(r.problem)
            }
        }
    }

    private fun agree(s: Screen.Welcome) {
        screen = s.copy(busy = true, problem = null)
        work({ flow.bind(s.code, s.facts) }) { r ->
            screen = when (r) {
                is ActivationFlow.BindResult.Bound -> Screen.Ready(r.pending)
                is ActivationFlow.BindResult.Restart -> Screen.Code(r.problem)
                is ActivationFlow.BindResult.Retry -> s.copy(busy = false, problem = r.problem)
            }
        }
    }

    // Back from step 2 to step 1: the bind is dropped with its key. Agreeing again binds a new
    // one, as the browser's consent POST moves the binding.
    private fun back(p: PendingActivation) {
        screen = Screen.Welcome(p.code, p.facts)
        worker.execute {
            // A key that cannot be deleted now is an unused Keystore entry, not a failure the
            // person can act on; an uncaught exception here would end the app.
            try {
                flow.abandon(p)
            } catch (_: Exception) {
            }
        }
    }

    private fun onPlaque(url: String) {
        val s = screen as? Screen.Tap ?: return
        if (s.busy) return
        val sun = PlaqueRead.sunQuery(url, BuildConfig.BASE_URL)
        if (sun == null) {
            screen = s.copy(problem = Problem.TAP_FAILED)
            return
        }
        screen = s.copy(busy = true, problem = null)
        work({ flow.complete(s.pending, sun) }) { r ->
            screen = when (r) {
                is ActivationFlow.CompleteResult.Activated ->
                    Screen.Activated(r.device.employeeName, r.secondDevice, s.pending.facts.brand)
                is ActivationFlow.CompleteResult.TapAgain -> Screen.Tap(r.pending, r.problem)
                is ActivationFlow.CompleteResult.Rebind -> Screen.Welcome(r.code, s.pending.facts, problem = r.problem)
                is ActivationFlow.CompleteResult.Restart -> Screen.Code(r.problem)
            }
            updateReader()
        }
    }

    private fun updateReader() {
        val adapter = nfc
        nfcState = when {
            adapter == null -> NfcState.NONE
            !adapter.isEnabled -> NfcState.OFF
            else -> NfcState.READY
        }
        if (screen is Screen.Tap && nfcState == NfcState.READY) startReader() else stopReader()
    }

    private fun startReader() {
        val adapter = nfc ?: return
        if (readerOn || !resumed) return
        // NFC-A only: NTAG 424 DNA. The platform's NDEF check reads the file -- the read the chip
        // answers with a fresh counter and CMAC -- and caches the message on the Ndef object.
        adapter.enableReaderMode(this, ::onTag, NfcAdapter.FLAG_READER_NFC_A, null)
        readerOn = true
    }

    private fun stopReader() {
        if (!readerOn) return
        nfc?.disableReaderMode(this)
        readerOn = false
    }

    // Called on a binder thread.
    private fun onTag(tag: Tag) {
        val url = Ndef.get(tag)?.cachedNdefMessage?.records?.firstOrNull()?.toUri()?.toString()
        runOnUiThread { if (url != null) onPlaque(url) else onPlaque("") }
    }

    private fun open(path: String) {
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(BuildConfig.BASE_URL.trimEnd('/') + path)))
    }

    // A failure nobody classified (the Keystore refusing a key, an unexpected exception) ends
    // the flow at the code screen with the server-side text: nothing was activated, and the
    // person starts again. The exception is not shown or logged -- its message is not ours.
    private fun <T> work(task: () -> T, done: (T) -> Unit) {
        worker.execute {
            val r = try {
                Result.success(task())
            } catch (e: Exception) { // Keystore failures are checked exceptions (KeyStoreException)
                Result.failure(e)
            }
            runOnUiThread {
                if (isDestroyed) return@runOnUiThread
                r.fold(done) {
                    screen = Screen.Code(Problem.SERVER)
                    updateReader()
                }
            }
        }
    }
}
