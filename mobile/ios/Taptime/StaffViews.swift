import SwiftUI

// The staff app's own screens on iOS (the native shell of 2026-11-24, the Android app's twin):
// a top bar, three tabs -- Today, My hours, Settings -- and the missing-time sheet. The tap screen
// (a plaque link's) and the result are TapViews.swift's and do not change (CLAUDE.md §9). On an
// iPhone the plaque is read from the app's own button too (iPhone 7-X have no background read,
// Q41 (a)), so Today's plaque stub IS that button.

private extension Color {
    static let greenLite = Color(red: 0xE1 / 255, green: 0xED / 255, blue: 0xE6 / 255)
    static let saffronLite = Color(red: 0xF7 / 255, green: 0xEB / 255, blue: 0xD6 / 255)
}

private let muted = Palette.ink.opacity(0.7)

private func initials(_ name: String) -> String {
    let w = name.split(whereSeparator: { $0 == " " })
    let a = w.first.map { String($0.prefix(1)) } ?? ""
    let b = w.count > 1 ? String(w.last!.prefix(1)) : ""
    return (a + b).uppercased(with: Locale(identifier: "en_US_POSIX"))
}

private func firstName(_ name: String) -> String { name.split(separator: " ").first.map(String.init) ?? name }

struct Avatar: View {
    let name: String
    var size: CGFloat = 34

    var body: some View {
        Text(initials(name))
            .font(Typeface.display(size * 0.36, .bold))
            .foregroundColor(Palette.paper)
            .frame(width: size, height: size)
            .background(Circle().fill(Palette.green))
            .accessibilityHidden(true)
    }
}

private struct Card<Content: View>: View {
    var fill: Color = Palette.paper
    var radius: CGFloat = 22
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0, content: content)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: radius, style: .continuous).fill(fill))
            .shadow(color: Palette.ink.opacity(0.06), radius: 1, y: 1)
    }
}

private struct LargeTitle: View {
    let text: String
    var body: some View {
        Text(text).font(Typeface.display(32, .bold)).tracking(-0.8).foregroundColor(Palette.ink)
            .accessibilityAddTraits(.isHeader).padding(.horizontal, 4)
    }
}

private struct Banner: View {
    let text: String
    var tone: NoticeTone = .ok
    var body: some View {
        Text(text).font(Typeface.display(14, .medium)).foregroundColor(Palette.ink)
            .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 16).padding(.vertical, 12)
            .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(
                tone == .ok ? Color.greenLite : (tone == .warn ? Color.saffronLite : Palette.tomato.opacity(0.12))))
    }
}

private struct Chevron: View {
    var body: some View { Image(systemName: "chevron.right").font(.system(size: 15, weight: .semibold)).foregroundColor(Palette.ink) }
}

private struct IconTile: View {
    let symbol: String
    var tint: Color = Palette.green
    var fill: Color = .greenLite
    var body: some View {
        Image(systemName: symbol).font(.system(size: 17, weight: .medium)).foregroundColor(tint)
            .frame(width: 38, height: 38).background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(fill))
            .accessibilityHidden(true)
    }
}

// MARK: - The shell

struct StaffRootView: View {
    @ObservedObject var staff: StaffModel
    @ObservedObject var tap: TapModel
    let view: TapModel.View
    let problem: Copy.Problem?

    var body: some View {
        let me = tap.me
        ZStack {
            Palette.porcelain.ignoresSafeArea()
            VStack(spacing: 0) {
                TopBar(business: me?.business ?? "", name: me?.employeeName ?? view.employeeName)
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        switch staff.tab {
                        case .today: TodayView(staff: staff, tap: tap, view: view, problem: problem)
                        case .hours: HoursView(staff: staff)
                        case .settings: SettingsView(staff: staff, me: me, name: view.employeeName)
                        }
                    }
                    .padding(.horizontal, 16).padding(.top, 4).padding(.bottom, 24)
                }
                TabBar(staff: staff)
            }
            if let list = staff.claims, staff.claimIndex < list.count {
                ClaimSheet(staff: staff, claim: list[staff.claimIndex], index: staff.claimIndex, count: list.count)
            }
            if let c = staff.confirm {
                ConfirmOverlay(staff: staff, which: c, business: me?.business ?? "")
            }
        }
        .preferredColorScheme(.light)
    }
}

