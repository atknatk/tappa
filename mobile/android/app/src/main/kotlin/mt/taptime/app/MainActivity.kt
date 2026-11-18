package mt.taptime.app

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.nfc.NfcAdapter
import android.nfc.Tag
import android.nfc.tech.Ndef
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
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
import mt.taptime.app.tap.Buttons
import mt.taptime.app.tap.Fix
import mt.taptime.app.tap.Retry
import mt.taptime.app.tap.TapFlow
import mt.taptime.app.tap.TapLocation
import mt.taptime.app.tap.TapProblem
import mt.taptime.app.ui.ActivatedScreen
import mt.taptime.app.ui.BlockedScreen
import mt.taptime.app.ui.CodeScreen
import mt.taptime.app.ui.LoadingScreen
import mt.taptime.app.ui.NfcState
import mt.taptime.app.ui.PlaqueTapScreen
import mt.taptime.app.ui.ReadyScreen
import mt.taptime.app.ui.ResultScreen
import mt.taptime.app.ui.RetryScreen
import mt.taptime.app.ui.SetUpScreen
import mt.taptime.app.ui.TapProblemScreen
import mt.taptime.app.ui.TapScreen
import mt.taptime.app.ui.TermsSheet
import mt.taptime.app.ui.WelcomeScreen
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.Brand
import mt.taptime.app.wire.HttpUrlTransport
import mt.taptime.app.wire.LookupReply
import mt.taptime.app.wire.OrgJsonDecoder
import mt.taptime.app.wire.ServerClock
import mt.taptime.app.wire.TapReply

// The staff app's one activity: the launch check and activation (M16-12), and the plaque tap
// (M16-13).
//
// HOW A PLAQUE OPENS IT (manifest): an NFC read of <base>/t (NDEF_DISCOVERED) or an App Link to the
// same URL (a QR scanned by the camera, a link). Either lands here with the URL as the intent's data,
// at launch or -- the activity is singleTop -- in onNewIntent. The URL goes to TapFlow.start and
// nowhere else; a URL that is not this server's <base>/t is "That link didn't work".
//
// STATE IS HELD HERE, IN MEMORY, AND NOWHERE ELSE. The manifest keeps this activity across
// configuration changes (android:configChanges), so a rotation does not rebuild it and the
// pending activation -- the invite code and the server's challenge -- and the tap screen's signed
// context never go into a saved state Bundle, which the system may write to disk. If the process
// dies, the person starts over: at the code screen, or with a new touch of the plaque.
//
// NETWORK AND KEYSTORE WORK runs on one background thread (a plain executor: no coroutine
// library is declared, Q36); results come back on the UI thread. The position reading of a press
// runs on the main thread (TapLocation) and hands its one result to the worker.
//
// DURING ACTIVATION the plaque is read in reader mode, only while step 3 is on screen and the
// activity is in the foreground; reader mode turns the NFC dispatch off, so the activation's read
// goes to activate/complete and nowhere else.
class MainActivity : ComponentActivity() {
    private sealed interface Screen {
        data object Loading : Screen
        data class Code(val problem: Problem?, val busy: Boolean = false, val notALink: Boolean = false) : Screen
        data class Welcome(
            val code: String, val facts: LookupReply,
            val problem: Problem? = null, val busy: Boolean = false, val terms: Boolean = false,
        ) : Screen
        data class Ready(val pending: PendingActivation) : Screen
        data class Tap(val pending: PendingActivation, val problem: Problem? = null, val busy: Boolean = false) : Screen
        data class Activated(val name: String, val secondDevice: Boolean, val brand: Brand) : Screen
        data class SetUp(val name: String) : Screen
        /** This phone cannot activate (KeyGate, or no NFC reader). */
        data class Blocked(val problem: Problem) : Screen
        /**
         * The launch check could not open the key (M16-12 hand-over (3)) or the clock is a day off
         * (M16-16 (4)): "Try again" runs it again.
         */
        data class Retryable(val problem: Problem) : Screen

        // --- the plaque tap (M16-13) ---
        /** The press is decided by this screen's [ready]; [busy] from the press until the answer. */
        data class Plaque(val url: String, val ready: TapFlow.Start.Ready, val busy: Boolean = false) : Screen
        data class Result(val reply: TapReply) : Screen
        data class TapTrouble(val problem: TapProblem, val url: String, val retry: Retry) : Screen
    }

    private var screen by mutableStateOf<Screen>(Screen.Loading)
    private var nfcState by mutableStateOf(NfcState.NONE)

    private val worker: ExecutorService = Executors.newSingleThreadExecutor()
    private lateinit var flow: ActivationFlow
    private lateinit var tapFlow: TapFlow
    private lateinit var location: TapLocation
    private var nfc: NfcAdapter? = null
    private var readerOn = false
    private var resumed = false

