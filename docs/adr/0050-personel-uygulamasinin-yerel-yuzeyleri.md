# ADR 0050 — Personel uygulamasının yerel yüzeyleri: eksik saat talebi, hatırlatma, kilit ekranı, vardiyam ve dil

- **Durum:** **Kabul edildi (2026-11-28)** — kullanıcı kabul etti (M16-42 kabul kriteri 4); kabul notu §11'de. Yazımı: cevaplanmış
  Q50–Q54'ten (2026-11-19), bu dalda **önceden yazılmış** koddan (M16-43 … M16-49) ve M16-42'nin kabul kriterlerinden. Kod belgeden
  önce yazıldığı için cevaplarla kodun ayrıldığı yerler gizlenmedi: **§7 Açık / çelişki**.
- **Tarih:** 2026-11-28 · **Dal:** `m16-native-ui` (PR #49, `main` dışı; yalnız belge) · **Plan:** [m16-mobil.md](../plan/m16-mobil.md) → M16-42
  (*Faz G ekleri*)
- **Karar veren:** kullanıcı (ürün sahibi) — **kullanıcı kararı 6** (2026-11-19, AskUserQuestion: personel uygulamasına altı ek) ve
  **Q50–Q54'ün cevapları** (2026-11-19, beşinde de öneri seçildi — [open-questions.md](../plan/open-questions.md) Q50–Q54, *cevaplandı*
  hücreleri). §2'deki **K-** maddeleri bu cevaplardır ve sabittir. Sınır sayıları ve uygulama ayrıntısı (§2'de *kod* diye anılanlar)
  kodun bugün yaptığıdır; bu ADR'nin kabulüyle bağlanır. **§3'teki üç ek** hiçbir Q'nun cevabı değildir — kabulde ayrıca sorulur.
- **İlgili:** ADR 0008 · 0023 · 0026 · **0032** (Faz C: talep, K3, K4; §A.7, §C.3–§C.5) · 0042 (vardiya kayıtta donar) · **0045** (§2.2
  bağımlılık yok, §2.3 cihaz anahtarı, §3 kırmızı çizgiler) · 0048 (salt okuma ölçüm kimliği; bu dalda yok) · 0051 · CLAUDE.md §4.1–§4.7,
  §5 (*Yön*, *Unutulan çıkış*, *Geç kalma*), §6, §7, §9 · Q41, Q48, **Q50–Q54**.
- **Atıf kuralı:** `file:line` atıfları `m16-native-ui` `5ea748d1`'e göredir. **Okundu** = dosyadan okundu, çalıştırılmadı.
  **Denetimde ölçüldü** = denetçi B'nin 2026-11-19 üretim sayımı (salt okuma, yalnız sayım); bu belgeyi yazan **yeniden ölçmedi**.

---

## 1. Bağlam — bugün (okundu)

1. **Kullanıcı kararı 6 (2026-11-19).** Personel uygulamasına altı ek: uygulamadan eksik yarı talebi, *"hâlâ içeridesin"* hatırlatması,
   kilit ekranında *"içeride"* sayacı, vardiyam, onay bekleyen kayıtlarım ve dil. Beşincisi yeni kart değildir (M16-31'in pencere geneli
   `waiting` sayısı, M16-33'ün bilgi satırı — state.md:2053) ve bu ADR'nin konusu değildir. Kalan beşi M16-43 … M16-49'dur.
2. **Canlı taban — denetimde ölçüldü (2026-11-19).** Sorgu M16-42 madde 1'dedir (`audit_log.at`; sözdizimi canlıda doğrulandı).
   13 lokasyonun ve 6 departmanın **hiçbirinde** vardiya yok; son 30 günde 75 girişin 59'u `shift_source='none'`, 16'sı NULL (00095
   öncesi), vardiyalı **0**; 30 günde 12 talep (`audit_log` `claim.submitted` sayımı da 12). **İş günü sınırı** (N1, tenant kesimiyle;
   çalışan ve departman kesimi 0 satır): girişten sonraki 10 saat iş günü sonunu 05:00 kesimli işletmelerde 31 girişin 13'ünde, 23:00
   kesimli işletmede 44 girişin 7'sinde aşar; 12 saat 23 girişte, 18 saat 75 girişin 69'unda aşar. Kapalı çiftlerin süresi: < 6 sa 19 ·
   6–8 sa 7 · 8–10 sa 18 · 10–12 sa 0 · 12–18 sa 1. Kırpılan 20 hatırlatmanın 1'i gerçek çıkıştan **önce** düşerdi (kırpmasız 10 saat
   kuralında 4 + 1). Demo işletmede de vardiya yok. **Sonuç:** vardiyaya bağlı her şey bugün boş çalışırdı; vardiyasız yol birincildir.
