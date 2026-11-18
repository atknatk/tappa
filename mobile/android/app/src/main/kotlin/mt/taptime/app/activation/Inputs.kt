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

    fun sunQuery(url: String, baseUrl: String): String? {
        val read = parse(url) ?: return null
        val base = parse(baseUrl) ?: return null
        if (!read.scheme.equals(base.scheme, ignoreCase = true)) return null
        if (!read.host.equals(base.host, ignoreCase = true)) return null
        if (effectivePort(read) != effectivePort(base)) return null
        if (read.rawPath != PATH) return null
        if (read.rawFragment != null) return null
        return read.rawQuery?.takeIf { it.isNotEmpty() }
    }

    /**
     * The URL "Open in browser" hands the browser: REBUILT as this server's `<base>/t?` + the verified
     * query, never the URL as it arrived (the iOS twin's rule). Whatever the intent carried besides
     * -- the host's case, an explicit default port -- is not passed on; a URL that is not this
     * server's plaque route yields none.
     */
    fun browserUrl(url: String, baseUrl: String): String? =
        sunQuery(url, baseUrl)?.let { baseUrl.trimEnd('/') + PATH + "?" + it }

    private fun parse(s: String): URI? = try {
        URI(s.trim()).takeIf { it.isAbsolute && it.host != null && it.rawUserInfo == null }
    } catch (_: URISyntaxException) {
        null
    }

}

/** The URI's port, the scheme's default when none is written: what both readers compare. */
internal fun effectivePort(u: URI): Int = when {
    u.port != -1 -> u.port
    u.scheme.equals("https", ignoreCase = true) -> 443
    else -> 80
}
