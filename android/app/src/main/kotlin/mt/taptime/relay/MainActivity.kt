package mt.taptime.relay

import android.app.Activity
import android.content.Context
import android.graphics.Color
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.LayerDrawable
import android.nfc.NfcAdapter
import android.nfc.Tag
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.view.inputmethod.InputMethodManager
import android.webkit.CookieManager
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean

/**
 * One screen, five states (K5): signed out → ready → writing → done / fault.
 *
 * Sign-in is the panel's own (K3): a WebView opens /admin/login, the operator signs
 * in exactly as in a browser — CSRF, the multi-business "choose" step, the flood
 * gate all stay the panel's business — and the session cookie the panel set is read
 * back from CookieManager and attached to the relay's three POSTs. No HTML is
 * scraped, no CSRF is imitated, and no password is ever held by this app — and the
 * DEVICE is told not to keep one either: the WebView opts out of the platform's
 * Autofill (setUpWebView), otherwise Samsung Pass / Google Password Manager would
 * offer to store the admin's password in a device or cloud vault outside the app
 * (security audit, F2).
 *
 * WHAT THE PHONE HOLDS, IN THE ADR'S WORDS RATHER THAN A SLOGAN: no plaque secret
 * as such — none is derived or delivered here — but not "no secret": for one round
 * its memory carries the sealed APDUs that ADR 0017 §3 calls EQUIVALENT to the
 * plaque's secret (they are sealed under the chip's public factory value), plus
 * the bearer handle and the panel cookie; none of it is zeroed and a debug build
 * gives it up to `am dumpheap`. README.md counts all three.
 *
 * WHAT IS LOGGED, AND WHAT NEVER IS (CLAUDE.md §7). Log lines below carry step
 * names, byte counts, outcome class names and fault words. They never carry the
 * session handle (a bearer credential), a C-APDU or R-APDU, or the cookie. The
 * relay loop does not even expose the handle to this class — Outcome has no field
 * for it — so the property holds by construction, and android/README.md shows the
 * grep that re-measures it.
 */
class MainActivity : Activity() {

    private enum class State { SIGNED_OUT, READY, WRITING, DONE, FAULT }

    private lateinit var panel: View
    private lateinit var serverRow: View
    private lateinit var serverField: EditText
    private lateinit var saveButton: Button
    private lateinit var checkButton: Button
    private lateinit var stamp: TextView
    private lateinit var headline: TextView
    private lateinit var message: TextView
    private lateinit var detail: TextView
    private lateinit var primaryButton: Button
    private lateinit var signOutButton: Button
    private lateinit var web: WebView

    private var state = State.SIGNED_OUT
    private var base: String = DEFAULT_SERVER
    private var nfc: NfcAdapter? = null

    /** Reader mode is on. enableReaderMode is re-entrant on the platform but each call reconfigures the NFC service; call it once. */
    private var readerOn = false

    /** A round is in flight on the reader thread; a second tag is ignored until it ends. */
    private val busy = AtomicBoolean(false)

    /** The screen is waiting for a plaque. Cleared on done/fault so the SAME plaque left on the phone cannot start a second round. */
    @Volatile
    private var armed = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        panel = findViewById(R.id.panel)
        serverRow = findViewById(R.id.serverRow)
        serverField = findViewById(R.id.serverField)
        saveButton = findViewById(R.id.saveButton)
        checkButton = findViewById(R.id.checkButton)
        stamp = findViewById(R.id.stamp)
        headline = findViewById(R.id.headline)
        message = findViewById(R.id.message)
        detail = findViewById(R.id.detail)
        primaryButton = findViewById(R.id.primaryButton)
        signOutButton = findViewById(R.id.signOutButton)
        web = findViewById(R.id.web)

