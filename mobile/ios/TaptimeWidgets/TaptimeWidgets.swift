import SwiftUI
import WidgetKit
#if canImport(ActivityKit)
import ActivityKit
#endif

// THE TAPTIME WIDGET EXTENSION (M16-47): the home-screen widget and, on iOS 16.2+, the lock-screen
// Live Activity with its Dynamic Island. It reads one thing -- the App Group's PresenceShare, which
// only the app writes -- and draws it; it makes no request, reads no location, keeps no process
// running. Tapping any of it opens the app (no widgetURL: the default action is the app's own
// launch; a URL would be routed as a plaque link).
//
// THE BUNDLE ON A 15.0 TARGET. The extension runs on 15.0 like the app, but its Live Activity is a
// 16.2 type. A WidgetBundle body cannot name an unavailable widget, so the entry point picks one of
// two bundles at launch -- the standard WidgetBundleBuilder-free availability split.
@main
struct TaptimeWidgetsMain {
    static func main() {
        if #available(iOS 16.2, *) {
            TaptimeWidgetsWithActivity.main()
        } else {
            TaptimeWidgetsHomeOnly.main()
        }
    }
}

struct TaptimeWidgetsHomeOnly: WidgetBundle {
    var body: some Widget {
        PresenceWidget()
    }
}

@available(iOS 16.2, *)
struct TaptimeWidgetsWithActivity: WidgetBundle {
    var body: some Widget {
        PresenceWidget()
        TimerActivityWidget()
    }
}

// MARK: - The home-screen widget

struct PresenceEntry: TimelineEntry {
    let date: Date
    let share: PresenceShare
}

/// Two entries at most: now, and showUntil, when an "at work" face turns into "open the app to
/// check". Nothing after that changes without the app, which reloads the timelines on every /me.
struct PresenceProvider: TimelineProvider {
    var store: () -> PresenceShare = { PresenceShareStore.shared.read() }

    func placeholder(in context: Context) -> PresenceEntry {
        PresenceEntry(date: Date(), share: .none)
    }

    func getSnapshot(in context: Context, completion: @escaping (PresenceEntry) -> Void) {
        completion(PresenceEntry(date: Date(), share: context.isPreview ? .none : store()))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<PresenceEntry>) -> Void) {
        completion(PresenceProvider.timeline(store(), now: Date()))
    }

    static func timeline(_ share: PresenceShare, now: Date) -> Timeline<PresenceEntry> {
        Timeline(entries: share.faceChanges(from: now).map { PresenceEntry(date: $0, share: share) }, policy: .never)
    }
}

struct PresenceWidget: Widget {
    static let kind = "taptime.presence"

    var body: some WidgetConfiguration {
        StaticConfiguration(kind: PresenceWidget.kind, provider: PresenceProvider()) { entry in
            PresenceWidgetView(entry: entry)
        }
        .configurationDisplayName(LiveCopy.widgetName)
        .description(LiveCopy.widgetDescription)
        .supportedFamilies(PresenceWidget.families)
    }

    static var families: [WidgetFamily] {
        if #available(iOSApplicationExtension 16.0, *) {
            return [.systemSmall, .accessoryRectangular]
        }
        return [.systemSmall]
    }
}

struct PresenceWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: PresenceEntry

    var body: some View {
        if PresenceWidgetView.isRectangular(family), #available(iOSApplicationExtension 16.0, *) {
            RectangularWidgetFace(share: entry.share, now: entry.date)
                .widgetGround(.clear)
        } else {
            SmallWidgetFace(share: entry.share, now: entry.date)
                .padding(PresenceWidgetView.inset)
                .widgetGround(SmallWidgetFace.ground(entry.share.face(at: entry.date)))
        }
    }

    private static func isRectangular(_ f: WidgetFamily) -> Bool {
        if #available(iOSApplicationExtension 16.0, *) { return f == .accessoryRectangular }
        return false
    }

    /// iOS 17+ adds its own content margins; before it the face pads itself (the design's 14 pt).
    private static var inset: CGFloat {
        if #available(iOSApplicationExtension 17.0, *) { return 0 }
        return 14
    }
}