private struct TopBar: View {
    let business: String
    let name: String
    var body: some View {
        HStack(spacing: 10) {
            Text("taptime").font(Typeface.display(20, .bold)).tracking(-0.4).foregroundColor(Palette.green)
            if !business.isEmpty {
                Rectangle().fill(Palette.line).frame(width: 1, height: 18)
                Text(business).font(Typeface.display(14, .medium)).foregroundColor(muted).lineLimit(1)
            }
            Spacer()
            Avatar(name: name)
        }
        .padding(.horizontal, 20).frame(height: 56)
    }
}

private struct TabBar: View {
    @ObservedObject var staff: StaffModel
    var body: some View {
        VStack(spacing: 0) {
            Rectangle().fill(Palette.line).frame(height: 1)
            HStack {
                item(.today, "house", StaffCopy.tabToday)
                item(.hours, "clock", StaffCopy.tabHours)
                item(.settings, "gearshape", StaffCopy.tabSettings)
            }
            .padding(.horizontal, 8).padding(.top, 6).padding(.bottom, 2)
        }
        .background(Palette.paper.ignoresSafeArea(edges: .bottom))
    }

    private func item(_ t: StaffModel.Tab, _ symbol: String, _ label: String) -> some View {
        let on = staff.tab == t
        return Button { staff.show(t) } label: {
            VStack(spacing: 4) {
                Image(systemName: on ? symbol + ".fill" : symbol).font(.system(size: 19, weight: .medium))
                    .frame(width: 56, height: 30).background(Capsule().fill(on ? Color.greenLite : Color.clear))
                Text(label).font(Typeface.display(12, on ? .bold : .medium))
            }
            .foregroundColor(on ? Palette.green : muted).frame(maxWidth: .infinity, minHeight: 56)
        }
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

// MARK: - Today

private struct TodayView: View {
    @ObservedObject var staff: StaffModel
    @ObservedObject var tap: TapModel
    let view: TapModel.View
    let problem: Copy.Problem?

    var body: some View {
        let me = tap.me
        let p = me?.presence
        LargeTitle(text: StaffCopy.hello(firstName(me?.employeeName ?? view.employeeName)))
        if let n = staff.notice { Banner(text: n) }
        if let pr = problem { Banner(text: [pr.title, pr.message].compactMap { $0 }.joined(separator: " — "), tone: .alert) }

        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    Circle().fill(p != nil ? Color.greenLite : Palette.line).frame(width: 8, height: 8)
                    Text(p != nil ? StaffCopy.atWork : StaffCopy.notIn).font(Typeface.mono(12, bold: true)).kerning(1.6)
                    Spacer()
                    if let p = p { Text(StaffCopy.inAt(p.inSinceLocal)).font(Typeface.mono(12)).opacity(0.75) }
                }
                if let p = p, let since = Presence.instant(p.inSince) {
                    TimelineView(.periodic(from: Date(), by: 1)) { ctx in
                        let now = ctx.date.addingTimeInterval(tap.api.clock.currentOffset)
                        let s = max(0, Int(now.timeIntervalSince(since)))
                        VStack(alignment: .leading, spacing: 12) {
                            HStack(alignment: .lastTextBaseline, spacing: 4) {
                                Text("\(s / 3600):\(String(format: "%02d", (s / 60) % 60))").font(Typeface.mono(64)).tracking(-2)
                                Text(":\(String(format: "%02d", s % 60))").font(Typeface.mono(22)).opacity(0.6)
                            }
                            .accessibilityElement(children: .combine)
                            if let end = Presence.instant(p.shiftEndsAt), end > since {
                                let frac = min(1, max(0, now.timeIntervalSince(since) / end.timeIntervalSince(since)))
                                let leftMin = Int(end.timeIntervalSince(now) / 60)
                                GeometryReader { g in
                                    ZStack(alignment: .leading) {
                                        Capsule().fill(Palette.paper.opacity(0.14))
                                        Capsule().fill(Color.greenLite).frame(width: g.size.width * frac)
                                    }
                                }
                                .frame(height: 8)
                                HStack {
                                    Text(StaffCopy.shiftUntil(p.shiftEndsAtLocal ?? ""))
                                    Spacer()
                                    Text(leftMin >= 0 ? StaffCopy.left(StaffCopy.duration(leftMin)) : StaffCopy.over(StaffCopy.duration(-leftMin)))
                                }
                                .font(Typeface.mono(12)).opacity(0.75)
                            }
                        }
                    }
                } else {
                    Text(StaffCopy.notInBody).font(Typeface.display(22, .bold))
                    if let s = me?.shift { Text("\(s.start)–\(s.end)" + (s.venue.isEmpty ? "" : " · \(s.venue)")).font(Typeface.display(13)).opacity(0.75) }
                }
            }
            .foregroundColor(Palette.paper)
            .padding(22).frame(maxWidth: .infinity, alignment: .leading).background(Palette.ink)

            Button {
                Task { await tap.press(want: view.direction.isEmpty ? nil : view.direction) }
            } label: {
                HStack(spacing: 14) {
                    IconTile(symbol: "iphone.radiowaves.left.and.right")
                    VStack(alignment: .leading, spacing: 1) {
                        Text(tap.pressing ? StaffCopy.reading : (p != nil ? StaffCopy.tapOut : StaffCopy.tapIn))
                            .font(Typeface.display(16, .bold)).foregroundColor(Palette.ink)
                        Text(StaffCopy.tapHint).font(Typeface.display(13)).foregroundColor(muted)
                    }
                    Spacer()
                    Chevron()
                }
                .padding(.horizontal, 22).padding(.vertical, 18).background(Palette.paper)
            }
            .disabled(tap.pressing)
        }
        .clipShape(RoundedRectangle(cornerRadius: 24, style: .continuous))