3. **Talep yolu, M16-43'ten önce.** Çalışanın tek yolu tarayıcı sonuç ekranının kartıydı: `Tap.resultClaim`
   (`internal/handler/claim.go:117`) → `claim.Service.Offer` (`internal/domain/claim/offer.go:38`); cevap `POST /api/claim` (`claim.go:47`)
   → `Tap.Claim` (`:299`) → `claim.Service.Submit` (`internal/domain/claim/submit.go:68`). Kartın bağlamı oturuma bağlı bir MAC'tir,
   ömrü 15 dk (`internal/handler/claimcontext.go:63-65`); çapa ve tür **yalnız** bağlamdan okunur (ADR 0032 `:791`). **K4** (aynı eksik
   yarı en çok iki kez sorulur — ADR 0032 `:11`, `:631`) yalnız o bağlamla uygulanır; çıkış talebinin ilk gösterimi çapadan **sonraki**
   kayıttır (`Chain.Shown`, `internal/domain/claim/chain.go:397`), yani bugünün hâlâ açık girişi tarayıcıda hiç sorulmaz. `Submit` bunu
   sınamıyordu: ardı gelmeyen bir `in` için `Chain.Missing` `KindOut` (`chain.go:140`) ve `WindowOf` boş olmayan bir pencere (`:251`)
   verir — denetim Y1'in vakası: 09:00 girişine 12:00'de onaylanan talep kişi işteyken manuel bir `out` yazar, 18:00 dokunuşu yeni bir
   `in` olur (M15'in kapattığı zincir tersinmesi).
4. **`/app/v1/me`, M16-45'ten önce:** `employeeName`, `brand`, `direction` (Q41). Yön `tenant.Directory.AppHome` → `predictTap`'tendir
   (`internal/domain/tenant/tapprediction.go:47`, açık giriş `GetLastOpenTransaction` `:81`) → `tap.PredictDirection`
   (`internal/domain/tap/decide.go:694`). `PredictDirection` açık girişi şimdiki iş gününün başından önceyse
   `LeftOpenPreviousBusinessDay`, 18 saatten eskiyse (`StaleOpenIn`, `decide.go:571`) `LeftOpenStale` sayar — yani bir dokunuşun
   **çıkış** yazacağı son an iş günü sonu ile `inSince + 18 sa`'nin küçüğüdür, yalnız 18 sa değil (N1).
5. **Vardiya modeli:** `locations` ve `departments`'ta günlük duvar saati (`db/migrations/00002_create_locations_departments.sql:32-33`,
   `:79-80`); haftanın günü ve **roster tablosu yok**; ürün kişinin izin günlerini bilmez. Örnek veride KF mekân vardiyası 10:00–22:00
   (`test/fixtures/seed.sql:174`) — bir **açılış penceresi**, kişinin vardiyası değil. Açık girişin vardiyası kayıtta donar (ADR 0042,
   00095): departmanınki, yoksa **dokunulan** lokasyonunki.
6. **Dil:** iOS metinleri düz Swift dizgeleri, Android `res/values/strings.xml`; sunucu uygulamaya metin değil veri gönderir (kapalı
   kod sözlükleri, `day` ve `HH:MM` alanları). Tarayıcıda i18n yok (`web/templates/layout/base.templ:135` `lang="en"`). **İki CLAUDE.md
   cümlesi** konuya değer: §7 kullanıcıya görünen metni Türkçe/İngilizce'yle sınırlar (`CLAUDE.md:556`); §9 *"Uygulama ekranları"* tap ve
   sonuç ekranının tarayıcıyla **"aynı metin"** olmasını ve iki platformun tek metin listesini paylaşmasını ister (`CLAUDE.md:598`).

## 2. Karar

### (i) Uygulamadan eksik yarı talebi — ADR 0032 Faz C'nin ikinci giriş noktası

- **K-1 — ikinci yazma yolu yok.** Uygulama aynı `claim.Service.Submit`'i, aynı kişi kilidi, pencere, ön dolgu (K3), `MaxBackdate`
  (90 gün, `internal/domain/manual/manual.go:218`), tek talep kuralı (`punch_claims_one_per_anchor`) ve müdür kuyruğuyla kullanır. Kod:
  tarayıcı (`claim.go:356`) ve uygulama (`internal/handler/appclaims.go:233`) **tek** adımı, `claimSend`'i (`appclaims.go:261-298`)
  koşar; `Submit`'in üretimdeki tek çağıranı odur (`TestAppClaims_SubmitHasOneCallerInTheHandlers`). Talep kayıt değildir; saati yalnız
  müdürün onayı manuel satırla yazar (onay akışına dokunulmadı).
