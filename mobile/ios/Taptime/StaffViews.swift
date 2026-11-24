import SwiftUI
import UIKit

// The staff app's own screens on iOS, drawn with the system's own components (the user's answer
// of 2026-11-24: "native components -- the tab bar, the navigation bar, a native drawer"): a
// TabView, a NavigationView per tab with a large title and its toolbar, an inset-grouped List for
// Settings, the system sheet (a draggable drawer, medium and large detents from iOS 16) with the
// system's wheel for the missing time, the system's confirmation dialog and alert. The brand is in
// the colours, the type and the status card; the chrome is iOS's.
//
// The tap screen (a plaque link's) and the result are TapViews.swift's and do not change (CLAUDE.md
// §9). An iPhone reads the plaque from the app's own button too (iPhone 7-X have no background
// read, Q41 (a)), so Today's plaque row IS that button.

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

/// The navigation bar's and tab bar's look: the system's bars, the brand's type and ink.
enum StaffAppearance {
    static func install() {
        let nav = UINavigationBarAppearance()
        nav.configureWithDefaultBackground()
        nav.backgroundColor = UIColor(Palette.porcelain).withAlphaComponent(0.92)
        let ink = UIColor(Palette.ink)
        if let large = Typeface.Faces.shared.display(34, .bold) {
            nav.largeTitleTextAttributes = [.font: large as UIFont, .foregroundColor: ink]
        }
        if let small = Typeface.Faces.shared.display(17, .bold) {
            nav.titleTextAttributes = [.font: small as UIFont, .foregroundColor: ink]
        }
        UINavigationBar.appearance().standardAppearance = nav
        UINavigationBar.appearance().scrollEdgeAppearance = nav
        UINavigationBar.appearance().compactAppearance = nav

        let tab = UITabBarAppearance()
        tab.configureWithDefaultBackground()
        UITabBar.appearance().standardAppearance = tab
        if #available(iOS 15.0, *) {
            UITabBar.appearance().scrollEdgeAppearance = tab
        }
    }
}

struct Avatar: View {
    let name: String
    var size: CGFloat = 32

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
    var radius: CGFloat = 20
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0, content: content)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: radius, style: .continuous).fill(fill))
    }
}

private struct Banner: View {
    let text: String
    var tone: NoticeTone = .ok
    var body: some View {
        Text(text).font(Typeface.display(14, .medium)).foregroundColor(Palette.ink)
            .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 16).padding(.vertical, 12)
            .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(
                tone == .ok ? Color.greenLite : (tone == .warn ? Color.saffronLite : Palette.tomato.opacity(0.12))))
    }
}

private struct Chevron: View {
    var body: some View {
        Image(systemName: "chevron.right").font(.system(size: 14, weight: .semibold)).foregroundColor(Color(UIColor.tertiaryLabel))
    }
}

/// The settings list's icon square: white symbol on a filled rounded square (the system's look).
private struct IconSquare: View {
    let symbol: String
    var fill: Color = Palette.green
    var body: some View {
        Image(systemName: symbol).font(.system(size: 15, weight: .semibold)).foregroundColor(.white)
            .frame(width: 29, height: 29).background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(fill))
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
        let name = me?.employeeName ?? view.employeeName
        TabView(selection: Binding(get: { staff.tab }, set: { staff.show($0) })) {
            NavigationView {
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        TodayView(staff: staff, tap: tap, view: view, problem: problem)
                    }
                    .padding(.horizontal, 16).padding(.vertical, 8)
                }
                .background(Palette.porcelain.ignoresSafeArea())
                .navigationTitle(StaffCopy.hello(firstName(name)))
                .toolbar { ToolbarItem(placement: .navigationBarTrailing) { Avatar(name: name) } }
                .refreshable { await tap.home(); await staff.refreshToday() }
            }
            .navigationViewStyle(.stack)
            .tabItem { Label(StaffCopy.tabToday, systemImage: "house") }
            .tag(StaffModel.Tab.today)

            NavigationView {
                HoursView(staff: staff)
                    .navigationTitle(StaffCopy.tabHours)
            }
            .navigationViewStyle(.stack)
            .tabItem { Label(StaffCopy.tabHours, systemImage: "clock") }
            .tag(StaffModel.Tab.hours)

            NavigationView {
                SettingsView(staff: staff, me: me, name: name)
                    .navigationTitle(StaffCopy.tabSettings)
            }
            .navigationViewStyle(.stack)
            .tabItem { Label(StaffCopy.tabSettings, systemImage: "gearshape") }
            .tag(StaffModel.Tab.settings)
        }
        .tint(Palette.green)
        .preferredColorScheme(.light)
        .sheet(isPresented: Binding(get: { staff.claims != nil }, set: { if !$0 { staff.closeSheet() } })) {
            if let list = staff.claims, staff.claimIndex < list.count {
                ClaimSheet(staff: staff, claim: list[staff.claimIndex], index: staff.claimIndex, count: list.count)
                    .drawerDetents()
            }
        }
    }
}