        if let n = me?.missingTimes, n > 0 {
            Button { Task { await staff.openMissing() } } label: {
                HStack(spacing: 14) {
                    Text("\(n)").font(Typeface.mono(16, bold: true)).foregroundColor(Palette.ink)
                        .frame(width: 40, height: 40).background(RoundedRectangle(cornerRadius: 12).fill(Palette.saffron))
                    VStack(alignment: .leading, spacing: 1) {
                        Text(n == 1 ? StaffCopy.missingOne : StaffCopy.missingMany(n)).font(Typeface.display(15, .bold))
                        Text(StaffCopy.missingSub).font(Typeface.display(13)).opacity(0.75)
                    }
                    .foregroundColor(Palette.ink)
                    Spacer()
                    Chevron()
                }
                .padding(.horizontal, 16).padding(.vertical, 14)
                .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Color.saffronLite))
            }
            .disabled(staff.claimBusy)
        }

        if let w = staff.week, (me?.hoursWeeks ?? 0) > 0 {
            Button { staff.show(.hours) } label: {
                Card(radius: 18) {
                    HStack(spacing: 14) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(StaffCopy.week).font(Typeface.display(13)).foregroundColor(muted)
                            Text(StaffCopy.duration(w.totals.workedMinutes)).font(Typeface.mono(24)).foregroundColor(Palette.ink)
                        }
                        Spacer()
                        WeekBars(days: w.days)
                        Chevron()
                    }
                    .padding(16)
                }
            }
        }
    }
}