        // The stored address is re-validated under THIS build's cleartext rule, so a
        // value a debug build saved cannot outlive it into a stricter build.
        base = try {
            HttpRelayServer.normaliseBase(prefs().getString(PREF_SERVER, DEFAULT_SERVER) ?: DEFAULT_SERVER, allowHttp = BuildConfig.DEBUG)
        } catch (e: IllegalArgumentException) {
            DEFAULT_SERVER
        }
        serverField.setText(base)
        nfc = NfcAdapter.getDefaultAdapter(this)

        saveButton.setOnClickListener { saveServer() }
        checkButton.setOnClickListener { checkServer() }
        primaryButton.setOnClickListener { onPrimary() }
        signOutButton.setOnClickListener { signOut() }
        setUpWebView()
        setUpBack()

        show(State.SIGNED_OUT, "Checking sign-in…", "")
        probeSignIn()
    }

    override fun onResume() {
        super.onResume()
        if (state != State.SIGNED_OUT) enableReader()
    }

    /**
     * Reader mode goes off with the activity, and the COST of that is a round cut in
     * two: a call, a notification shade, a screen timeout mid-round means the next
     * transceive throws TagLost and the loop aborts. What that leaves behind depends
     * on how far the round got (Wording.afterInterruption): after exchange
     * ROW_WRITTEN_AFTER_EXCHANGES an inventory row squats the uid, and after
     * SECRET_INSTALLED_AFTER_EXCHANGES the plaque's secret is installed with SDM still off
     * and the row written — recoverable only through ADR 0017 §5.3's path, which is
     * NOT shipped.
     *
     * ⚠️ WHETHER THERE IS AN ALTERNATIVE IS NOT MEASURED. The auditor's reading of
     * AOSP is that NfcActivityManager ties reader mode to the activity's RESUMED
     * state on its own, so not calling disableReaderMode here would change nothing;
     * that reading was not measured on this device (it would need a build without
     * this call, a signed-in session, and a mid-round backgrounding). Until it is,
     * this call is explicit so that the behaviour does not depend on the platform's.
     */
    override fun onPause() {
        super.onPause()
        disableReader()
    }

    private fun disableReader() {
        if (!readerOn) return
        readerOn = false
        nfc?.disableReaderMode(this)
    }

    // --- sign-in -------------------------------------------------------------------

    private fun server(): HttpRelayServer = HttpRelayServer(base, cookie = { panelCookie() }, allowHttp = BuildConfig.DEBUG)

    /** The panel cookie as CookieManager holds it for the panel path, or null when there is none. */
    private fun panelCookie(): String? {
        val all = CookieManager.getInstance().getCookie("$base${HttpRelayServer.PANEL_PATH}/") ?: return null
        return if (all.contains("$PANEL_COOKIE=")) all else null
    }

    private fun probeSignIn() {
        if (panelCookie() == null) {
            show(State.SIGNED_OUT, "Sign in to the panel to start encoding plaques.", "")
            return
        }
        Thread {
            val ok = server().probeSignedIn()
            runOnUiThread {
                if (ok) becomeReady() else show(State.SIGNED_OUT, "Your panel sign-in has expired. Sign in again.", "")
            }
        }.start()
    }

    private fun setUpWebView() {
        // No script: the sign-in form works without one (the show-password toggle
        // ships hidden and is revealed by a script the panel serves; without it the
        // form is unchanged). The panel's dashboard is never used from here.
        web.settings.javaScriptEnabled = false
        web.settings.domStorageEnabled = false
        web.settings.allowFileAccess = false
        web.settings.setSupportMultipleWindows(false)
        // Autofill (API 26+) joins a WebView's forms; on this phone that is Samsung
        // Pass / Google Password Manager asking "save this password?" after the
        // sign-in POST, which would persist the admin's password outside the app
        // (audit F2). Opt the whole subtree out. Not measured on a live session (a
        // live sign-in against production is out of bounds); the flag is the
        // platform's documented switch.
        web.importantForAutofill = View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS
        CookieManager.getInstance().setAcceptCookie(true)
        web.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                // Stay on the panel's origin; anything else is not a sign-in.
                val url = request.url.toString()
                return !url.startsWith("$base/")
            }

            override fun onPageFinished(view: WebView, url: String?) {
                // Signed in = the panel cookie exists AND the page that finished is a
                // panel page other than the sign-in flow (the panel 303s to /admin after
                // a good sign-in).
                //
                // MEASURED ON THE DEVICE, NOT REASONED: without the two guards below
                // this looped. closeWebView() loads about:blank, about:blank finishes,
                // the cookie is still there, becomeReady() runs again, closeWebView()
                // runs again — and every pass re-enabled reader mode (a burst of
                // reader_mode_change events 40 ms apart in `dumpsys nfc`) and kept the
                // UI from ever going idle. So: only while the sign-in page is showing,
                // and only for a page on the panel's own origin.
                if (url == null || web.visibility != View.VISIBLE) return
                if (!url.startsWith("$base/")) return
                val onLoginFlow = url.startsWith("$base${HttpRelayServer.LOGIN_PATH}")
                if (!onLoginFlow && panelCookie() != null) {
                    CookieManager.getInstance().flush()
                    closeWebView()
                    becomeReady()
                }
            }
        }
    }

    private fun openWebView() {
        web.visibility = View.VISIBLE
        panel.visibility = View.GONE
        web.loadUrl("$base${HttpRelayServer.LOGIN_PATH}")
    }

    private fun closeWebView() {
        // Hidden FIRST, so the about:blank that follows cannot be mistaken for a
        // sign-in landing (see onPageFinished).
        web.visibility = View.GONE
        panel.visibility = View.VISIBLE
        web.stopLoading()
        web.loadUrl("about:blank")
    }

    private fun setUpBack() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(0) { if (!onBackFromWebView()) finish() }
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        if (!onBackFromWebView()) {
            @Suppress("DEPRECATION")
            super.onBackPressed()
        }
    }

    /** Back while the sign-in page is open closes it. Returns true when it did something. */
    private fun onBackFromWebView(): Boolean {
        if (web.visibility != View.VISIBLE) return false
        if (web.canGoBack()) web.goBack() else closeWebView()
        return true
    }

    private fun becomeReady() {
        armed = true
        show(State.READY, "Hold a blank plaque flat against the back of the phone and keep it still.", "")
        enableReader()
    }

    private fun signOut() {
        val s = server()
        show(State.SIGNED_OUT, "Signing out…", "")
        armed = false
        disableReader()
        Thread {
            val status = s.signOut()
            Log.i(TAG, "sign-out answered HTTP $status")
            runOnUiThread {
                CookieManager.getInstance().removeAllCookies(null)
                CookieManager.getInstance().flush()
                show(State.SIGNED_OUT, "Signed out. Sign in to encode plaques.", "")
            }
        }.start()
    }

    private fun saveServer() {
        val typed = serverField.text.toString()
        val normalised = try {
            HttpRelayServer.normaliseBase(typed, allowHttp = BuildConfig.DEBUG)
        } catch (e: IllegalArgumentException) {
            show(State.SIGNED_OUT, "That server address will not do: ${e.message}.", "")
            return
        }
        hideSoftInput()
        serverField.setText(normalised)
        if (normalised != base) {
            // A different server means a different panel; its cookie is not ours.
            CookieManager.getInstance().removeAllCookies(null)
            CookieManager.getInstance().flush()
        }
        base = normalised
        prefs().edit().putString(PREF_SERVER, base).apply()
        show(State.SIGNED_OUT, "Server set to $base. Sign in to encode plaques.", "")
    }

    private fun onPrimary() {
        when (state) {
            State.SIGNED_OUT -> openWebView()
            State.DONE, State.FAULT -> becomeReady()
            State.READY, State.WRITING -> Unit
        }
    }

    /**
     * "Check server": the signed-out probe (HttpRelayServer.probeOrigin). It sends no
     * cookie — explicitly a server built with `cookie = { null }` — so it cannot open a
     * round even while a panel session exists on this phone; it only asks the chain
     * whether the address and the Origin header are right.
     */
    private fun checkServer() {
        show(State.SIGNED_OUT, "Checking $base…", "")
        val s = HttpRelayServer(base, cookie = { null }, allowHttp = BuildConfig.DEBUG)
        Thread {
            val p = s.probeOrigin()
            Log.i(TAG, "server check: HTTP ${p.status} location=${p.location} originAccepted=${p.originAccepted}")
            runOnUiThread {
                show(State.SIGNED_OUT, p.describe(), "HTTP ${p.status}" + (if (p.location != null && p.status != -1) " → ${p.location}" else ""))
            }
        }.start()
    }

    // --- NFC -----------------------------------------------------------------------

    private fun enableReader() {
        val adapter = nfc
        if (adapter == null) {
            show(State.FAULT, "This phone has no NFC, so it cannot encode plaques.", "")
            return
        }
        if (!adapter.isEnabled) {
            show(State.FAULT, "NFC is switched off. Turn it on in the phone's settings, then come back.", "")
            return
        }
        if (readerOn) return
        readerOn = true
        // K4: NFC-A, no NDEF probing (the chip's NDEF file is exactly what the round
        // rewrites), no platform sound, default presence check.
        adapter.enableReaderMode(
            this,
            { tag -> onTag(tag) },
            NfcAdapter.FLAG_READER_NFC_A or NfcAdapter.FLAG_READER_SKIP_NDEF_CHECK or NfcAdapter.FLAG_READER_NO_PLATFORM_SOUNDS,
            null,
        )
    }

    /** Runs on the NFC reader thread, never on the main thread — the whole round lives here. */
    private fun onTag(tag: Tag) {
        if (!armed) return
        if (!busy.compareAndSet(false, true)) return
        armed = false
        runOnUiThread { show(State.WRITING, "Keep the plaque still.", "0/$EXPECTED_EXCHANGES · connecting") }
        Log.i(TAG, "round: tag in field, opening ISO-DEP")

        val outcome: Outcome = try {
            IsoDepChip.open(tag).use { chip ->
                Log.i(TAG, "round: connected, max transceive ${chip.maxTransceiveLength} bytes, timeout ${IsoDepChip.TRANSCEIVE_TIMEOUT_MS} ms")
                RelayLoop(server(), chip) { n, step ->
                    Log.i(TAG, "round: exchange $n after step $step")
                    runOnUiThread { show(State.WRITING, "Keep the plaque still.", "$n/$EXPECTED_EXCHANGES · $step") }
                }.run()
            }
        } catch (e: IOException) {
            Outcome.ChipError(0, e.message ?: e.javaClass.simpleName, abortSent = false)
        } finally {
            busy.set(false)
        }

        Log.i(TAG, "round: ended as ${outcome.javaClass.simpleName} after ${outcome.exchanges} exchanges" + faultWordForLog(outcome))
        runOnUiThread {
            val detailLine = "${outcome.exchanges}/$EXPECTED_EXCHANGES exchanges"
            when (outcome) {
                is Outcome.Completed -> show(State.DONE, Wording.forOutcome(outcome), detailLine)
                // The button re-arms the reader either way; its LABEL must not say
                // "try again" under a message that says not to (Wording.retryIsSafe).
                else -> show(
                    State.FAULT, Wording.forOutcome(outcome), detailLine,
                    primary = if (Wording.retryIsSafe(outcome)) "Try again" else "Next plaque",
                )
            }
        }
    }

    /** The fault WORD and the HTTP status are vocabulary, not secrets; nothing else from an outcome is logged. */
    private fun faultWordForLog(o: Outcome): String = when (o) {
        is Outcome.Faulted -> " (HTTP ${o.status} ${o.fault})"
        is Outcome.Refused -> " (HTTP ${o.status})"
        is Outcome.Completed -> if (o.trailingFault != null) " (trailing ${o.trailingFault})" else ""
        else -> ""
    }

    // --- rendering -----------------------------------------------------------------

    private fun show(s: State, text: String, detailText: String, primary: String? = null) {
        state = s
        message.text = text
        detail.text = detailText
        detail.visibility = if (detailText.isEmpty()) View.GONE else View.VISIBLE
        serverRow.visibility = if (s == State.SIGNED_OUT) View.VISIBLE else View.GONE
        signOutButton.visibility = if (s == State.SIGNED_OUT || s == State.WRITING) View.GONE else View.VISIBLE
        when (s) {
            State.SIGNED_OUT -> {
                stampWord("Signed out", color(R.color.line))
                headline.text = "Not signed in"
                primaryButton.text = "Sign in"
                primaryButton.visibility = View.VISIBLE
            }
            State.READY -> {
                stampWord("Ready", color(R.color.tappa_green))
                headline.text = "Hold the plaque"
                primaryButton.visibility = View.GONE
            }
            State.WRITING -> {
                stampWord("Writing", color(R.color.saffron))
                headline.text = "Encoding…"
                primaryButton.visibility = View.GONE
            }
            State.DONE -> {
                stampWord("Done", color(R.color.tappa_green))
                headline.text = "Plaque encoded"
                primaryButton.text = "Next plaque"
                primaryButton.visibility = View.VISIBLE
            }
            State.FAULT -> {
                stampWord("Fault", color(R.color.tomato))
                headline.text = "Not encoded"
                primaryButton.text = primary ?: "Try again"
                primaryButton.visibility = View.VISIBLE
            }
        }
    }

    /**
     * The stamp (tappa-brand): the WORD is always ink; the status colour is carried by
     * the 2dp border, the 1dp inner ring and a 10 % ground — never by the text.
     */
    private fun stampWord(word: String, tone: Int) {
        stamp.text = word.uppercase()
        stamp.setTextColor(color(R.color.ink))
        val outer = GradientDrawable().apply {
            setStroke(dp(2), tone)
            setColor(Color.argb(0x1a, Color.red(tone), Color.green(tone), Color.blue(tone)))
        }
        val ring = GradientDrawable().apply {
            setStroke(dp(1), tone)
            setColor(Color.TRANSPARENT)
        }
        val layers = LayerDrawable(arrayOf(outer, ring))
        layers.setLayerInset(1, dp(4), dp(4), dp(4), dp(4))
        stamp.background = layers
    }

    private fun color(id: Int): Int = getColor(id)

    private fun dp(v: Int): Int = (v * resources.displayMetrics.density + 0.5f).toInt()

    private fun hideSoftInput() {
        val imm = getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
        imm.hideSoftInputFromWindow(serverField.windowToken, 0)
    }

    private fun prefs() = getSharedPreferences("relay", Context.MODE_PRIVATE)

    companion object {
        private const val TAG = "TaptimeRelay"

        /** K5: the default server. Changeable on the screen; the Origin header follows it. */
        const val DEFAULT_SERVER = "https://taptime.mt"

        /** The panel's session cookie name — internal/adminauth/cookie.go, CookieName. Scoped Path=/admin there. */
        const val PANEL_COOKIE = "tappa_admin_session"

        /**
         * The number of exchanges a round costs on today's server (internal/encode,
         * len(roundSteps) = 11). Cosmetic — it only makes the progress line read
         * "3/11"; the loop takes its cue from `done`, never from this number.
         */
        const val EXPECTED_EXCHANGES = 11

        /** The ONLY thing written to SharedPreferences: the server address. Never a cookie, never a handle. */
        private const val PREF_SERVER = "server"
    }
}