- **K-2 — giriş yeri (Q50 (1) (a)).** Ana ekranda *"Missing times"*, yalnız `/me` `missingTimes > 0` iken, **Saatlerim ayarından
  bağımsız** (talep kartı tarayıcıda da Saatlerim'den bağımsızdır). Kod: `missingTimes` = `Open`'ın uzunluğu, okuma hatasında 0 + ERROR
  satırı, `/me` yine 200 (`appclaims.go:300-312`); Android `ui/StaffScreens.kt:162`, iOS `StaffViews.swift:254`.
- **K-3 — K4 bu girişe uygulanmaz (Q50 (2) (a)), ama K4'ün dolaylı koruması açıkça kurulur.** K4 sonuç ekranının *sorma* sınırıdır;
  kişinin kendi açtığı liste sorma değildir. Motorun bir sonraki dokunuşla hâlâ kapatacağı açık giriş talep **kabul etmez**:
  `Chain.StillClosable` (`chain.go:166-183`) — zincirin son yönlü kaydı bir `in` ve `tap.PredictDirection` *şimdi* `out` diyor
  (`LeftOpen == nil`). `Open` onu listelemez (`internal/domain/claim/open.go:100`); `Submit` kişi kilidi altında, kilit altında okunan
  zincirde `ErrStillOpen` döner (`submit.go:131-137`; `internal/domain/claim/claim.go:128-131`). Ardından yönlü bir kayıt gelince, iş günü
  bitince ya da 18 sa geçince çapa talep alır. Motorun kendi fonksiyonu olduğu için liste ve düğme ayrışamaz
  (`TestOpenDB_TheStillOpenCheckInTakesNoClaim`). Tarayıcı yolu etkilenmez: kart yalnız çapadan sonraki bir kayıtta çıkar ve
  `transactions` değişmez olduğundan o kayıt `Submit` anında da vardır (`claim.go:375-378`); sonuç ekranının K4 kuralı aynen durur
  (`TestClaimDB_TheCardIsShownOnTwoTapsOnly`).
- **K-4 — geriye bakış 7 gün (Q50 (4) (a)).** Kod: `OpenLookback` 7 gün, en çok 20 öğe, en yeni önce; gönderilenler 30 gün
  (`open.go:26-33`). K4 uygulanmadığı için iki gösterimi geçmiş çapa da listededir (`TestOpenDB_TheListIgnoresK4AndLooksBackAWeek`).
  Deaktive kişide liste boş (`open.go:67-69`).
- **K-5 — tarayıcı eşi yok (Q50 (3) (a)).** Tarayıcıda liste yoktur; tarayıcı Saatlerim'i (M16-32) salt okumadır. Tarayıcı formu *Sonra*.
- **K-6 — gönderilen talebin görünürlüğü (Q50 (5) (b)).** Kararın kendisi (*Approved* / *Not approved*) her zaman görünür — kişinin
  kendi isteğinin cevabıdır; onaylanan **saat** bir kayıttır ve yalnız Saatlerim açıkken (`tenants.staff_hours_weeks IS NOT NULL`,
  Q48) gösterilir. Domain bunu taşır (`open.go:128-175`, `showHours` `:160-162`;
  `TestOpenDB_SentShowsTheDecisionAndTheHourOnlyWhenAllowed`). **Handler bugün saati hiç göstermez — §7 Ç1.** Kararın notu ve karar
  veren seçilmez (`db/queries/punchclaims.sql:253-272`).
- **Kod — bağlam cihaza bağlı.** `mintForDevice` / `parseForDevice` (`claimcontext.go:167-177`): aynı yük, etiket, anahtar ve 15 dk;
  tutucu `app-device:<id>` (`internal/handler/tapcontext.go:374`). Önek iki uzayı ayırır: cihaz için basılmış bağlam bir oturumda, oturum
  bağlamı bir cihazda doğrulanmaz (`claimcontext.go:144-154`; `TestAppClaimsDB_ListSendAndTheContextsHolder`).
- **Kod — iki uç** (`internal/handler/app.go:218-220`, rotalar `:251-252`): `POST /app/v1/claims` imzalı okuma (`GET` değil: imzalı okuma
  sorgu dizgesini reddeder), gövde `{}` (`appclaims.go:126-185`); `POST /app/v1/claims/send` nonce'lu imza, gövde `{"context","at"}`
  (`:207-242`). Saatler ve tarihler işletmenin diliminde, **sunucuda** biçimlenir (`HH:MM`, `YYYY-MM-DD`, `day` = `today` | `yesterday` |
  `earlier`, sunucunun *şimdi*sine göre — `:82-95`).
- **Kod — sonuçlar veridir, hata değil:** 200 `{"result": …}`, kapalı yedi kelime — `sent`, `already_sent`, `outside_window`, `expired`,
  `resolved`, `not_active`, `still_open` (`appclaims.go:197-205`). Bozuk, süresi geçmiş ya da başka cihazın bağlamı var olan
  `stale_context`'tir; kapalı hata sözlüğüne **yeni kod eklenmedi**. `still_open` bir red değil bir *"henüz değil"*dir: uygulama metni
  *"You're still clocked in"* / *"Nothing was sent. Tap the plaque when you leave."* (Android `res/values/strings.xml:328-329`).
- **Kod — log:** opak id ve tür; saat, ad, mekân yok (`appclaims.go:14`, `:240`).

### (ii) Yerel hatırlatma ve kilit ekranı sayacı

- **K-7 — sunucu push'u yok, mekanik olarak.** APNs/FCM yok; arka plan modu, konum ve konum tetikleyicili bildirim yok; Android'de
  foreground service, `RECEIVE_BOOT_COMPLETED` ve kesin alarm izni yok; bağımlılık eklenmedi (`NotificationCompat`, WorkManager yok —
  platform API'leri, minSdk 26; `staff/StaffNotifications.kt:15-17`). Anlar **yalnız** `/me`'den gelir; telefon yön, dilim, vardiya
  hesaplamaz. Mekanik tutanlar: iOS hedeflerinde `aps-environment` yok (`mobile/ios/Taptime/Taptime.entitlements:16`, `TaptimeWidgets/TaptimeWidgets.entitlements:7`; izin
  listesi `scripts/mobile-ios-test.sh:53-54`); Android `MergedManifestTest.theStaffReceiverIsPrivateAndNothingRunsInTheBackground`
  (`mobile/android/app/src/test/kotlin/mt/taptime/app/MergedManifestTest.kt:189-212`: boot alıcısı, kesin alarm, C2DM, Firebase,
  foreground service, servis yok); konum kalıpları redline R2 (`scripts/redline-check.sh:511`;
  `TestRedline_M16_MobileBackgroundLocation`). iOS hatırlatması zaman tetikleyicisidir (`UNTimeIntervalNotificationTrigger`,
  `mobile/ios/Taptime/StaffCopy.swift:261`); Live Activity `pushType: nil` ile yerelde başlar ve biter (`PresenceSync.swift:202-203`).
- **K-8 — açık girişin sınırı tek bir `tap` yardımcısıdır.** `tap.OpenUntil(in, cutoff)` = `min(NextBusinessDayStart(BusinessDayStart(in)),
  in + StaleOpenIn)` (`decide.go:715-742`) — `PredictDirection`'ın `LeftOpen` vermeye başladığı an; iş günü yoksa yalnız 18 sa. `/me`'nin
  `presence.staleAt`'i budur ve `presence`'ın her anı onunla sınırlanır: sınırdan sonra *"clock out"* hatırlatması kişiyi yeni bir
  **giriş** yazmaya gönderirdi (`TestDecide_OpenUntilIsWherePredictDirectionStopsClosing`, `TestDecide_OpenUntilPinsTheCardsInstants`).
- **K-9 — hatırlatma anı (Q51 (1) (a), (2) (b) + (d)).** Taban: owner vardiyayı gerçek diye işaretlediyse ve dondurulmuş vardiyanın sonu
  `(inSince, staleAt]` içindeyse **vardiya bitişi + 30 dk**; değilse **`inSince + 10 sa`** (vardiya uydurmaz; metin girişin saatini
  söyler). Taban **`staleAt − 30 dk`**'ya kırpılır; kırpılmış an girişten 1 saatten kısa sonraysa hatırlatma **yok** (22:45 girişi, 23:00
  kesim). Kod: `internal/domain/tenant/presence.go:21-37` (sabitler), `:68-94` (`presenceOf`); `TestPresenceOf_TheCardsInstants`,
  `TestPresenceOf_NightShiftAcrossMidnight`, `TestAppHomeDB_PresenceAndUsualShift`. Metin: *"Still at work?"* / *"You clocked in at
  09:00. If you've finished, remember to clock out."*; vardiya kolunda *"… Your shift ended at 17:00."* (Android `strings.xml:334-336`,
  iOS `StaffCopy.swift:194-196`) — koşulludur, çalışmayı bitirmeye çağırmaz.