private struct WeekBars: View {
    let days: [HoursResponse.Day]
    var body: some View {
        let mx = max(1, days.map(\.workedMinutes).max() ?? 1)
        HStack(alignment: .bottom, spacing: 4) {
            ForEach(Array(days.enumerated()), id: \.offset) { _, d in
                RoundedRectangle(cornerRadius: 3)
                    .fill(d.workedMinutes > 0 ? Palette.green : Palette.line)
                    .frame(width: 8, height: d.workedMinutes > 0 ? 6 + 30 * CGFloat(d.workedMinutes) / CGFloat(mx) : 4)
            }
        }
        .frame(height: 36, alignment: .bottom).accessibilityHidden(true)
    }
}

// MARK: - My hours

private func weekday(_ date: String, _ pattern: String) -> String {
    guard let d = StaffModel.day(date) else { return date }
    let f = DateFormatter()
    f.timeZone = TimeZone(identifier: "UTC")
    f.locale = Locale(identifier: StaffCopy.turkish ? "tr_TR" : "en_GB")
    f.setLocalizedDateFormatFromTemplate(pattern)
    return f.string(from: d)
}

private struct HoursView: View {
    @ObservedObject var staff: StaffModel
    @State private var chosen: String?

    var body: some View {
        HStack {
            LargeTitle(text: StaffCopy.tabHours)
            Spacer()
            if let h = staff.hours {
                HStack(spacing: 2) {
                    arrow("chevron.left", StaffCopy.previous, h.hasPrev) { staff.shiftWeek(-7) }
                    Text(weekday(h.week, "dMMM")).font(Typeface.mono(13, bold: true)).padding(.horizontal, 4)
                    arrow("chevron.right", StaffCopy.next, h.hasNext) { staff.shiftWeek(7) }
                }
                .padding(2).background(RoundedRectangle(cornerRadius: 14).fill(Palette.paper))
            }
        }
        if staff.hoursOff {
            Card {
                VStack(alignment: .leading, spacing: 8) {
                    IconTile(symbol: "clock")
                    Text(StaffCopy.hoursOffTitle).font(Typeface.display(18, .bold))
                    Text(StaffCopy.hoursOffBody).font(Typeface.display(14)).foregroundColor(muted)
                }
                .padding(22)
            }
        } else if let h = staff.hours {
            summary(h)
            dayChips(h)
            if let day = h.days.first(where: { $0.date == current(h) }) {
                timeline(day)
            }
            if h.nextCursor != nil {
                Button(StaffCopy.more) { Task { await staff.loadHours(week: h.week, after: h.nextCursor) } }
                    .font(Typeface.display(15, .medium)).foregroundColor(muted).frame(maxWidth: .infinity, minHeight: 48)
            }
        } else if staff.hoursFailed {
            Banner(text: StaffCopy.network, tone: .alert)
        } else {
            ForEach([132, 80, 220], id: \.self) { hgt in
                RoundedRectangle(cornerRadius: 22).fill(Palette.line.opacity(0.6)).frame(height: CGFloat(hgt))
            }
        }
    }