private extension View {
    /// The sheet as a drawer: half height and full height, with its grabber (iOS 16+). On iOS 15 the
    /// system sheet is full height, still dragged down to close.
    @ViewBuilder func drawerDetents() -> some View {
        if #available(iOS 16.0, *) {
            presentationDetents([.medium, .large]).presentationDragIndicator(.visible)
        } else {
            self
        }
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
        if let b = me?.business, !b.isEmpty {
            Text(b).font(Typeface.display(15, .medium)).foregroundColor(muted).padding(.horizontal, 4).padding(.top, -6)
        }
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
                                ProgressView(value: frac).progressViewStyle(.linear).tint(.greenLite)
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
                    Image(systemName: "wave.3.right").font(.system(size: 18, weight: .semibold)).foregroundColor(.white)
                        .frame(width: 44, height: 44).background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Palette.green))
                    VStack(alignment: .leading, spacing: 1) {
                        Text(tap.pressing ? StaffCopy.reading : (p != nil ? StaffCopy.tapOut : StaffCopy.tapIn))
                            .font(Typeface.display(17, .bold)).foregroundColor(Palette.ink)
                        Text(StaffCopy.tapHint).font(Typeface.display(13)).foregroundColor(muted)
                    }
                    Spacer()
                    if tap.pressing { ProgressView() } else { Chevron() }
                }
                .padding(.horizontal, 20).padding(.vertical, 16).background(Palette.paper)
            }
            .disabled(tap.pressing)
        }
        .clipShape(RoundedRectangle(cornerRadius: 24, style: .continuous))

        if let n = me?.missingTimes, n > 0 {
            Button { Task { await staff.openMissing() } } label: {
                HStack(spacing: 14) {
                    Text("\(n)").font(Typeface.mono(16, bold: true)).foregroundColor(Palette.ink)
                        .frame(width: 40, height: 40).background(RoundedRectangle(cornerRadius: 11, style: .continuous).fill(Palette.saffron))
                    VStack(alignment: .leading, spacing: 1) {
                        Text(n == 1 ? StaffCopy.missingOne : StaffCopy.missingMany(n)).font(Typeface.display(15, .bold))
                        Text(StaffCopy.missingSub).font(Typeface.display(13)).opacity(0.75)
                    }
                    .foregroundColor(Palette.ink)
                    Spacer()
                    if staff.claimBusy { ProgressView() } else { Chevron() }
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
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                content
            }
            .padding(.horizontal, 16).padding(.vertical, 8)
        }
        .background(Palette.porcelain.ignoresSafeArea())
        .refreshable { await staff.loadHours(week: staff.hours?.week, after: nil) }
        .toolbar {
            // Always the two buttons (a conditional toolbar needs iOS 16): disabled with no week.
            ToolbarItemGroup(placement: .navigationBarTrailing) {
                Button { staff.shiftWeek(-7) } label: { Image(systemName: "chevron.left") }
                    .disabled(!(staff.hours?.hasPrev ?? false) || staff.hoursBusy).accessibilityLabel(StaffCopy.previous)
                Button { staff.shiftWeek(7) } label: { Image(systemName: "chevron.right") }
                    .disabled(!(staff.hours?.hasNext ?? false) || staff.hoursBusy).accessibilityLabel(StaffCopy.next)
            }
        }
    }

    @ViewBuilder private var content: some View {
        if staff.hoursOff {
            Card {
                VStack(alignment: .leading, spacing: 8) {
                    IconSquare(symbol: "clock")
                    Text(StaffCopy.hoursOffTitle).font(Typeface.display(18, .bold))
                    Text(StaffCopy.hoursOffBody).font(Typeface.display(14)).foregroundColor(muted)
                }
                .padding(20)
            }
        } else if let h = staff.hours {
            Text(weekLabel(h)).font(Typeface.mono(13, bold: true)).foregroundColor(muted).padding(.horizontal, 4).padding(.top, -6)
            summary(h)
            dayChips(h)
            if let day = h.days.first(where: { $0.date == current(h) }) {
                timeline(day)
            }
            if h.nextCursor != nil {
                Button(StaffCopy.more) { Task { await staff.loadHours(week: h.week, after: h.nextCursor) } }
                    .frame(maxWidth: .infinity, minHeight: 44)
            }
        } else if staff.hoursFailed {
            Banner(text: StaffCopy.network, tone: .alert)
        } else {
            ProgressView().frame(maxWidth: .infinity, minHeight: 200)
        }
    }

    private func weekLabel(_ h: HoursResponse) -> String {
        guard let d = StaffModel.day(h.week), let e = Calendar(identifier: .gregorian).date(byAdding: .day, value: 6, to: d) else { return h.week }
        return weekday(h.week, "dMMM") + " – " + weekday(StaffModel.dayString(e), "dMMM")
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
                    .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(on ? Palette.ink : Palette.paper.opacity(future ? 0.5 : 1)))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.saffron, lineWidth: open && !on ? 2 : 0))
                }
                .buttonStyle(.plain)
                .disabled(future)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
    }

    private func timeline(_ day: HoursResponse.Day) -> some View {
        let recs = staff.records.filter { $0.date == day.date }
        let directed = recs.filter { !$0.direction.isEmpty }
        let others = recs.filter { $0.direction.isEmpty }
        return Card(radius: 22) {
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Text(weekday(day.date, "EEEEdMMMM")).font(Typeface.display(17, .bold))
                    Spacer()
                    if day.workedMinutes > 0 { Text(StaffCopy.duration(day.workedMinutes)).font(Typeface.mono(14, bold: true)) }
                }
                .padding(.bottom, 12)
                if recs.isEmpty && staff.saysNoRecords(on: day.date) {
                    Text(StaffCopy.dayEmpty).font(Typeface.display(14)).foregroundColor(muted)
                }
                ForEach(Array(directed.enumerated()), id: \.offset) { i, r in
                    row(r)
                    if i < directed.count - 1 {
                        let next = directed[i + 1]
                        let pair = r.direction == "in" && next.direction == "out"
                        HStack(spacing: 12) {
                            Rectangle().fill(pair ? Palette.green : Palette.line).frame(width: 2, height: 36).frame(width: 24)
                            if pair, let m = minutes(r.at, next.at, nextDay: next.date != r.date) {
                                Text(StaffCopy.duration(m)).font(Typeface.mono(12, bold: true)).foregroundColor(Palette.green)
                                    .padding(.horizontal, 9).padding(.vertical, 3).background(RoundedRectangle(cornerRadius: 8).fill(Color.greenLite))
                            }
                        }
                    }
                }
                if !others.isEmpty {
                    Divider().padding(.vertical, 12)
                    ForEach(Array(others.enumerated()), id: \.offset) { _, r in
                        Text("\(r.at) · \(StaffCopy.state(r.state))").font(Typeface.display(13)).foregroundColor(muted)
                    }
                }
            }
            .padding(.horizontal, 18).padding(.vertical, 16)
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

