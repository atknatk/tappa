import CoreText
import SwiftUI
import XCTest
@testable import Taptime

// M16-47: the lock-screen card and the home-screen widget's faces, drawn in-process with
// ImageRenderer (iOS 16+). The system draws them in its own process on a phone; this proves the
// faces lay out and gives the review an image: with TAPTIME_RENDER_DIR set (xcodebuild passes
// TEST_RUNNER_TAPTIME_RENDER_DIR through), each face is written there as a PNG.
final class LiveViewsRenderTests: XCTestCase {
    /// 4:12:08 ago, the design's count.
    private let inSince = Date(timeIntervalSinceNow: -15_128)

    /// What the extension's UIAppFonts does for it, done here for the app's process: the three latin
    /// files the faces name (the app bundle carries the same bytes).
    override class func setUp() {
        super.setUp()
        for f in LiveType.files {
            guard let u = Bundle.main.url(forResource: f, withExtension: "woff2") else { continue }
            CTFontManagerRegisterFontsForURL(u as CFURL, .process, nil)
        }
    }

    /// The names the faces ask for exist once the files are registered -- the woff2 registers, and
    /// every name is spelled as the font spells it. A wrong name would silently draw the system face.
    func testTheNamedFacesResolveFromTheBundledFiles() {
        for name in [LiveType.mono400, LiveType.mono700, LiveType.display400, LiveType.display500, LiveType.display700] {
            XCTAssertNotNil(UIFont(name: name, size: 12), name)
        }
    }

    @MainActor
    private func render<V: View>(_ name: String, _ view: V, width: CGFloat, height: CGFloat? = nil) throws {
        guard #available(iOS 16.0, *) else { throw XCTSkip("ImageRenderer is iOS 16") }
        let r = ImageRenderer(content: view.frame(width: width, height: height))
        r.scale = 3
        let image = try XCTUnwrap(r.uiImage, name)
        XCTAssertGreaterThan(image.size.width, 0, name)
        XCTAssertGreaterThan(image.size.height, 0, name)
        if let dir = ProcessInfo.processInfo.environment["TAPTIME_RENDER_DIR"], let png = image.pngData() {
            try png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
        }
    }

    @MainActor
    func testTheLockCardDraws() throws {
        guard #available(iOS 16.0, *) else { throw XCTSkip("LockCard is iOS 16") }
        let backdrop = LiveLook.green
        let card = { (end: Date?, endLocal: String?, stale: Bool) in
            LockCard(inSince: self.inSince, inSinceLocal: "17:54", shiftEndsAt: end, shiftEndsAtLocal: endLocal, until: nil, stale: stale)
                .background(RoundedRectangle(cornerRadius: 24).fill(LiveLook.card))
                .padding(16).background(backdrop)
        }
        let end = inSince.addingTimeInterval(8 * 3600 + 6 * 60)
        try render("lock-shift", card(end, "02:00", false), width: 393)
        try render("lock-noshift", card(nil, nil, false), width: 393)
        try render("lock-stale", card(end, "02:00", true), width: 393)
    }

    /// [G47-e]: no shift line, and no bar, without the server's shiftEndsAtLocal.
    func testNoShiftLineWithoutTheShiftsEnd() throws {
        guard #available(iOS 16.0, *) else { throw XCTSkip("LockCard is iOS 16") }
        let end = inSince.addingTimeInterval(3600 * 8)
        XCTAssertTrue(LockCard(inSince: inSince, inSinceLocal: "17:54", shiftEndsAt: end, shiftEndsAtLocal: "02:00", until: nil, stale: false).showsShift)
        XCTAssertFalse(LockCard(inSince: inSince, inSinceLocal: "17:54", shiftEndsAt: end, shiftEndsAtLocal: nil, until: nil, stale: false).showsShift)
        XCTAssertFalse(LockCard(inSince: inSince, inSinceLocal: "17:54", shiftEndsAt: nil, shiftEndsAtLocal: "02:00", until: nil, stale: false).showsShift)
        // A shift that ended before the check-in has no bar to draw (a range the other way would trap).
        XCTAssertFalse(LockCard(inSince: inSince, inSinceLocal: "17:54", shiftEndsAt: inSince.addingTimeInterval(-60), shiftEndsAtLocal: "02:00",
                                until: nil, stale: false).showsShift)
    }

    @MainActor
    func testTheSmallWidgetFacesDraw() throws {
        let now = Date()
        let faces: [(String, PresenceShare)] = [
            ("widget-in", PresenceShare(isIn: true, inSince: inSince, inSinceLocal: "17:54", showUntil: now.addingTimeInterval(3600))),
            ("widget-out", PresenceShare(isIn: false)),
            ("widget-unknown", .none),
        ]
        for (name, share) in faces {
            let face = SmallWidgetFace(share: share, now: now)
                .padding(14)
                .background(RoundedRectangle(cornerRadius: 24).fill(SmallWidgetFace.ground(share.face(at: now))))
                .padding(8).background(LiveLook.green)
            try render(name, face, width: 174, height: 174)
        }
    }
}