private extension View {
    /// The widget's ground: containerBackground on iOS 17+ (required there), a plain background before.
    @ViewBuilder
    func widgetGround(_ color: Color) -> some View {
        if #available(iOSApplicationExtension 17.0, *) {
            containerBackground(color, for: .widget)
        } else {
            background(color)
        }
    }
}

// MARK: - The Live Activity (iOS 16.2+)

#if canImport(ActivityKit)
@available(iOS 16.2, *)
struct TimerActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: TimerAttributes.self) { context in
            LockCard(inSince: context.state.inSince, inSinceLocal: context.state.inSinceLocal,
                     shiftEndsAt: context.state.shiftEndsAt, shiftEndsAtLocal: context.state.shiftEndsAtLocal,
                     until: nil, stale: context.isStale)
                .activityBackgroundTint(LiveLook.card)
                .activitySystemActionForegroundColor(LiveLook.paper)
        } dynamicIsland: { context in
            let s = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    HStack(spacing: 6) {
                        Circle().fill(LiveLook.greenLite).frame(width: 7, height: 7)
                        Text(LiveCopy.clockedIn).font(LiveType.mono(11, bold: true)).kerning(1.2)
                    }
                    .foregroundColor(LiveLook.paper)
                    .padding(.leading, 6)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    Text(LiveCopy.wordmark).font(LiveType.display(13, .bold)).foregroundColor(LiveLook.greenLite)
                        .padding(.trailing, 6)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading, spacing: 6) {
                        if context.isStale {
                            Text(LiveCopy.check).font(LiveType.display(16, .bold))
                        } else {
                            HStack(alignment: .firstTextBaseline) {
                                LiveLook.count(from: s.inSince, until: nil).font(LiveType.mono(32)).monospacedDigit()
                                Spacer(minLength: 8)
                                Text(LiveCopy.inAt(s.inSinceLocal)).font(LiveType.mono(12)).foregroundColor(LiveLook.paper.opacity(0.75)).fixedSize()
                            }
                            if let end = s.shiftEndsAt, let endLocal = s.shiftEndsAtLocal, end > s.inSince {
                                ProgressView(timerInterval: s.inSince...end, countsDown: false, label: { EmptyView() }, currentValueLabel: { EmptyView() })
                                    .progressViewStyle(.linear).tint(LiveLook.greenLite)
                                Text(LiveCopy.shiftEnds(endLocal)).font(LiveType.mono(12)).foregroundColor(LiveLook.paper.opacity(0.75))
                            }
                        }
                    }
                    .foregroundColor(LiveLook.paper)
                    .padding(.horizontal, 6)
                }
            } compactLeading: {
                LiveMark()
            } compactTrailing: {
                // A running Text takes all the width it is offered; the island would grow to the
                // screen's edge (measured in the simulator). H:MM:SS at 13 pt fits in 62.
                IslandCount(state: s, stale: context.isStale, size: 13)
                    .frame(width: 62, alignment: .trailing)
            } minimal: {
                IslandCount(state: s, stale: context.isStale, size: 9)
                    .frame(maxWidth: 40)
                    .minimumScaleFactor(0.6)
            }
            .keylineTint(LiveLook.greenLite)
        }
    }
}

/// The island's running count; with a stale activity, the dot alone (there is no count to trust).
@available(iOS 16.2, *)
private struct IslandCount: View {
    let state: TimerAttributes.ContentState
    let stale: Bool
    let size: CGFloat

    var body: some View {
        if stale {
            Circle().fill(LiveLook.greenLite).frame(width: 8, height: 8)
        } else {
            LiveLook.count(from: state.inSince, until: nil)
                .font(LiveType.mono(size)).monospacedDigit()
                .multilineTextAlignment(.trailing)
                .foregroundColor(LiveLook.greenLite)
        }
    }
}
#endif