    // The location permission is asked AT THE PRESS, never at start (CLAUDE.md §4.2, the manifest's
    // note): the browser's prompt comes with getCurrentPosition at the press too. Whatever the
    // answer, the press goes on -- a refusal sends no fix (§5 rows 6-7, §4.6).
    private var afterPermission: (() -> Unit)? = null
    private val askLocation = registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        afterPermission?.invoke()
        afterPermission = null
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        val client = AppClient(HttpUrlTransport(BuildConfig.BASE_URL), ServerClock(), OrgJsonDecoder)
        val keys = KeystoreKeys(applicationContext)
        val state = PrefsDeviceState(applicationContext)
        flow = ActivationFlow(client, keys, state)
        tapFlow = TapFlow(client, keys, state, BuildConfig.BASE_URL)
        location = TapLocation(applicationContext)
        nfc = NfcAdapter.getDefaultAdapter(this)
        setContent { Render() }
        // A PLAQUE URL IS ACTED ON ONCE, when it is delivered. Reopened from Recents, or after the
        // system rebuilt the activity, the same intent comes back with the same, already spent,
        // counter: a press on it could only record a sun-invalid reject, and that reject would start
        // the person's 60-second debounce over their real next tap (tap.js keeps a restored NFC page
        // as it was for the same reason). Such a launch is the launcher's.
        val replayed = savedInstanceState != null || (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY) != 0
        val plaque = if (replayed) null else plaqueUrlOf(intent)
        if (plaque != null) startTap(plaque) else home()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val url = plaqueUrlOf(intent) ?: return
        when (val s = screen) {
            // A press in flight: its answer decides the screen. The new touch is not lost on the
            // plaque (its counter is the next one); the person taps again if they meant to.
            is Screen.Plaque -> if (!s.busy) startTap(url)
            // An activation with a consent already bound takes the touch, as the browser's
            // consented activation takes GET /t (ADR 0026). Step 3's reader mode usually reads it
            // first; this is the dispatch reaching a step-2 screen.
            is Screen.Ready -> {
                screen = Screen.Tap(s.pending)
                onPlaque(url)
            }
            is Screen.Tap -> if (!s.busy) onPlaque(url)
            // Mid-consent: the person is in the middle of setting up; their screen stays.
            is Screen.Welcome -> Unit
            is Screen.Code -> if (!s.busy) startTap(url)
            else -> startTap(url)
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
            is Screen.Code -> CodeScreen(s.problem, s.notALink, s.busy, ::submitCode)
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
            is Screen.Blocked -> BlockedScreen(s.problem)
            is Screen.Retryable -> RetryScreen(s.problem) { home() }
            is Screen.Plaque -> PlaqueTapScreen(s.ready.facts, s.busy) { want -> press(s, want) }
            is Screen.Result -> ResultScreen(s.reply)
            // The buttons are Buttons.of's (the iOS twin's): "Open in browser" only while the URL is
            // certainly unspent; "Try again" after a lost press goes HOME and resends nothing.
            is Screen.TapTrouble -> TapProblemScreen(
                s.problem,
                Buttons.of(s.problem, s.retry),
                onRetry = { if (s.retry == Retry.HOME) home() else startTap(s.url) },
                onBrowser = { openInBrowser(s.url) },
                onActivate = { screen = activationEntry(null) },
            )
        }
    }

    // --- launch and activation (M16-12) ---------------------------------------------------------

    private fun home() {
        screen = Screen.Loading
        work({ flow.home() }) { r ->
            screen = when (r) {
                ActivationFlow.HomeResult.NotActivated -> activationEntry(null)
                ActivationFlow.HomeResult.SignedOut -> activationEntry(Problem.SIGNED_OUT)
                ActivationFlow.HomeResult.KeyUnreadable -> Screen.Retryable(Problem.KEY_UNREADABLE)
                ActivationFlow.HomeResult.Clock -> Screen.Retryable(Problem.CLOCK)
                is ActivationFlow.HomeResult.SetUp -> Screen.SetUp(r.device.employeeName)
            }
        }
    }

    // The code screen, unless this phone cannot activate at all (ActivationFlow.refusedAtLaunch).
    private fun activationEntry(notice: Problem?): Screen =
        ActivationFlow.refusedAtLaunch(hasNfcReader = nfc != null)?.let { Screen.Blocked(it) } ?: Screen.Code(notice)

    private fun submitCode(pasted: String) {
        val code = InviteCode.from(pasted, BuildConfig.BASE_URL)
        if (code == null) {
            screen = Screen.Code(null, notALink = true)
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
                ActivationFlow.BindResult.Unsupported -> Screen.Blocked(Problem.NO_SECURE_KEY)
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
                is ActivationFlow.CompleteResult.Restart ->
                    if (r.problem == Problem.NO_SECURE_KEY) Screen.Blocked(r.problem) else Screen.Code(r.problem)
            }
            updateReader()
        }
    }

    // --- the plaque tap (M16-13) ----------------------------------------------------------------

    // tap/start: a signed read that writes nothing and advances no counter (GET /t's twin).
    private fun startTap(url: String) {
        screen = Screen.Loading
        updateReader()
        // Nothing was written by a start that failed in a way nobody classified: "Try again" is safe.
        work({ tapFlow.start(url) }, failed = { Screen.TapTrouble(TapProblem.SERVER, url, Retry.RESTART) }) { r ->
            screen = when (r) {
                is TapFlow.Start.Ready -> Screen.Plaque(url, r)
                is TapFlow.Start.Problem -> Screen.TapTrouble(r.problem, url, r.retry)
            }
        }
    }

    // The press: ONE position reading (the permission asked here the first time), then POST
    // /app/v1/tap. Both buttons are disabled at once -- a second press has nothing to hit while
    // the reading is under way (tap.js's markPending).
    private fun press(s: Screen.Plaque, want: String) {
        if (s.busy) return
        screen = s.copy(busy = true)
        withOneFix { fix ->
            // An unclassified failure of the press may have come after the request went out: no
            // retry of the same context, the next touch is a fresh tap.
            work({ tapFlow.press(s.ready, want.ifEmpty { null }, fix) }, failed = { Screen.TapTrouble(TapProblem.LOST, s.url, Retry.HOME) }) { r ->
                screen = when (r) {
                    is TapFlow.Press.Recorded -> Screen.Result(r.reply)
                    is TapFlow.Press.Problem -> Screen.TapTrouble(r.problem, s.url, r.retry)
                }
            }
        }
    }

    private fun withOneFix(then: (Fix?) -> Unit) {
        val fine = checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
        val coarse = checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED
        if (fine || coarse) {
            location.read(then)
            return
        }
        // Asked once per press while not granted; a person who chose "don't ask again" is answered
        // by the system at once, and the press goes on without a fix.
        afterPermission = { location.read(then) }
        try {
            askLocation.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
        } catch (_: ActivityNotFoundException) {
            afterPermission = null
            then(null)
        }
    }

    // --- NFC and intents ------------------------------------------------------------------------

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

    // The plaque URL an intent carries: the NFC dispatch (NDEF_DISCOVERED) and the App Link (VIEW)
    // both put it in the intent's data. Anything else -- the launcher's MAIN -- carries none.
    private fun plaqueUrlOf(intent: Intent?): String? {
        if (intent == null) return null
        if (intent.action != NfcAdapter.ACTION_NDEF_DISCOVERED && intent.action != Intent.ACTION_VIEW) return null
        return intent.data?.toString()
    }

    private fun open(path: String) {
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(BuildConfig.BASE_URL.trimEnd('/') + path)))
    }

    // "Open in browser" (ADR 0045 Bedel 8): the SAME plaque URL, in a browser. A plain VIEW intent
    // would come straight back here -- this app is the URL's App Link -- so the intent carries a
    // selector that only browsers answer (CATEGORY_APP_BROWSER); the browser receives the URL.
    // Offered only while the URL is certainly unspent (Buttons.of): never after a press whose
    // answer was lost.
    private fun openInBrowser(url: String) {
        val view = Intent(Intent.ACTION_VIEW, Uri.parse(url)).apply {
            addCategory(Intent.CATEGORY_BROWSABLE)
            selector = Intent.makeMainSelectorActivity(Intent.ACTION_MAIN, Intent.CATEGORY_APP_BROWSER)
        }
        try {
            startActivity(view)
        } catch (_: ActivityNotFoundException) {
            // No browser on the phone: there is nowhere to send the person, and the screen says
            // what to do (tap again / ask the manager).
        }
    }

    // A failure nobody classified (the Keystore refusing a key, an unexpected exception) ends
    // the flow with the server-side text: nothing was activated or recorded, and the person
    // starts again -- at the code screen during activation, with a new touch during a tap. The
    // exception is not shown or logged -- its message is not ours.
    private fun <T> work(task: () -> T, failed: () -> Screen = { Screen.Code(Problem.SERVER) }, done: (T) -> Unit) {
        worker.execute {
            val r = try {
                Result.success(task())
            } catch (e: Exception) { // Keystore failures are checked exceptions (KeyStoreException)
                Result.failure(e)
            }
            runOnUiThread {
                if (isDestroyed) return@runOnUiThread
                r.fold(done) {
                    screen = failed()
                    updateReader()
                }
            }
        }
    }
}
