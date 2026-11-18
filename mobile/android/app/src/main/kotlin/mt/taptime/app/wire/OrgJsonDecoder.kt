package mt.taptime.app.wire

import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject
import org.json.JSONTokener

// The phone's decoder: the platform's org.json, converted to the plain shape [JsonDecoder]
// promises. The conversion is the whole of this file on purpose -- org.json is a stub on the
// unit-test classpath, so every rule about WHAT the fields mean lives in Wire.kt, which the JVM
// tests run. Two org.json habits are neutralised here rather than at each read:
//   * JSON null is JSONObject.NULL, and optString() turns it into the four letters "null";
//     here it becomes Kotlin null.
//   * the top level must be an object; a bare value or trailing text is refused.
// JSONException carries a fragment of the input in its message; it is replaced by a fixed one.
object OrgJsonDecoder : JsonDecoder {
    override fun decodeObject(text: String): Map<String, Any?> {
        try {
            val tokener = JSONTokener(text)
            val value = tokener.nextValue()
            if (value !is JSONObject) throw JsonException("not a JSON object")
            if (tokener.more() && tokener.nextClean() != 0.toChar()) throw JsonException("text after the JSON object")
            return convert(value)
        } catch (_: JSONException) {
            throw JsonException("malformed JSON")
        }
    }

    private fun convert(o: JSONObject): Map<String, Any?> {
        val out = LinkedHashMap<String, Any?>()
        val keys = o.keys()
        while (keys.hasNext()) {
            val k = keys.next()
            out[k] = value(o.get(k))
        }
        return out
    }

    private fun value(v: Any?): Any? = when (v) {
        null, JSONObject.NULL -> null
        is JSONObject -> convert(v)
        is JSONArray -> (0 until v.length()).map { value(v.get(it)) }
        else -> v
    }
}
