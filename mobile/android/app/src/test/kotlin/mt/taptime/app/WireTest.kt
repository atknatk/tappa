package mt.taptime.app

import mt.taptime.app.wire.Bodies
import mt.taptime.app.wire.Brand
import mt.taptime.app.wire.JsonException
import mt.taptime.app.wire.JsonObjectWriter
import mt.taptime.app.wire.Replies
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/**
 * Request bodies byte for byte, and the reply shapes of M16-07's contract table read into the
 * app's types. The reply strings are the Go side's own pins (internal/handler/app_test.go,
 * the response-shape test), with real values where this side validates them.
 */
class WireTest {
    private val code = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn-_0"
    private val point = ByteArray(65) { if (it == 0) 4 else it.toByte() }

    @Test
    fun theBodiesAreTheContractsFieldsInItsOrder() {
        assertEquals("""{"code":"$code"}""", String(Bodies.lookup(code)))
        val key = mt.taptime.app.wire.B64Url.encode(point)
        assertEquals("""{"code":"$code","consent":true,"publicKey":"$key"}""", String(Bodies.bind(code, point)))
        assertEquals(
            """{"code":"$code","publicKey":"$key","platform":"android","sun":"tag=04A1&ctr=000011&cmac=00AA"}""",
            String(Bodies.complete(code, point, "tag=04A1&ctr=000011&cmac=00AA")),
        )
    }

    @Test
    fun theWriterEscapesWhatJsonRequiresAndKeepsUtf8() {
        val w = JsonObjectWriter().put("s", "a\"b\\c\nd\u0001é").put("n", null).put("b", false).put("i", 7).put("d", 35.9)
        val text = String(w.bytes(), Charsets.UTF_8)
        assertEquals("{\"s\":\"a\\\"b\\\\c\\nd\\u0001é\",\"b\":false,\"i\":7,\"d\":35.9}", text)
        // It reads back to the same values: what is signed is what the server decodes.
        val back = MiniJson.decodeObject(text)
        assertEquals("a\"b\\c\nd\u0001é", back["s"])
        assertFalse("a null field is omitted, not sent as null", back.containsKey("n"))
        assertTrue("é is UTF-8, two bytes", w.bytes().size == text.length + 1)
    }

    @Test
    fun theWriterRefusesWhatJsonCannotCarry() {
        try {
            JsonObjectWriter().put("d", Double.NaN).bytes()
            fail("NaN was written")
        } catch (_: IllegalArgumentException) {
        }
    }

    @Test
    fun aLookupReplyReadsWithItsBrand() {
        val r = Replies.lookup(MiniJson.decodeObject(
            """{"employeeName":"E","businessName":"B","locationName":"L","wifiSsid":"W","secondDevice":true,""" +
                """"brand":{"name":"N","logo":{"sha256":"ab","width":1,"height":2},"accent":"1F5C41"}}"""))
        assertEquals("E", r.employeeName)
        assertEquals("B", r.businessName)
        assertEquals("L", r.locationName)
        assertEquals("W", r.wifiSsid)
        assertTrue(r.secondDevice)
        assertEquals(Brand("N", Brand.Logo("ab", 1, 2), "1F5C41"), r.brand)
    }

    @Test
    fun aLookupWithoutTheOptionalsIsTaptimesLook() {
        val r = Replies.lookup(MiniJson.decodeObject("""{"employeeName":"E","businessName":"B","secondDevice":false,"brand":{}}"""))
        assertNull(r.locationName)
        assertNull(r.wifiSsid)
        assertEquals(Brand.TAPTIME, r.brand)
    }

    @Test
    fun aFieldTheServerAddsLaterIsIgnored() {
        val r = Replies.me(MiniJson.decodeObject("""{"employeeName":"E","brand":{},"direction":"in","later":1}"""))
        assertEquals("in", r.direction)
    }

    @Test
    fun aMissingOrMistypedRequiredFieldIsRefused() {
        for (bad in listOf(
            """{"businessName":"B","secondDevice":false,"brand":{}}""",
            """{"employeeName":1,"businessName":"B","secondDevice":false,"brand":{}}""",
            """{"employeeName":"E","businessName":"B","secondDevice":"yes","brand":{}}""",
            """{"employeeName":"E","businessName":"B","secondDevice":false,"brand":{"logo":{"sha256":"a","width":1.5,"height":2}}}""",
        )) {
            try {
                Replies.lookup(MiniJson.decodeObject(bad))
                fail("read: $bad")
            } catch (_: JsonException) {
            }
        }
    }