    private func arrow(_ symbol: String, _ label: String, _ on: Bool, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: 15, weight: .semibold)).frame(width: 40, height: 40)
                .foregroundColor(on ? Palette.ink : Palette.ink.opacity(0.25))
        }
        .disabled(!on || staff.hoursBusy).accessibilityLabel(label)
    }

    private func current(_ h: HoursResponse) -> String? {
        if let c = chosen, h.days.contains(where: { $0.date == c }) { return c }
        let today = StaffModel.dayString(Date())
        if h.days.contains(where: { $0.date == today }) { return today }
        let byDay = Set(staff.records.map(\.date))
        return h.days.last(where: { byDay.contains($0.date) })?.date ?? h.days.first?.date
    }

    private func summary(_ h: HoursResponse) -> some View {
        let t = h.totals
        var notes: [String] = []
        if t.awaitingMinutes > 0 { notes.append(StaffCopy.awaiting(StaffCopy.duration(t.awaitingMinutes))) }
        if t.openEntries > 0 { notes.append(StaffCopy.openNote(t.openEntries)) }
        if t.checkInMissing > 0 { notes.append(StaffCopy.inMissing(t.checkInMissing)) }
        if let w = h.waiting, w.records + w.claims > 0 { notes.append(StaffCopy.waitingLine(records: w.records, claims: w.claims)) }
        if t.truncated { notes.append(StaffCopy.truncated) }
        return VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .bottom) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(h.hasNext ? StaffCopy.counted : StaffCopy.week).font(Typeface.display(13)).opacity(0.75)
                    Text(StaffCopy.duration(t.workedMinutes)).font(Typeface.mono(36)).tracking(-1)
                }
                Spacer()
                if t.openEntries > 0 {
                    Text(StaffCopy.openChip(t.openEntries)).font(Typeface.display(12, .medium)).foregroundColor(.saffronLite)
                        .padding(.horizontal, 10).padding(.vertical, 6).background(RoundedRectangle(cornerRadius: 10).fill(Palette.saffron.opacity(0.25)))
                }
            }
            if !notes.isEmpty { Text(notes.joined(separator: " ")).font(Typeface.display(13)).opacity(0.78) }
        }
        .foregroundColor(Palette.paper).padding(.horizontal, 22).padding(.vertical, 20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 24, style: .continuous).fill(Palette.ink))
    }

    private func dayChips(_ h: HoursResponse) -> some View {
        let today = StaffModel.dayString(Date())
        let sel = current(h)
        return HStack(spacing: 6) {
            ForEach(h.days, id: \.date) { d in
                let on = d.date == sel
                let recs = staff.records.filter { $0.date == d.date }
                let open = !recs.isEmpty && d.workedMinutes == 0 && recs.contains { $0.direction == "in" }
                let future = d.date > today
                Button { chosen = d.date } label: {
                    VStack(spacing: 5) {
                        Text(weekday(d.date, "EEE")).font(Typeface.display(11)).opacity(0.7)
                        Text(weekday(d.date, "d")).font(Typeface.mono(15, bold: true))
                        RoundedRectangle(cornerRadius: 2).fill(d.workedMinutes > 0 ? (on ? Color.greenLite : Palette.green) : (open ? Palette.saffron : (future ? .clear : Palette.line)))
                            .frame(width: 18, height: 4)
                        Text(d.workedMinutes > 0 ? "\(d.workedMinutes / 60):\(String(format: "%02d", d.workedMinutes % 60))" : (open ? "•" : "—"))
                            .font(Typeface.mono(10))
                    }
                    .foregroundColor(on ? Palette.paper : Palette.ink.opacity(future ? 0.5 : 1))
                    .frame(maxWidth: .infinity).padding(.top, 10).padding(.bottom, 8)
                    .background(RoundedRectangle(cornerRadius: 14).fill(on ? Palette.ink : Palette.paper.opacity(future ? 0.5 : 1)))
                    .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Palette.saffron, lineWidth: open && !on ? 2 : 0))
                }
                .disabled(future)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
    }

    private func timeline(_ day: HoursResponse.Day) -> some View {
        let recs = staff.records.filter { $0.date == day.date }
        let directed = recs.filter { !$0.direction.isEmpty }
        let others = recs.filter { $0.direction.isEmpty }
        return Card(radius: 24) {
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Text(weekday(day.date, "EEEEdMMMM")).font(Typeface.display(17, .bold))
                    Spacer()
                    if day.workedMinutes > 0 { Text(StaffCopy.duration(day.workedMinutes)).font(Typeface.mono(14, bold: true)) }
                }
                .padding(.bottom, 12)
                if recs.isEmpty {
                    Text(StaffCopy.dayEmpty).font(Typeface.display(14)).foregroundColor(muted)
                }
                ForEach(Array(directed.enumerated()), id: \.offset) { i, r in
                    row(r)
                    if i < directed.count - 1 {
                        let next = directed[i + 1]
                        let pair = r.direction == "in" && next.direction == "out"
                        HStack(spacing: 12) {
                            Rectangle().fill(pair ? Palette.green : Palette.line).frame(width: 2, height: 40).frame(width: 24)
                            if pair, let m = minutes(r.at, next.at, nextDay: next.date != r.date) {
                                Text(StaffCopy.duration(m)).font(Typeface.mono(12, bold: true)).foregroundColor(Palette.green)
                                    .padding(.horizontal, 9).padding(.vertical, 3).background(RoundedRectangle(cornerRadius: 8).fill(Color.greenLite))
                            }
                        }
                    }
                }
                if !others.isEmpty {
                    Rectangle().fill(Palette.line).frame(height: 1).padding(.vertical, 12)
                    ForEach(Array(others.enumerated()), id: \.offset) { _, r in
                        Text("\(r.at) · \(StaffCopy.state(r.state))").font(Typeface.display(13)).foregroundColor(muted)
                    }
                }
            }
            .padding(.horizontal, 20).padding(.vertical, 18)
        }
    }

    private func row(_ r: HoursResponse.Record) -> some View {
        let tone: Color = r.state == "approved" ? Palette.green : (r.state == "waiting" ? Palette.saffron : (r.state == "rejected" ? Palette.tomato : Palette.line))
        var sub: [String] = []
        if !r.venue.isEmpty { sub.append(r.venue) }
        if let c = StaffCopy.channel(r.channel) { sub.append(c) }
        if r.state != "approved" { sub.append(StaffCopy.state(r.state)) }
        if let c = StaffCopy.claim(r.claim) { sub.append(c) }
        return HStack(spacing: 12) {
            ZStack {
                Circle().fill(tone.opacity(0.18)).frame(width: 22, height: 22)
                Circle().fill(tone).frame(width: 12, height: 12)
            }
            .frame(width: 24)
            VStack(alignment: .leading, spacing: 1) {
                Text(r.direction == "in" ? StaffCopy.dirIn : StaffCopy.dirOut).font(Typeface.display(15, .bold))
                if !sub.isEmpty { Text(sub.joined(separator: " · ")).font(Typeface.display(13)).foregroundColor(muted) }
            }
            Spacer()
            Text(r.at).font(Typeface.mono(18))
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }

    private func minutes(_ a: String, _ b: String, nextDay: Bool) -> Int? {
        func hm(_ s: String) -> Int? {
            let p = s.split(separator: ":").compactMap { Int($0) }
            return p.count == 2 ? p[0] * 60 + p[1] : nil
        }
        guard let x = hm(a), let y = hm(b) else { return nil }
        let m = y - x + (nextDay ? 24 * 60 : 0)
        return m >= 0 ? m : nil
    }
}