- **K-10 — işletme ayarı yok (Q51 (3) (a)).** Bildirim telefonla sahibi arasındadır; sunucu yeni veri üretmez. Koşullu migration 00121
  (`staff_reminders`) yazılmadı. **Sunucudan kapatma yolu:** `remindAt`'i null döndüren bir sürüm — kurulu bildirim bir sonraki
  senkronda silinir (Android `staff/PresenceSync.kt:56-63`, iOS `StaffCopy.swift:234-243`).
- **K-11 — izin anı (Q51 (4) (a)).** Ayarlar'da *"Remind me to clock out"*, **varsayılan kapalı**; bildirim izni yalnız anahtar açılırken
  istenir, tap, sonuç ve aktivasyon ekranlarında asla (Android `AndroidManifest.xml:18-21`, `strings.xml:292-293`; Android anahtarları
  `staff/StaffNotifications.kt:35-43`; iOS `StaffCopy.swift:211-226`).
- **K-12 — kilit ekranı sayacı (Q53 (1)–(4), hepsi (a)).** Ayarlar'da *"Show a timer on the lock screen"*, **varsayılan kapalı**
  (`strings.xml:294`; iOS `PresenceSync.swift:62-72`). Kilit ekranında *"Clocked in"* + sistemin çizdiği sayaç + yalnız owner vardiyayı
  işaretlediyse *"Shift ends 17:00"*; **işletme, mekân ve ad yok** (Android `StaffNotifications.kt:87-111`; iOS `Shared/LiveCopy.swift:3-27`).
  Ömür: `showUntil = min(remindAt + 2 sa, staleAt)` (hatırlatma yoksa `staleAt`), **asla iş günü sonundan sonra değil** (`presence.go:85-92`);
  Android `setTimeoutAfter(showUntil)` ile kalkar (`StaffNotifications.kt:106`), iOS'ta `staleDate = showUntil` sonrası bayat görünüm
  *"Open Taptime to check"* (sayaçsız) sistem sınırına kadar (`PresenceSync.swift:84-104`). Android 14+'ta kaydırılarak kapatılan sayaç
  **aynı giriş boyunca geri gelmez** (Q53 (4) (a)) — **kod bunu yalnız bellekte tutar: §7 Ç2.**

### (iii) `presence` ve Saatlerim

- **K-13.** `presence`, Saatlerim ayarından (Q48) **bağımsızdır**: kişinin **şu anki** durumudur ve `/me`'nin `direction`'ı (Q41) onu
  zaten söyler; bir kayıt listesi değildir, Q48'in kapsamına girmez. Yalnız tahmin `out` iken doludur — önceki iş gününden kalmış ya da
  bayat açık giriş *"içeride"* sayılmaz; tahminin okuduğu **aynı** satır ve kesimden hesaplanır (`internal/domain/tenant/apphome.go:34-37`,
  `:107-125`). Tel biçimi: `{"inSince","inSinceLocal","shiftEndsAt","shiftEndsAtLocal","remindAt","showUntil","staleAt"}` — anlar RFC 3339
  UTC, `*Local` saatler sunucuda işletmenin diliminde (`internal/handler/apptap.go:100-108`, `:117-136`; `TestAppContract_ResponseShapes`).

### (iv) Vardiyam