// MARK: - The missing-time drawer

private struct ClaimSheet: View {
    @ObservedObject var staff: StaffModel
    let claim: ClaimsResponse.Open
    let index: Int
    let count: Int
    @State private var time = Date()

    var body: some View {
        NavigationView {
            Form {
                Section {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("\(weekday(claim.anchorDate, "EEEdMMM")) · \(claim.anchorAt)").font(Typeface.mono(12, bold: true))
                            .padding(.horizontal, 10).padding(.vertical, 4).background(RoundedRectangle(cornerRadius: 8).fill(Color.saffronLite))
                        Text(StaffCopy.lead(kind: claim.kind, day: claim.day, date: weekday(claim.anchorDate, "EEEdMMM"), at: claim.anchorAt))
                            .font(Typeface.display(15)).foregroundColor(Palette.ink.opacity(0.8))
                        Text(StaffCopy.question(claim.kind)).font(Typeface.display(24, .bold)).tracking(-0.5)
                    }
                    .padding(.vertical, 6)
                }
                Section {
                    DatePicker(StaffCopy.question(claim.kind), selection: $time, displayedComponents: .hourAndMinute)
                        .datePickerStyle(.wheel).labelsHidden()
                        .environment(\.locale, Locale(identifier: "en_GB"))
                        .frame(maxWidth: .infinity)
                } footer: {
                    let hint = staff.claimHint(claim, picked: Self.hm(time))
                    Text(hint.text).foregroundColor(hint.problem ? Palette.tomato : nil)
                }
                Section {
                    Button(StaffCopy.skip) { staff.skip() }.disabled(staff.claimBusy)
                }
            }
            .navigationTitle(count > 1 ? "\(index + 1)/\(count)" : "")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(StaffCopy.cancel) { staff.closeSheet() }.disabled(staff.claimBusy)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if staff.claimBusy {
                        ProgressView()
                    } else {
                        Button(StaffCopy.sendShort) { Task { await staff.send(claim, at: Self.hm(time)) } }.font(.body.weight(.semibold))
                    }
                }
            }
        }
        .tint(Palette.green)
        .interactiveDismissDisabled(staff.claimBusy)
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
        List {
            Section {
                HStack(spacing: 14) {
                    Avatar(name: me?.employeeName ?? name, size: 56)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(me?.employeeName ?? name).font(Typeface.display(18, .bold))
                        if let b = me?.business, !b.isEmpty { Text(b).font(Typeface.display(13)).foregroundColor(muted) }
                        Label(StaffCopy.connected, systemImage: "checkmark").font(Typeface.display(12, .medium)).foregroundColor(Palette.green)
                    }
                }
                .padding(.vertical, 4)
            }
            Section {
                Toggle(isOn: Binding(get: { staff.remind }, set: { v in Task { await staff.setRemind(v) } })) {
                    Label { Text(StaffCopy.remind) } icon: { IconSquare(symbol: "bell.fill") }
                }
                // M16-47: only where a Live Activity can run (iOS 16.2+); an iPhone 7 has no row.
                if LiveCounter.supported {
                    Toggle(isOn: Binding(get: { staff.counter }, set: { v in staff.setCounter(v) })) {
                        Label {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(StaffCopy.counter)
                                Text(StaffCopy.counterSub).font(.caption).foregroundColor(.secondary)
                            }
                        } icon: { IconSquare(symbol: "lock.fill") }
                    }
                }
            } header: {
                Text(StaffCopy.notifications)
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    Text(staff.notificationsOff ? StaffCopy.notificationsOff : StaffCopy.remindHint)
                    if LiveCounter.supported, staff.activitiesOff {
                        Text(StaffCopy.activitiesOff)
                    }
                }
            }
            if let s = me?.shift {
                Section(StaffCopy.shift) {
                    HStack {
                        Label { Text(StaffCopy.usualShift) } icon: { IconSquare(symbol: "clock.fill") }
                        Spacer()
                        Text("\(s.start)–\(s.end)").font(Typeface.mono(15)).foregroundColor(.secondary)
                    }
                }
            }
            Section(StaffCopy.thisPhone) {
                Button {
                    if let u = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(u) }
                } label: {
                    HStack {
                        Label { Text(StaffCopy.language).foregroundColor(Palette.ink) } icon: { IconSquare(symbol: "globe", fill: Palette.ink.opacity(0.7)) }
                        Spacer()
                        Chevron()
                    }
                }
                Button(role: .destructive) { staff.confirm = .signOut } label: {
                    Label { Text(StaffCopy.signOut) } icon: { IconSquare(symbol: "rectangle.portrait.and.arrow.right", fill: Palette.tomato) }
                }
            }
            Section {
                Button(role: .destructive) { staff.confirm = .delete } label: {
                    Text(StaffCopy.delete).frame(maxWidth: .infinity)
                }
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    Text(StaffCopy.deleteSub)
                    Text(StaffCopy.privacy)
                    if let v = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String { Text(StaffCopy.version(v)) }
                }
            }
        }
        .listStyle(.insetGrouped)
        .task { await staff.refreshNotificationState() }
        .confirmationDialog(StaffCopy.signOutTitle, isPresented: Binding(get: { staff.confirm == .signOut }, set: { if !$0 { staff.confirm = nil } }),
                            titleVisibility: .visible) {
            Button(StaffCopy.signOutConfirm, role: .destructive) { Task { await staff.perform(.signOut) } }
            Button(StaffCopy.cancel, role: .cancel) { staff.confirm = nil }
        } message: {
            Text(staff.confirmFailed ? StaffCopy.failed : StaffCopy.signOutBody)
        }
        .alert(StaffCopy.deleteTitle, isPresented: Binding(get: { staff.confirm == .delete }, set: { if !$0 { staff.confirm = nil } })) {
            Button(StaffCopy.deleteConfirm, role: .destructive) { Task { await staff.perform(.delete) } }
            Button(StaffCopy.cancel, role: .cancel) { staff.confirm = nil }
        } message: {
            Text(staff.confirmFailed ? StaffCopy.failed : StaffCopy.deleteBody(me?.business ?? ""))
        }
    }
}
