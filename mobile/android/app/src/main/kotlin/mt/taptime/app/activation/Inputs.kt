package mt.taptime.app.activation

import java.net.URI
import java.net.URISyntaxException

// What the person and the plaque hand the app, read before anything is sent.

/**
 * The invite code from what was pasted. Q40: the invitation link (`<base>/activate?code=…`)
 * opens the browser wizard, not the app, so the app's first screen takes the CODE -- and what a
 * person can copy out of a message is the whole link. Both are accepted: the bare value, or the
 * `code` parameter of a link that is THIS server's activation link -- the base URL's scheme and
 * host, the path exactly `/activate` (security review, 2026-11-18; the iOS twin's rule). A
 * look-alike link of another host never has its code sent here. The code is 43 unpadded
 * base64url characters (internal/invite, codeLen); anything else is refused without a request.
 * The value is held in memory for the activation and never stored or logged.
 */
object InviteCode {
    const val PATH = "/activate"
    private const val LEN = 43

    fun from(pasted: String, baseUrl: String): String? {
        val s = pasted.trim()
        if (looksLikeCode(s)) return s
        val uri = try {
            URI(s)
        } catch (_: URISyntaxException) {
            return null
        }
        val base = try {
            URI(baseUrl.trim())
        } catch (_: URISyntaxException) {
            return null
        }
        if (uri.scheme == null || !uri.scheme.equals(base.scheme, ignoreCase = true)) return null
        if (uri.host == null || !uri.host.equals(base.host, ignoreCase = true) || uri.rawUserInfo != null) return null
        // The PORT too (M16-12 hand-over (2)): PlaqueRead compares it, and the two readers of
        // "this server's URL" must agree -- https://taptime.mt:8443 is another server.
        if (effectivePort(uri) != effectivePort(base)) return null
        if (uri.rawPath != PATH) return null
        val query = uri.rawQuery ?: return null
        for (pair in query.split('&')) {
            val eq = pair.indexOf('=')
            if (eq > 0 && pair.substring(0, eq) == "code") {
                val v = pair.substring(eq + 1)
                return if (looksLikeCode(v)) v else null
            }
        }
        return null
    }

    private fun looksLikeCode(s: String): Boolean =
        s.length == LEN && s.all { it in 'A'..'Z' || it in 'a'..'z' || it in '0'..'9' || it == '-' || it == '_' }
}

/**
 * The SUN query of a plaque read: the `tag=…&ctr=…&cmac=…` of the URL the chip wrote, exactly as
 * written (the server parses and verifies it -- sun.Parse, sun.Verify; the app judges nothing
 * about the chip, ADR 0045 §2.1).
 *
 * The URL must be THIS app's server's plaque route, `<base>/t`: same scheme, host and port, path
 * exactly `/t`. Another tag's URL is not sent anywhere -- a stranger's NFC sticker must not make
 * the app post its contents to our server.
 */
object PlaqueRead {
    const val PATH = "/t"

    fun sunQuery(url: String, baseUrl: String): String? = routeQuery(url, baseUrl, PATH)

    /**
     * The URL "Open in browser" hands the browser: REBUILT as this server's `<base>/t?` + the verified
     * query, never the URL as it arrived (the iOS twin's rule). Whatever the intent carried besides
     * -- the host's case, an explicit default port -- is not passed on; a URL that is not this
     * server's plaque route yields none.
     */
    fun browserUrl(url: String, baseUrl: String): String? =
        sunQuery(url, baseUrl)?.let { baseUrl.trimEnd('/') + PATH + "?" + it }

}

/**
 * A venue screen's LIVE QR (M16-22, ADR 0045 §2.5): the camera reads the screen's rotating code,
 * `<base>/q?t=<token>`, and the App Link opens the app with it. The query goes to tap/start as
 * `liveQR`, exactly as written -- the server opens the token (GET /q's reader) and the app judges
 * nothing about it, as it judges nothing about a chip.
 *
 * The same origin rule as [PlaqueRead] (scheme, host, port, the exact path `/q`, no user, no
 * fragment): a look-alike host's code is never sent. The token is a ~20-second credential: it is
 * held in memory only and never printed (§7).
 */
object LiveQrRead {
    const val PATH = "/q"

    fun query(url: String, baseUrl: String): String? = routeQuery(url, baseUrl, PATH)

    /** "Open in browser" for a live QR: `<base>/q?` + the verified query, never the URL as it arrived. */
    fun browserUrl(url: String, baseUrl: String): String? =
        query(url, baseUrl)?.let { baseUrl.trimEnd('/') + PATH + "?" + it }
}

/**
 * What opened the tap screen: a plaque (NFC, or its printed static QR -- the same `<base>/t`
 * route, the server tells them apart) or a venue screen's live QR (`<base>/q`). [query] is sent to
 * tap/start and nowhere else; neither is ever printed (the SUN query carries the chip's CMAC, the
 * live query a live token).
 */
sealed interface TapLink {
    val query: String

    /** `<base>/t?` + the query: tap/start's `sun`. */
    class Plaque(override val query: String) : TapLink {
        override fun toString() = "Plaque(query=<redacted>)"
    }

    /** `<base>/q?` + the query: tap/start's `liveQR`. */
    class LiveQr(override val query: String) : TapLink {
        override fun toString() = "LiveQr(query=<redacted>)"
    }

    companion object {
        /** This server's plaque or live QR URL, or null for anything else (it is sent nowhere). */
        fun of(url: String, baseUrl: String): TapLink? =
            PlaqueRead.sunQuery(url, baseUrl)?.let(::Plaque) ?: LiveQrRead.query(url, baseUrl)?.let(::LiveQr)

        /** The URL "Open in browser" hands the browser, rebuilt from the base (the iOS twin's rule). */
        fun browserUrl(url: String, baseUrl: String): String? =
            PlaqueRead.browserUrl(url, baseUrl) ?: LiveQrRead.browserUrl(url, baseUrl)
    }
}

/**
 * The query of [url] when it is THIS server's [path]: same scheme, host and port as [baseUrl], the
 * path exactly [path], no user, no fragment, a non-empty query. The one origin check of the two
 * readers ([PlaqueRead], [LiveQrRead]).
 */
private fun routeQuery(url: String, baseUrl: String, path: String): String? {
    val read = parseAbsolute(url) ?: return null
    val base = parseAbsolute(baseUrl) ?: return null
    if (!read.scheme.equals(base.scheme, ignoreCase = true)) return null
    if (!read.host.equals(base.host, ignoreCase = true)) return null
    if (effectivePort(read) != effectivePort(base)) return null
    if (read.rawPath != path) return null
    if (read.rawFragment != null) return null
    return read.rawQuery?.takeIf { it.isNotEmpty() }
}

private fun parseAbsolute(s: String): URI? = try {
    URI(s.trim()).takeIf { it.isAbsolute && it.host != null && it.rawUserInfo == null }
} catch (_: URISyntaxException) {
    null
}

/** The URI's port, the scheme's default when none is written: what both readers compare. */
internal fun effectivePort(u: URI): Int = when {
    u.port != -1 -> u.port
    u.scheme.equals("https", ignoreCase = true) -> 443
    else -> 80
}