- **K-14 — ne (Q52 (1) (a)).** *"Usual shift"*: kişinin departman vardiyası, yoksa **profil** lokasyonunun vardiyası ve o lokasyonun adı;
  **tarih yok** (roster yok; *"next shift"* izin gününde yanlış olurdu). Profil, dokunulan lokasyon değil: satır *"usual"* der, dokunuş
  yoktur ve geç kalma hesaplamaz (CLAUDE.md §5'in *"dokunulan lokasyon"* kuralı geç kalmanındır). İçerideyken bildirimin ve kilit ekranının
  vardiya bitişi açık girişin **dondurulmuş** vardiyasıdır (`presence.go:70-77`). Kod: `usualShiftOf` (`presence.go:117-144`),
  `GetEmployeeUsualShift` (`db/queries/employees.sql:1388`); `TestUsualShiftOf_DepartmentThenProfileLocation`. İki yerde: ana ekran ve
  Ayarlar (Android `ui/StaffScreens.kt:144`, `:531`; iOS `StaffViews.swift:228`, `:633`).
- **K-15 — kim açar (Q52 (2) (c)).** Ayrı owner anahtarı **`tenants.staff_shift_visible`**, varsayılan kapalı — migration **00116**
  (öneri numarası 00120'nin yerine; `db/migrations/00116_add_the_staff_shift_switch.sql:1-34`). Anlamı *"bu değerler personelin gerçek
  vardiyasıdır"*: vardiyam satırını, hatırlatmanın vardiya kolunu ve kilit ekranının *"Shift ends"* satırını **birlikte** açar. Panel: Account
  ekranında yalnız owner, değişiklik ve `audit_log` `tenant.staff_shift_updated` tek transaction'da
  (`internal/handler/accountstaffshift.go:18-54`; `TestStaffShiftDB_OwnerSwitchesWithItsTrailRow`); metin *"These are my staff's real shift
  times."* (`web/templates/pages/account.templ:807-820`). Okuma başarısızsa vardiya kullanılmaz ve `presence` de düşer — *"gerçek değil"*
  tahmin edilmez (`apphome.go:127-132`).
- **Bağlantı (adlandırıldı).** Anahtar motorun kararını değiştirmez: geç kalma, talep ön dolgusu (K3) ve müdür onayının bandı (ADR 0032
  §C.4) zaten **girilmiş her** vardiyayı kullanır (`00116:11-12`). Hatırlatmayı vardiyaya bağlamak için owner'ın vardiya **girmesi**
  gerekir ve vardiya girmek o üçünü de açar — owner bunu yalnız hatırlatma için yapmamalı; vardiyasız işletme hatırlatmayı yine alır.

### (v) Dil

- **K-16 — Q54'ün cevapları.** (1) (a) pilot müdürlerinin adlandırdığı en çok 2–3 dil — liste M16-49'un ön koşuludur ve **ayrıca sorulur**;
  (2) (a) makine taslağı + her dil için anadili konuşan bir onaycı satır satır (adı repoya yazılmaz); (3) (a) **telefonun dili** —
  iOS'un ve Android 13+'ün uygulama başına dil ayarı dahil, **platformun** ekranı; uygulamada seçici yok; listede olmayan dil → İngilizce;
  (4) (a) tarayıcı tap ekranı çevrilmez; (5) (a) Faz G'de, onaycı zamanında yoksa (b) yayından sonra. Kaynak dil İngilizcedir; kişi, mekân
  ve işletme adı çevrilmez; **hukuki metin (gizlilik bildirimi) çevrilmez**.
- **Kod bugün:** dil listesi **`en` + `tr`** (Android `res/xml/locales_config.xml`; `res/values-tr/strings.xml` 328 kaydın tamamı); iOS
  yalnız personel ekranlarında ve kilit ekranında `StaffCopy.t(en, tr)` çiftleri, telefonun ilk dili `tr` ise (`StaffCopy.swift:4-12`,
  `Shared/LiveCopy.swift:9-14`). Ayarlar'daki *"Language"* satırı uygulama içi seçici değildir, platformun ekranını açar (Android 13+
  `ACTION_APP_LOCALE_SETTINGS`, `MainActivity.kt:624-627`; iOS Ayarlar uygulaması, `StaffViews.swift:644`). Listenin, onayın ve iki
  platformun kapsamının durumu **§7 Ç3, Ç4, Ç6**'dadır.
- **K-17 — iki CLAUDE.md cümlesi (önerilen sözcükler; kabulde onaylanır, M16-49'un commit'inde uygulanır).** §7 (`CLAUDE.md:556`):
  *"… Türkçe/İngilizce olabilir; personel uygulamasının ekran metinleri ayrıca ADR 0050'nin dil listesindeki dillerde (kaynak dil
  İngilizce)."* · §9 *"Uygulama ekranları"* (`CLAUDE.md:598`): *"aynı metin"* → *"İngilizcede aynı metin; ADR 0050'nin listesindeki dillerde
  onaylı çevirisi — tarayıcı İngilizce kalır"*. Sözcükler M16-49 madde 2'nin önerisidir; Q54'ün *cevaplandı* hücresi sözcüklerin onayını
  **anmaz** — bu yüzden kabulde sorulur.
- **Çeviri onayının kaydı** (M16-49 madde 5): her dilin commit'i dil başına onaylanan anahtar sayısını ve onay tarihini kart düzeltmesine
  yazar; onaysız satır İngilizce kalır; onaysız dil sevk edilmez.

## 3. Hiçbir Q'nun cevabı olmayan, kodda var olan üç ek — kabulde ayrıca sorulur

- **E-1 — *"Remind me in an hour"* ve *"Open Taptime"*** (iki platform, `62c6d6d1`, 2026-11-24). Erteleme yalnız bir saat sonrası en geç
  `staleAt − 30 dk` ise sunulur ve kurulur (Android `StaffNotifications.kt:141`, `:155`; iOS `StaffCopy.swift:258-277`). Q51 erteleme
  sormadı; sınır K-8/K-9'a uyar.
- **E-2 — ana ekran widget'ı** (Android `staff/PresenceWidget.kt`, `62c6d6d1`; iOS `TaptimeWidgets/`, `250bd852`). Kod yorumu onu
  *"the user's request of 2026-11-24"* diye anar (`PresenceWidget.kt:13`); plan belgelerinde kaydı yok. Ad, mekân, saat toplamı taşımaz;
  kendi isteği yoktur, uygulamanın son `/me`'sinden çizer. İki platform farklı sınır kullanır — §7 Ç7.