// MARK: - The missing-time sheet

private struct ClaimSheet: View {
    @ObservedObject var staff: StaffModel
    let claim: ClaimsResponse.Open
    let index: Int
    let count: Int
    @State private var time = Date()

    var body: some View {
        ZStack(alignment: .bottom) {
            Palette.ink.opacity(0.55).ignoresSafeArea()
                .onTapGesture { if !staff.claimBusy { staff.closeSheet() } }
            VStack(alignment: .leading, spacing: 16) {
                Capsule().fill(Palette.line).frame(width: 40, height: 5).frame(maxWidth: .infinity)
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 8) {
                        Text("\(weekday(claim.anchorDate, "EEEdMMM")) · \(claim.anchorAt)").font(Typeface.mono(12, bold: true))
                            .padding(.horizontal, 10).padding(.vertical, 4).background(RoundedRectangle(cornerRadius: 8).fill(Color.saffronLite))
                        if count > 1 { Text("\(index + 1)/\(count)").font(Typeface.mono(12)).foregroundColor(muted) }
                    }
                    Text(StaffCopy.lead(kind: claim.kind, day: claim.day, date: weekday(claim.anchorDate, "EEEdMMM"), at: claim.anchorAt))
                        .font(Typeface.display(15)).foregroundColor(Palette.ink.opacity(0.8))
                    Text(StaffCopy.question(claim.kind)).font(Typeface.display(26, .bold)).tracking(-0.5)
                }
                DatePicker("", selection: $time, displayedComponents: .hourAndMinute)
                    .datePickerStyle(.wheel).labelsHidden()
                    .environment(\.locale, Locale(identifier: "en_GB"))
                    .frame(maxWidth: .infinity)
                    .background(RoundedRectangle(cornerRadius: 20).fill(Palette.porcelain))
                Text(staff.claimProblem ?? StaffCopy.window(from: claim.windowFrom.at, to: claim.windowTo.at))
                    .font(Typeface.display(13, staff.claimProblem != nil ? .medium : .regular))
                    .foregroundColor(staff.claimProblem != nil ? Palette.tomato : muted)
                    .frame(maxWidth: .infinity)
                Button {
                    Task { await staff.send(claim, at: Self.hm(time)) }
                } label: {
                    Text(staff.claimBusy ? StaffCopy.sending : StaffCopy.send).font(Typeface.display(17, .bold)).foregroundColor(Palette.paper)
                        .frame(maxWidth: .infinity, minHeight: 56)
                        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(Palette.green.opacity(staff.claimBusy ? 0.5 : 1)))
                }
                .disabled(staff.claimBusy)
                Button(StaffCopy.skip) { staff.skip() }
                    .font(Typeface.display(15, .medium)).foregroundColor(muted).frame(maxWidth: .infinity, minHeight: 44)
                    .disabled(staff.claimBusy)
            }
            .padding(.horizontal, 20).padding(.top, 10).padding(.bottom, 20)
            .background(RoundedRectangle(cornerRadius: 28, style: .continuous).fill(Palette.paper).ignoresSafeArea(edges: .bottom))
            .transition(.move(edge: .bottom))
        }
        .onAppear { time = Self.date(claim.prefill ?? claim.anchorAt) }
        .onChange(of: claim.context) { _ in time = Self.date(claim.prefill ?? claim.anchorAt) }
    }

    private static var cal: Calendar {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = .current
        return c
    }

    static func date(_ hm: String) -> Date {
        let p = hm.split(separator: ":").compactMap { Int($0) }
        return cal.date(bySettingHour: p.first ?? 9, minute: p.count > 1 ? p[1] : 0, second: 0, of: Date()) ?? Date()
    }

    static func hm(_ d: Date) -> String {
        let c = cal.dateComponents([.hour, .minute], from: d)
        return String(format: "%02d:%02d", c.hour ?? 0, c.minute ?? 0)
    }
}