    @Test
    fun bindAndCompleteRepliesMustCarryValuesTheSignedTextAccepts() {
        val id = "6f1c2a9e-3b4d-4e5f-8a7b-0c1d2e3f4a5b"
        val challenge = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
        assertEquals(id, Replies.bind(MiniJson.decodeObject("""{"activationId":"$id","challenge":"$challenge"}""")).activationId)
        assertEquals(id, Replies.complete(MiniJson.decodeObject("""{"deviceId":"$id","employeeName":"E"}""")).deviceId)
        for (bad in listOf(
            """{"activationId":"I","challenge":"$challenge"}""",
            """{"activationId":"${id.uppercase()}","challenge":"$challenge"}""",
            """{"activationId":"$id","challenge":"C"}""",
        )) {
            try {
                Replies.bind(MiniJson.decodeObject(bad))
                fail("read: $bad")
            } catch (_: JsonException) {
            }
        }
    }

    // M16-13: the tap's bodies, in the contract table's order, and its two replies (the Go side's
    // own pins, internal/handler/app_test.go's response-shape test).
    @Test
    fun theTapBodiesAreTheContractsFieldsInItsOrder() {
        assertEquals("""{"sun":"tag=04A1&ctr=000011&cmac=00AA"}""", String(Bodies.tapStart("tag=04A1&ctr=000011&cmac=00AA")))
        // M16-22: a live QR's query, written as it came (the JSON writer escapes nothing a base64url
        // token or '=' could need).
        assertEquals("""{"liveQR":"t=AQ-_09azAZ"}""", String(Bodies.tapStartLive("t=AQ-_09azAZ")))
        assertEquals("""{"context":"X","want":"in","lat":35.9,"lng":14.5,"mockLocation":true}""",
            String(Bodies.tap("X", "in", 35.9, 14.5, mockLocation = true)))
        assertEquals("mock false is absent", """{"context":"X","want":"out","lat":35.9,"lng":14.5}""",
            String(Bodies.tap("X", "out", 35.9, 14.5, mockLocation = false)))
        assertEquals("no fix: no coordinate and no mock flag", """{"context":"X"}""", String(Bodies.tap("X", null, null, null, mockLocation = true)))
        assertEquals("half a fix is no fix", """{"context":"X"}""", String(Bodies.tap("X", "", 35.9, null, mockLocation = true)))
        assertEquals("an unknown want is not sent", """{"context":"X"}""", String(Bodies.tap("X", "both", null, null, mockLocation = false)))
    }

    @Test
    fun theTapRepliesReadTheContractsShapes() {
        val start = Replies.tapStart(MiniJson.decodeObject("""{"employeeName":"E","locationName":"L","brand":{},"direction":"in","qr":false,"context":"X"}"""))
        assertEquals("E", start.employeeName)
        assertEquals("L", start.locationName)
        assertEquals(Brand.TAPTIME, start.brand)
        assertEquals("in", start.direction)
        assertFalse(start.qr)
        assertEquals("X", start.context)
        // Another business's plaque: the server writes an empty venue and no brand.
        val foreign = Replies.tapStart(MiniJson.decodeObject("""{"employeeName":"E","locationName":"","brand":{},"direction":"","qr":true,"context":"X"}"""))
        assertNull(foreign.locationName)
        assertEquals("", foreign.direction)
        assertTrue(foreign.qr)
        assertEquals("a direction this app does not know is the neutral Tap", "",
            Replies.tapStart(MiniJson.decodeObject("""{"employeeName":"E","brand":{},"direction":"up","qr":false,"context":"X"}""")).direction)
        try {
            Replies.tapStart(MiniJson.decodeObject("""{"employeeName":"E","brand":{},"direction":"in","qr":false,"context":""}"""))
            fail("an empty context cannot be pressed with")
        } catch (_: JsonException) {
        }

        val tap = Replies.tap(MiniJson.decodeObject("""{"outcome":"recorded","verdict":"ok","direction":"out","venue":"V","at":"09:00","businessType":"bar"}"""))
        assertEquals("ok", tap.verdict)
        assertEquals("out", tap.direction)
        assertEquals("V", tap.venue)
        assertEquals("09:00", tap.at)
        assertEquals("bar", tap.businessType)
        assertFalse(tap.practice)
        assertTrue(Replies.tap(MiniJson.decodeObject("""{"outcome":"recorded","verdict":"ok","direction":"","venue":"V","at":"09:00","businessType":"bar","practice":true}""")).practice)
    }
}