- **E-3 — iOS'ta kişinin kapattığı Live Activity bir sonraki `/me`'de yeniden başlar.** `SystemCounter.running` yalnız `active`/`stale`
  etkinlikleri sayar (`PresenceSync.swift:182-184`), plan boş görünce `start` üretir (`:95-96`). Q53 (4) yalnız Android 14+'ı sordu; Q53'ün
  gerekçesi (*"kişinin kapattığı bir şeyi geri getirmek onun kararını çiğner"*) iOS'a da uyar.

## 4. Kırmızı çizgiler — madde madde

- **§4.1:** biyometri yok; bildirim, Live Activity ve widget kullanıcı varlığı bayrağı ya da anahtar erişim denetimi istemez; ADR 0045
  §2.3'ün cihaz anahtarı değişmez.
- **§4.2:** konum yok, arka plan yok, push yok (K-7); hatırlatma zaman tetikleyicilidir, konum tetikleyicisi ve geofence yoktur. GPS yalnız
  tap anında okunur (değişmedi).
- **§4.3:** talep kayıt değildir (`punch_claims`); saati yalnız müdürün onayı `channel='manual'` satırla yazar; `transactions`'a yeni yazar
  yok. `still_open` hiçbir satır yazmaz.
- **§4.4:** değmez (sayaç ve SUN yolu değişmedi).
- **§4.5:** her yeni okuma açık `tenant_id` **ve** `employee_id` taşır, her tabloda (`ListOwnPunchClaims` `punchclaims.sql:262-272`,
  `GetEmployeeUsualShift` `employees.sql:1388-1418`); kimlik doğrulanmış cihazın imzalı okumasından (`authRead` / `authRequest`); bağlam
  cihaza bağlı. RLS: `TestRLS_PunchClaims_TenantIsolation`. Yeni tablo yok; 00116 tek sütun ve sütun düzeyi `UPDATE` GRANT'ı.
- **§4.6:** talep ve kayıt kaybolmaz; `still_open` bir red değil bir *"henüz değil"*dir — açık giriş bir sonraki dokunuşla kapanır ya da
  sonra talep alır. Sessiz onay yok: talep müdür kuyruğundadır.
- **§4.7 + §7:** kilit ekranında ve widget'ta ad, işletme, mekân yok; log'da saat yok (yalnız opak id ve tür); yeni sır yok.

## 5. Değerlendirilen alternatifler