// MARK: - Settings

private struct SettingsView: View {
    @ObservedObject var staff: StaffModel
    let me: MeResponse?
    let name: String

    var body: some View {
        LargeTitle(text: StaffCopy.tabSettings)
        Card {
            HStack(spacing: 14) {
                Avatar(name: me?.employeeName ?? name, size: 56)
                VStack(alignment: .leading, spacing: 2) {
                    Text(me?.employeeName ?? name).font(Typeface.display(18, .bold))
                    if let b = me?.business, !b.isEmpty { Text(b).font(Typeface.display(13)).foregroundColor(muted) }
                    Label(StaffCopy.connected, systemImage: "checkmark").font(Typeface.display(12, .medium)).foregroundColor(Palette.green)
                }
                Spacer()
            }
            .padding(16)
        }
        section(StaffCopy.notifications)
        Card {
            Toggle(isOn: Binding(get: { staff.remind }, set: { v in Task { await staff.setRemind(v) } })) {
                HStack(spacing: 14) {
                    IconTile(symbol: "bell")
                    VStack(alignment: .leading, spacing: 2) {
                        Text(StaffCopy.remind).font(Typeface.display(16, .bold))
                        Text(StaffCopy.remindHint).font(Typeface.display(13)).foregroundColor(muted)
                    }
                }
            }
            .tint(Palette.green).padding(.horizontal, 16).padding(.vertical, 12)
        }
        if staff.notificationsOff { Banner(text: StaffCopy.notificationsOff, tone: .warn) }
        if let s = me?.shift {
            section(StaffCopy.shift)
            Card {
                HStack(spacing: 14) {
                    IconTile(symbol: "clock")
                    VStack(alignment: .leading, spacing: 2) {
                        Text(StaffCopy.usualShift).font(Typeface.display(16, .bold))
                        if !s.venue.isEmpty { Text(s.venue).font(Typeface.display(13)).foregroundColor(muted) }
                    }
                    Spacer()
                    Text("\(s.start)–\(s.end)").font(Typeface.mono(15, bold: true))
                }
                .padding(.horizontal, 16).padding(.vertical, 12)
            }
        }
        section(StaffCopy.thisPhone)
        Card {
            row("globe", StaffCopy.language, StaffCopy.languageSub, tint: Palette.green) {
                if let u = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(u) }
            }
            Rectangle().fill(Palette.line).frame(height: 1).padding(.leading, 68)
            row("rectangle.portrait.and.arrow.right", StaffCopy.signOut, nil, tint: Palette.tomato) { staff.confirm = .signOut }
            Rectangle().fill(Palette.line).frame(height: 1).padding(.leading, 68)
            row("trash", StaffCopy.delete, StaffCopy.deleteSub, tint: Palette.tomato) { staff.confirm = .delete }
        }
        Text(StaffCopy.privacy).font(Typeface.display(12)).foregroundColor(muted).padding(.horizontal, 8)
        if let v = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String {
            Text(StaffCopy.version(v)).font(Typeface.mono(12)).foregroundColor(muted).padding(.horizontal, 8)
        }
    }

    private func section(_ t: String) -> some View {
        Text(t).font(Typeface.display(13, .medium)).foregroundColor(muted).padding(.leading, 8).padding(.top, 6)
    }

    private func row(_ symbol: String, _ title: String, _ sub: String?, tint: Color, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 14) {
                IconTile(symbol: symbol, tint: tint, fill: tint == Palette.green ? .greenLite : Palette.tomato.opacity(0.1))
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(Typeface.display(16, .bold)).foregroundColor(tint == Palette.green ? Palette.ink : Palette.tomato)
                    if let s = sub { Text(s).font(Typeface.display(13)).foregroundColor(muted) }
                }
                Spacer()
                if tint == Palette.green { Chevron() }
            }
            .padding(.horizontal, 16).padding(.vertical, 12).frame(minHeight: 64)
        }
    }
}

