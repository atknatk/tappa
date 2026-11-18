package mt.taptime.app

import mt.taptime.app.wire.JsonDecoder
import mt.taptime.app.wire.JsonException

/**
 * The unit tests' JSON reader. org.json on this classpath is android.jar's STUB -- every call
 * throws "Method ... not mocked" (measured; unitTests.isReturnDefaultValues = false) -- and no
 * JSON library is a dependency of this build (Q36), so the tests read the vectors file and the
 * fake server's bodies with this small RFC 8259 parser. It yields exactly the shape
 * [JsonDecoder] promises and OrgJsonDecoder produces on the phone: String, Boolean, Long or
 * Double, Map (insertion order), List, and JSON null as Kotlin null.
 *
 * Test-only: the app's own decoder is the platform's org.json.
 */
object MiniJson : JsonDecoder {
    override fun decodeObject(text: String): Map<String, Any?> {
        val p = Parser(text)
        val v = p.value()
        p.ws()
        if (!p.end()) throw JsonException("text after the JSON value")
        @Suppress("UNCHECKED_CAST")
        return v as? Map<String, Any?> ?: throw JsonException("not a JSON object")
    }

    private class Parser(val s: String) {
        var i = 0
        fun end() = i >= s.length
        fun ws() {
            while (i < s.length && s[i] in " \t\r\n") i++
        }
        private fun fail(what: String): Nothing = throw JsonException("$what at $i")
        private fun expect(c: Char) {
            ws()
            if (i >= s.length || s[i] != c) fail("expected '$c'")
            i++
        }

        fun value(): Any? {
            ws()
            if (end()) fail("unexpected end")
            return when (val c = s[i]) {
                '{' -> obj()
                '[' -> arr()
                '"' -> str()
                't' -> lit("true", true)
                'f' -> lit("false", false)
                'n' -> lit("null", null)
                else -> if (c == '-' || c in '0'..'9') num() else fail("unexpected '$c'")
            }
        }

        private fun lit(word: String, v: Any?): Any? {
            if (!s.startsWith(word, i)) fail("bad literal")
            i += word.length
            return v
        }

        private fun obj(): Map<String, Any?> {
            expect('{')
            val m = LinkedHashMap<String, Any?>()
            ws()
            if (s.getOrNull(i) == '}') {
                i++
                return m
            }
            while (true) {
                ws()
                val k = str()
                expect(':')
                if (m.containsKey(k)) fail("duplicate key")
                m[k] = value()
                ws()
                when (s.getOrNull(i)) {
                    ',' -> i++
                    '}' -> {
                        i++
                        return m
                    }
                    else -> fail("expected ',' or '}'")
                }
            }
        }

        private fun arr(): List<Any?> {
            expect('[')
            val l = ArrayList<Any?>()
            ws()
            if (s.getOrNull(i) == ']') {
                i++
                return l
            }
            while (true) {
                l += value()
                ws()
                when (s.getOrNull(i)) {
                    ',' -> i++
                    ']' -> {
                        i++
                        return l
                    }
                    else -> fail("expected ',' or ']'")
                }
            }
        }

        private fun str(): String {
            if (s.getOrNull(i) != '"') fail("expected a string")
            i++
            val out = StringBuilder()
            while (true) {
                if (end()) fail("unterminated string")
                val c = s[i++]
                when {
                    c == '"' -> return out.toString()
                    c == '\\' -> {
                        if (end()) fail("bad escape")
                        when (val e = s[i++]) {
                            '"' -> out.append('"')
                            '\\' -> out.append('\\')
                            '/' -> out.append('/')
                            'b' -> out.append('\b')
                            'f' -> out.append('\u000c')
                            'n' -> out.append('\n')
                            'r' -> out.append('\r')
                            't' -> out.append('\t')
                            'u' -> {
                                if (i + 4 > s.length) fail("bad unicode escape")
                                out.append(s.substring(i, i + 4).toInt(16).toChar())
                                i += 4
                            }
                            else -> fail("bad escape '$e'")
                        }
                    }
                    c.code < 0x20 -> fail("control character in string")
                    else -> out.append(c)
                }
            }
        }

        private fun num(): Any {
            val start = i
            if (s[i] == '-') i++
            while (i < s.length && (s[i].isDigit() || s[i] in ".eE+-")) i++
            val t = s.substring(start, i)
            return t.toLongOrNull() ?: t.toDoubleOrNull() ?: fail("bad number")
        }
    }
}