| Alternatif | Neden değil |
|---|---|
| Sunucu push'u (APNs/FCM) | §4.2'nin ruhu ve ADR 0045 §2.2 (üçüncü parti SDK yok); sunucu telefonu uyandırmaz. Bedeli: tarayıcıdan çıkış telefona ulaşmaz (§6 B-1). |
| Android foreground service | Sürekli arka plan işi; §4.2 satış argümanına aykırı; Play beyanı ister. |
| `NotificationCompat` / WorkManager | Yeni bağımlılık (CLAUDE.md §1); minSdk 26'da platform API'leri yeter. |
| Kesin alarm izinleri (`SCHEDULE_EXACT_ALARM`, `USE_EXACT_ALARM`) | Ayrı kullanıcı izni ve Play beyanı; gecikme metni zaten girişin saatini söylediği için doğru kalır. M16-40 eşiği (≤ 30 dk) aşılırsa `setWindow` denenir. |
| *"Next shift"* (Q52 (1) (b)) | Roster yok; izin gününde yanlış olur. |
| K4'ü aynen uygulamak (Q50 (2) (b)) | Kendi açılan liste sorma değildir; asıl koruma (hâlâ açık giriş) K-3 ile açıkça kurulur. |
| Hatırlatmayı yalnız vardiyalı girişte kurmak (Q51 (2) (a)) | Üretimde vardiyalı giriş 0 (§1.2): özellik hiç çalışmazdı. |
| Sınırı aşan hatırlatmayı hiç kurmamak (Q51 (2) (d)'nin öbür dalı) | Akşam girişlerinin üçte birinden fazlasında özelliği sessizce kapatırdı. |
| İşletme ayarı `staff_reminders` (Q51 (3) (b), 00121) | Bildirim telefonla sahibi arasındadır; işletmenin verisi değişmez. |
| Uygulama içi dil seçici / işletme varsayılan dili (Q54 (3) (b), (c), 00122) | Yeni §9 yüzeyi ya da yeni sütun; platformun dil ekranı yeter. |
| Tarayıcıya talep formu ve çeviri (Q50 (3) (b), Q54 (4) (b)) | *Sonra* — ayrı kartlar; tarayıcı ekranı kutsaldır (§9). |

## 6. Bedeller

- **B-1 — tarayıcıdan çıkıştan sonra:** push olmadığı için bildirim yine düşebilir ve sayaç `showUntil`'e kadar yanlışlıkla *"Clocked in"* der
  (vardiyasız girişte en çok `inSince + 12 sa`; her durumda en çok `staleAt`, yani asla iş günü sonundan ya da 18 sa'ten sonra değil); iOS'ta
  sonra bayat görünüm. Senkronlanmamış telefonda kurulu bildirim bir kez daha düşer.
- **B-2 — Android'de bir saate kadar gecikme:** tek **kesin olmayan** alarm (`setAndAllowWhileIdle`, `StaffNotifications.kt:78`); Doze'da daha geç.
  M16-40 üç hatırlatmayla ölçer, eşik ≤ 30 dk. Yeniden başlatma alarmı düşürür; uygulama bir sonraki açılışta yeniden kurar (`PresenceSync.kt:10-12`).
- **B-3 — iOS 15–16.1'de sayaç yok:** dağıtım hedefi 15.0 (`mobile/ios/Taptime.xcodeproj/project.pbxproj:538`), `ActivityContent` ve
  `staleDate` iOS 16.2 (`PresenceSync.swift:74-78`); iPhone 7 dahil. Hatırlatma çalışır.
- **B-4 — Live Activity sınırı:** Apple'a göre en çok 8 saat etkin, sonra kilit ekranında en çok 4 saat (denetçinin belge okuması — ölçülmedi).
- **B-5 — tarayıcı ve uygulama arasında talep girişi farkı:** tarayıcıda K4 (iki gösterim) ve liste yok; uygulamada 7 günlük liste. Kötüye
  kullanımın sınırı pencere + müdür onayı + bant dışı onay adımıdır (ADR 0032 §C.4).
- **B-6 — İngilizce tarayıcı:** tarayıcı tap ekranı çevrilmez (K-16 (4)).
- **B-7 — kırpılmış hatırlatma kişi çalışırken düşebilir** (denetimde ölçüldü: 20'nin 1'i); kesimi vardiyanın ortasına düşen işletmede her
  akşam — sorun kesim ayarıdır (ADR 0032 §A.7), metin koşullu olduğu için yanlış bir şey iddia etmez.
- **B-8 — vardiya bağı:** hatırlatmayı vardiyaya bağlamak vardiya girmeyi, o da geç kalmayı, K3 ön dolgusunu ve onay bandını açar (K-15).
- **B-9 — iş günü anahtarı kapalıyken hiçbir şey kurulmaz:** tahmin kapısı kapalıyken (`TAPPA_BUSINESS_DAY_FROM` etkin değil) `presence` ve
  `shift` null (`apphome.go:107-112`) — hatırlatma, sayaç ve vardiyam o işletmede görünmez.
- **B-10 — `day` sunucunun *şimdi*sine göre:** tarayıcıda kartı gösteren dokunuşa göreydi (adlandırılmış fark).

## 7. Açık / çelişki — cevaplarla ya da kartlarla kod arasında (hiçbiri bu ADR'de düzeltilmedi)

- **Ç1 — Q50 (5) (b) ile kod çelişir.** Cevap: onaylanan saat Saatlerim açıkken görünür. Kod: `POST /app/v1/claims` `Sent`'i **her zaman**
  `showHours=false` ile çağırır (`appclaims.go:152-154`); yorum *"that switch does not exist yet"* der — ama Saatlerim anahtarı
  (`staff_hours_weeks`, 00117) ertesi gün `5fef28a9`'da geldi ve `/me` onu zaten okuyor (`apphome.go:119-121`; sorgunun yorumu da
  *"until it exists, never"* der — `punchclaims.sql:257-258`). Sonuç: `approvedAt` hep null; M16-43'ün [G43-l] maddesi handler düzeyinde
  sağlanmıyor. Domain ve testi doğru. Sahibi: M16-43 düzeltme turu.
- **Ç2 — Q53 (4) (a) Android'de kısmi.** Cevap: kaydırılan sayaç aynı giriş boyunca geri gelmez. Kod: kapatılan girişin anahtarı yalnız
  **bellekte** (`StaffNotifications.kt:163-167`, *"memory only"*; `PresenceSync.kt:48-50`) — süreç ölünce aynı giriş için bir sonraki
  senkronda sayaç geri gelir. Sahibi: M16-47 (ya da kullanıcı bedeli kabul eder).
- **Ç3 — Q54 (1) (a) ve (2) (a)'nın kaydı yok.** Türkçe Android'de `main`'de (state.md ledger M16-49) ve 328 kaydın tamamını, tap ve sonuç
  ekranları dahil, çevirir. Dil listesinin (*"ayrıca sorulur"*) kullanıcı kararı ve onaycının onay sayısı/tarihi (M16-49 madde 5) plan
  belgelerinde bulunamadı; `values-tr` başlığı onayı bir **şart** olarak anar (`values-tr/strings.xml:2`), kayıt olarak değil. M16-49'un
  *"onaysız dil sevk edilmez"* kuralıyla çelişebilir — kayıt varsa kabulde eklenir.
- **Ç4 — CLAUDE.md §9 ile kod çelişir.** §9 tap ve sonuç ekranının tarayıcıyla **"aynı metin"** olmasını ve iki platformun tek metin listesini
  paylaşmasını ister (`CLAUDE.md:598`). Android tap/sonuç Türkçe (`values-tr` `tap_clock_in` *"Giriş yap"*), iOS tap/sonuç yalnız İngilizce
  (`TapCopy.swift`'te çeviri yok; Türkçe yalnız `StaffCopy` ve `LiveCopy`'de), tarayıcı İngilizce. K-17'nin CLAUDE.md değişikliği
  uygulanmadı; M16-49 madde 6'nın platform paritesi tap ekranlarında yok.
- **Ç5 — M16-43 kartı ile kod (Q değil).** (a) `claim.submitted`'in `detail`'inde `"via":"app"` yok (`submit.go:41-49`) — M16-42'nin (i)
  tarifi ve M16-29 pilotunun sayımı buna dayanıyordu; uygulamadan gelen talep audit'te tarayıcınınkinden ayırt edilemez. (b) açık öğede
  `venue` yok (`appclaims.go:55-66`). (c) `/app/v1/hours`'ta `records[].claimable` yok; yerinde talebin **durumu** `claim` var
  (`internal/handler/apphours.go:62-70`) — Q50 (1) (a) seçeneğinin parantezindeki *"Saatlerim'in açık kayıt satırından da"* girişi kurulmadı.
- **Ç6 — iOS'ta *"or one you choose"* doğrulanmadı.** Ayarlar satırı *"The phone's language, or one you choose"* der (`StaffCopy.swift:168`);
  iOS'un uygulama başına dil seçeneği uygulama birden çok yerelleştirme bildirdiğinde görünür — projede `knownRegions` yalnız `en`, `Base`
  (`project.pbxproj:384-387`), `.lproj` yok. Ölçülmedi; doğruysa satır iOS'ta yanlış bir şey vaat eder.
- **Ç7 — iki widget farklı sınır kullanır.** Android widget'ı `staleAt`'e kadar *"at work"* der (`PresenceWidget.kt:31`, `:49`), iOS widget'ı
  ve iki platformun kilit ekranı `showUntil`'e kadar (`Shared/PresenceShare.swift:16-17`, `:56-68`). Tarayıcıdan çıkıştan sonra Android
  widget'ı iş günü sonuna (en çok 18 sa) kadar yanlışlıkla *"at work"* diyebilir — Q53 (3) (a)'nın kilit ekranı için seçtiği sınırdan uzun.
- **Kartın yapılmamış maddeleri.** Canlı taban **yeniden ölçülmedi** (M16-42 madde 1 *"yeniden ölçer"* der; bu belge koordinatörün talimatıyla
  denetimin 2026-11-19 sayılarını kullanır). §9 adımının (M16-42 madde 3) kaydı yalnız kod yorumundadır (*"the user's §9 answers of
  2026-11-23"*, `strings.xml:276-278`); plan belgelerinde yok. ADR 0032'nin append notu (madde 5) bu ADR'nin kabulüyle yazılır (§10).
  `UNLocationNotificationTrigger` için R2'de birebir kalıp yok; tetikleyicinin istediği bölge türleri (`CLCircularRegion`, `CLBeaconRegion`)
  R2'dedir (`redline-check.sh:511`).

## 8. Ölçülmeyenler

Apple ve Android belge okumaları (Live Activity 8 + 4 saat; Android 8–13'te süren bildirimin kaydırılamaması, 14+'ta kilit açıkken
kaydırılması ve *"Clear all"*'ın kapatmaması; kesin olmayan alarmın gecikmesi) · M16-40'ın gecikme ölçümü · onaycılar ve onay kaydı · KF/KM
kadrosunun dilleri (M8-07 envanteri dil sormaz) · iOS uygulama başına dil ekranı (Ç6) · canlı tabanın yeniden ölçümü.

## 9. Karar verilmedi

Tarayıcıya talep formu (Q50 (3) (b)) ve tarayıcı çevirisi (Q54 (4) (b)) — *Sonra*, ayrı kartlar · roster ve *"next shift"* · uygulamanın tap
yanıtının talep kartını taşıması (M16-07 devri (3), *Sonra*) · §3'ün üç eki ve §7'nin çelişkileri — kabulde.

## 10. Uygulama

- **M16-42** (bu ADR): kabul; **ADR 0032'ye append notu** kabul tarihiyle (satır silinmez): *"Uygulama girişi (ADR 0050, TARİH): K4 sonuç
  ekranının sorma sınırıdır ve uygulamanın kendi açılan ekranına uygulanmaz; K4'ün ilk gösterimin bir sonraki kayıt olmasıyla dolaylı
  sağladığı koruma — hâlâ açık girişe talep yok — `Open` ve `Submit`'te açıkça kurulur."* `TestClaimDB_TheCardIsShownOnTwoTapsOnly`'nin
  ölçtüğü sonuç ekranı kuralı değişmez.
- **M16-43 … M16-49:** kod yazıldı (state.md ledger: M16-43 `AppClaimsPath`; M16-45 `6ef5be6f`, 00116; M16-46 `62c6d6d1`; M16-47 `250bd852`;
  M16-48; M16-49 `values-tr`); denetim durumları ledger'da ölçülmedi. Ç1 ve Ç5 → M16-43; Ç2 ve Ç7 → M16-47; Ç3, Ç4, Ç6 ve K-17 → M16-49.
- **M16-40:** Faz G turu (hatırlatma, Android gecikme eşiği, vardiyam, kilit ekranı) bu ADR'nin kabulünden sonra.

## 11. Kabul

**2026-11-28 — kullanıcı kabul etti** (ana oturumun sorusu: *"ADR 0050'yi kodda zaten olan üç ekle birlikte kabul ediyor musun?"* → *"kabul ediyorum"*).

- **K-1 … K-17** olduğu gibi kabul edildi; K-17'nin iki CLAUDE.md cümlesi §2 (v)'deki sözcüklerle onaylandı ve M16-49'un commit'inde uygulanır.
- **§3'ün üç eki kabul edildi:** E-1 (hatırlatmanın *"Remind me in an hour"* / *"Open Taptime"* eylemleri), E-2 (iki platformda ana ekran
  widget'ı), E-3 (iOS'ta kapatılan Live Activity'nin bir sonraki `/me`'de geri gelmesi).
- **§7'nin çelişkileri:** soru cevabı koddan üstündür — her biri §10'daki sahip kartta koda düzeltilir (Ç1, Ç5 → M16-43; Ç2, Ç7 → M16-47;
  Ç3, Ç4, Ç6 → M16-49); kabul bunlardan hiçbirini bedel olarak kabul etmez.
- ADR 0032'ye §10'daki append notu aynı gün yazıldı.
