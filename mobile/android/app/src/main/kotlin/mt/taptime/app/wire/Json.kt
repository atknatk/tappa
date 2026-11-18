package mt.taptime.app.wire

// JSON at the wire's edge, in two halves.
//
// WRITING is done here, by hand: every request body is a flat object of strings, booleans and
// numbers in a fixed key order, and the body is SIGNED -- its raw bytes are hashed into the
// canonical text -- so the app wants to know exactly which bytes it sends. The writer is small,
// deterministic and runs on the JVM, so its output is pinned byte for byte in a unit test.
//
// READING goes through [JsonDecoder]. On the phone that is the platform's org.json
// ([OrgJsonDecoder], ADR 0045 §2.2: no JSON library). org.json in the unit-test classpath is
// the android.jar STUB, which throws on every call (testOptions.unitTests.isReturnDefaultValues
// = false, measured: "Method ... not mocked"), so the JVM tests hand the client a decoder of
// their own that yields the same shape -- a Map of String to String / Boolean / Number / Map /
// List, JSON null as Kotlin null. Every mapping from that shape to the app's types lives here,
// on the JVM-tested side; the org.json adapter only converts.

/** Decodes one JSON object to a map; throws [JsonException] on anything else. */
fun interface JsonDecoder {
    fun decodeObject(text: String): Map<String, Any?>
}

class JsonException(message: String) : Exception(message)

/** A flat JSON object, keys in insertion order. Values: String, Boolean, Int, Long, Double (finite). */
class JsonObjectWriter {
    private val fields = mutableListOf<Pair<String, Any>>()

    fun put(key: String, value: Any?): JsonObjectWriter {
        if (value != null) fields += key to value
        return this
    }

    fun bytes(): ByteArray = toString().toByteArray(Charsets.UTF_8)

    override fun toString(): String {
        val out = StringBuilder("{")
        fields.forEachIndexed { i, (k, v) ->
            if (i > 0) out.append(',')
            quote(out, k)
            out.append(':')
            when (v) {
                is String -> quote(out, v)
                is Boolean, is Int, is Long -> out.append(v.toString())
                is Double -> {
                    require(v.isFinite()) { "JSON has no NaN or infinity" }
                    out.append(v.toString())
                }
                else -> throw IllegalArgumentException("unsupported JSON value type ${v::class.java.simpleName}")
            }
        }
        return out.append('}').toString()
    }

    private fun quote(out: StringBuilder, s: String) {
        out.append('"')
        for (c in s) {
            when {
                c == '"' -> out.append("\\\"")
                c == '\\' -> out.append("\\\\")
                c == '\n' -> out.append("\\n")
                c == '\r' -> out.append("\\r")
                c == '\t' -> out.append("\\t")
                c.code < 0x20 -> out.append("\\u").append(String.format("%04x", c.code))
                else -> out.append(c)
            }
        }
        out.append('"')
    }
}

/** Typed reads over a decoded object. A missing or mistyped REQUIRED field is a [JsonException]. */
internal class Fields(private val m: Map<String, Any?>) {
    fun string(k: String): String = m[k] as? String ?: throw JsonException("field $k: want a string")
    fun optString(k: String): String? = when (val v = m[k]) {
        null -> null
        is String -> v
        else -> throw JsonException("field $k: want a string")
    }
    fun bool(k: String): Boolean = m[k] as? Boolean ?: throw JsonException("field $k: want a boolean")
    fun optBool(k: String): Boolean = when (val v = m[k]) {
        null -> false
        is Boolean -> v
        else -> throw JsonException("field $k: want a boolean")
    }
    fun int(k: String): Int {
        val n = m[k] as? Number ?: throw JsonException("field $k: want a number")
        val d = n.toDouble()
        if (d != Math.floor(d) || d < Int.MIN_VALUE || d > Int.MAX_VALUE) throw JsonException("field $k: want an integer")
        return d.toInt()
    }
    @Suppress("UNCHECKED_CAST")
    fun optObject(k: String): Fields? = when (val v = m[k]) {
        null -> null
        is Map<*, *> -> Fields(v as Map<String, Any?>)
        else -> throw JsonException("field $k: want an object")
    }
}
