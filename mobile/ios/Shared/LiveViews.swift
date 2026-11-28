import SwiftUI

// THE COUNTER'S FACES (M16-47; the design's ios/Lock screen). Compiled into BOTH targets: the
// extension draws them on the lock screen, in the Dynamic Island and on the home screen; the app
// compiles them only so that its tests can render them (LiveViewsRenderTests). They take instants
// and the server's "HH:MM" strings and nothing else -- the same set the App Group carries.
//
// THE COUNT IS THE SYSTEM'S. Every running number is Text(timerInterval:) / Text(_:style: .timer)
// and the bar is ProgressView(timerInterval:): the system advances them, so the widget needs no
// timeline entry per second and the app runs nothing in the background (§4.2).
//
// THE TYPE IS NAMED, NOT HANDED OVER (measured 2026-11-24 in the iOS 18.6 simulator). The app's
// Typeface builds a CTFont from the woff2 bytes without registering it; a widget's or a Live
// Activity's views are archived in the extension and drawn by the system's own process, and that
// archive cannot carry such a font: chronod logged "Failed to return view entry from archive ...
// SwiftUI.CodablePlatformFont" for every region and drew the activity EMPTY. So the extension
// declares its three latin files in UIAppFonts (TaptimeWidgets/Info.plist) and the faces ask for
// them by PostScript name; Font.custom falls back to the system face when a name does not
// resolve, so a missing font costs the look, never the count. The latin-ext subsets share the
// latin ones' names and cannot be registered beside them: a Turkish or Maltese letter outside
// latin is drawn by the system's cascade.
enum LiveType {
    static let mono400 = "IBMPlexMono-Regular"
    static let mono700 = "IBMPlexMono-Bold"
    /// Space Grotesk is variable; these are its named instances.
    static let display400 = "SpaceGrotesk-Light_Regular"
    static let display500 = "SpaceGrotesk-Light_Medium"
    static let display700 = "SpaceGrotesk-Light_Bold"
    /// The files UIAppFonts lists (and the tests register in-process).
    static let files = ["ibm-plex-mono-v20-latin-400", "ibm-plex-mono-v20-latin-700", "space-grotesk-v22-latin-wght"]

    enum Weight { case regular, medium, bold }

    static func mono(_ size: CGFloat, bold: Bool = false) -> Font {
        .custom(bold ? mono700 : mono400, fixedSize: size)
    }

    static func display(_ size: CGFloat, _ weight: Weight = .regular) -> Font {
        switch weight {
        case .regular: return .custom(display400, fixedSize: size)
        case .medium: return .custom(display500, fixedSize: size)
        case .bold: return .custom(display700, fixedSize: size)
        }
    }
}

enum LiveLook {
    static let ink = Color(red: 0x15 / 255, green: 0x22 / 255, blue: 0x19 / 255)
    static let paper = Color(red: 0xFF / 255, green: 0xFD / 255, blue: 0xF4 / 255)
    static let green = Color(red: 0x1F / 255, green: 0x5C / 255, blue: 0x41 / 255)
    static let greenLite = Color(red: 0xE1 / 255, green: 0xED / 255, blue: 0xE6 / 255)
    static let line = Color(red: 0xC9 / 255, green: 0xD2 / 255, blue: 0xC8 / 255)

    /// The lock-screen card's ground: ink at 82 % (the design's rgba(21,34,25,0.82)).
    static let card = ink.opacity(0.82)

    /// A running count from `from`; it stops at `until` when that is later (the activity's showUntil).
    @ViewBuilder
    static func count(from: Date, until: Date?) -> some View {
        if #available(iOS 16.0, *), let u = until, u > from {
            Text(timerInterval: from...u, countsDown: false)
        } else {
            Text(from, style: .timer)
        }
    }
}

/// The small dot + "taptime" pair every face opens with.
struct LiveMark: View {
    var dot: Color = LiveLook.greenLite
    var word: Color = LiveLook.greenLite
    var size: CGFloat = 12

    var body: some View {
        HStack(spacing: 6) {
            Circle().fill(dot).frame(width: 8, height: 8)
            Text(LiveCopy.wordmark).font(LiveType.display(size, .bold)).foregroundColor(word)
        }
        .accessibilityElement(children: .combine)
    }
}

// MARK: - The lock screen (Live Activity)

@available(iOS 16.0, *)
struct LockCard: View {
    let inSince: Date
    let inSinceLocal: String
    let shiftEndsAt: Date?
    let shiftEndsAtLocal: String?
    /// The activity's staleDate (showUntil): the count stops there.
    let until: Date?
    /// context.isStale: past showUntil the card no longer counts (a browser clock-out never
    /// reaches the phone) and says to open the app instead (M16-47).
    let stale: Bool