private struct ConfirmOverlay: View {
    @ObservedObject var staff: StaffModel
    let which: StaffModel.Confirm
    let business: String

    var body: some View {
        ZStack {
            Palette.ink.opacity(0.55).ignoresSafeArea().onTapGesture { if !staff.confirmBusy { staff.confirm = nil } }
            VStack(alignment: .leading, spacing: 12) {
                Text(which == .signOut ? StaffCopy.signOutTitle : StaffCopy.deleteTitle).font(Typeface.display(20, .bold))
                Text(staff.confirmFailed ? StaffCopy.failed : (which == .signOut ? StaffCopy.signOutBody : StaffCopy.deleteBody(business)))
                    .font(Typeface.display(15)).foregroundColor(Palette.ink.opacity(0.8))
                Button {
                    Task { await staff.perform(which) }
                } label: {
                    Text(which == .signOut ? StaffCopy.signOutConfirm : StaffCopy.deleteConfirm).font(Typeface.display(17, .bold))
                        .foregroundColor(Palette.paper).frame(maxWidth: .infinity, minHeight: 56)
                        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(Palette.tomato.opacity(staff.confirmBusy ? 0.5 : 1)))
                }
                .disabled(staff.confirmBusy).padding(.top, 4)
                Button(StaffCopy.cancel) { staff.confirm = nil }
                    .font(Typeface.display(15, .medium)).foregroundColor(muted).frame(maxWidth: .infinity, minHeight: 44)
                    .disabled(staff.confirmBusy)
            }
            .padding(22)
            .background(RoundedRectangle(cornerRadius: 24, style: .continuous).fill(Palette.paper))
            .padding(24)
        }
    }
}
