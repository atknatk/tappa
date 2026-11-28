import Foundation
#if canImport(ActivityKit)
import ActivityKit
#endif

// THE LOCK-SCREEN COUNTER'S TYPE (M16-47; compiled into BOTH targets, as ActivityKit requires: the
// app requests the activity, the extension draws it, and both name the same type). It carries no
// person: the static attributes are empty, and the changing state is the check-in's instant, the
// shift's end when the owner marked shifts real, and the two wall clocks the server wrote. Started
// locally with no push type -- there is no push token and no push service (Q53, M16-46 [G46-e]).
//
// iOS 16.2+: ActivityContent and its staleDate are 16.2 (Apple's ActivityKit reference, re-read for
// this card); a phone on 15.0-16.1 (iPhone 7 included) gets no counter (Q53 (3)).
#if canImport(ActivityKit)
@available(iOS 16.2, *)
struct TimerAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var inSince: Date
        var inSinceLocal: String
        var shiftEndsAt: Date?
        var shiftEndsAtLocal: String?
    }
}
#endif