    /// The shift line and its bar: only when the owner marked shifts real and the server sent the
    /// end's wall clock (M16-47 [G47-e]).
    var showsShift: Bool {
        guard let end = shiftEndsAt, shiftEndsAtLocal != nil else { return false }
        return end > inSince
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                HStack(spacing: 8) {
                    Circle().fill(LiveLook.greenLite).frame(width: 7, height: 7)
                    Text(LiveCopy.clockedIn).font(LiveType.mono(11, bold: true)).kerning(1.5)
                }
                Spacer()
                Text(LiveCopy.wordmark).font(LiveType.display(13, .bold)).foregroundColor(LiveLook.greenLite)
            }
            if stale {
                Text(LiveCopy.check).font(LiveType.display(17, .bold))
                Text(LiveCopy.inAt(inSinceLocal)).font(LiveType.mono(12)).foregroundColor(LiveLook.paper.opacity(0.75))
            } else {
                HStack(alignment: .firstTextBaseline) {
                    LiveLook.count(from: inSince, until: until)
                        .font(LiveType.mono(40)).monospacedDigit()
                        .lineLimit(1).minimumScaleFactor(0.6)
                    Spacer(minLength: 8)
                    Text(LiveCopy.inAt(inSinceLocal)).font(LiveType.mono(12)).foregroundColor(LiveLook.paper.opacity(0.75))
                        .fixedSize()
                }
                if showsShift, let end = shiftEndsAt, let endLocal = shiftEndsAtLocal {
                    ProgressView(timerInterval: inSince...end, countsDown: false, label: { EmptyView() }, currentValueLabel: { EmptyView() })
                        .progressViewStyle(.linear)
                        .tint(LiveLook.greenLite)
                    Text(LiveCopy.shiftEnds(endLocal)).font(LiveType.mono(12)).foregroundColor(LiveLook.paper.opacity(0.75))
                }
            }
        }
        .foregroundColor(LiveLook.paper)
        .padding(.horizontal, 18)
        .padding(.vertical, 16)
    }
}

// MARK: - The home screen (systemSmall)

struct SmallWidgetFace: View {
    let share: PresenceShare
    let now: Date

    static func ground(_ face: PresenceShare.Face) -> Color { face == .atWork ? LiveLook.ink : LiveLook.paper }

    var body: some View {
        let face = share.face(at: now)
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Circle().fill(face == .atWork ? LiveLook.greenLite : LiveLook.line).frame(width: 8, height: 8)
                Spacer()
                Text(LiveCopy.wordmark).font(LiveType.display(12, .bold))
                    .foregroundColor(face == .atWork ? LiveLook.greenLite : LiveLook.green)
            }
            Spacer(minLength: 4)
            switch face {
            case .atWork:
                VStack(alignment: .leading, spacing: 2) {
                    Text(LiveCopy.atWork).font(LiveType.mono(11, bold: true)).kerning(1.3)
                    LiveLook.count(from: share.inSince ?? now, until: share.showUntil)
                        .font(LiveType.mono(26)).monospacedDigit()
                        .lineLimit(1).minimumScaleFactor(0.5)
                    if let hm = share.inSinceLocal {
                        Text(LiveCopy.inAt(hm)).font(LiveType.display(12)).foregroundColor(LiveLook.paper.opacity(0.7))
                    }
                }
                .foregroundColor(LiveLook.paper)
            case .out:
                VStack(alignment: .leading, spacing: 4) {
                    Text(LiveCopy.notIn).font(LiveType.mono(11, bold: true)).kerning(1.3).foregroundColor(LiveLook.ink.opacity(0.7))
                    Text(LiveCopy.tapIn).font(LiveType.display(16, .bold)).foregroundColor(LiveLook.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }
            case .unknown:
                Text(LiveCopy.check).font(LiveType.display(16, .bold)).foregroundColor(LiveLook.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }
}

// MARK: - The iOS 16 lock screen's own widget (accessoryRectangular)

@available(iOS 16.0, *)
struct RectangularWidgetFace: View {
    let share: PresenceShare
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            switch share.face(at: now) {
            case .atWork:
                Text(LiveCopy.atWork).font(LiveType.mono(11, bold: true))
                LiveLook.count(from: share.inSince ?? now, until: share.showUntil)
                    .font(LiveType.mono(20)).monospacedDigit().lineLimit(1).minimumScaleFactor(0.6)
                if let hm = share.inSinceLocal { Text(LiveCopy.inAt(hm)).font(LiveType.display(12)) }
            case .out:
                Text(LiveCopy.notIn).font(LiveType.mono(11, bold: true))
                Text(LiveCopy.tapIn).font(LiveType.display(13)).lineLimit(2)
            case .unknown:
                Text(LiveCopy.wordmark).font(LiveType.display(12, .bold))
                Text(LiveCopy.check).font(LiveType.display(13)).lineLimit(2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
