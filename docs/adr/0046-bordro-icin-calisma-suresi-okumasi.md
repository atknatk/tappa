# ADR 0046 — Bordro için onaylı çalışma süresi okuması (`GET /v1/timesheets`)

- **Durum:** Önerildi (2026-11-14), denetim bekliyor.
- **Tarih:** 2026-11-14 · **Görev:** M11 API-14 · **Issue:** atknatk/tappa#36 · **Dal:** `api-timesheets`
- **Karar sahibi:** kullanıcı, 2026-11-14 (issue #36: *"Phase 1: hours → payslip"* — Malta Calculator Taptime saatlerini Malta bordrosuna çevirecek).
- **İlgili:** CLAUDE.md §4.3 (kayıt değişmez), §4.5 (tenant izolasyonu), §4.6 (kayıt kaybolmaz, sessiz onay yok), §4.7, §5 (yön zinciri, gece vardiyası, unutulan çıkış, manuel kayıt), §6 (UTC, float yok) · ADR 0028 (dış API; §2 yetki kuralı, §8 ortak sözleşme) · ADR 0027 §9 (holding: süre işverendedir) · ADR 0032 §A.6 (çıkış sınıflandırması) · ADR 0042 (vardiya kayıtta donar)
- **Ölçüldü** = bu dalda, şerit veritabanı `tappa-ts-db` (goose 103), testle. **Okundu** = dosyadan, çalıştırılmadı.

## Bağlam

Mevcut `/v1` okumalarının hiçbiri bordroya uygun değil (okundu):

- **Punch akışı** (`GET /v1/punches`) tanım gereği manuel kayıtları ve düzeltmeleri içermez: rehber §6.1 *"a manual record a manager enters in the Taptime panel — manual records are your system's"* (`docs/api/integration-guide.md:632`) ve §6.3 *"For hours, Taptime's report is the record"* (`docs/api/integration-guide.md:723`). Punch'ları saf eşleyen bir bordro yanlış öder: rehberin kendi örneği 16 sa ↔ 0 sa (`docs/api/integration-guide.md:724-726`).
- **Rapor motoru** (`internal/domain/ledger`) doğru sayıları zaten hesaplıyor, ama yalnız panel için ve yalnız **7 günlük** bir dönem için: `ReportDays = 7` (`internal/domain/ledger/report.go:53`), `Period.Days()` sabit döner (`report.go:180`), `Reader.Hours` dönemi `WeekOf` ile kurar (`report.go:490`).

Malta Calculator'ın ihtiyacı: bir bordro dönemi için, işletmenin **kendi** her çalışanının **yerel iş günü** başına sayılan çalışma süresi — panel raporunun saydığının **aynısı**.

## Karar

### 1. Uç nokta: salt okuma, motorun kendisi

`GET /v1/timesheets?from=YYYY-MM-DD&to=YYYY-MM-DD[&limit=…][&cursor=…]`, diğer `/v1` okumalarıyla **aynı zincirde**: host kapısı, başlıklar, gövde sınırı, guard, Bearer zinciri (biçim → adres kovası → çözümleyici → anahtar kovası), `writesTakeNoQuery` (GET'e dokunmaz) — `Surface.Mount`'un kimlikli grubu (`internal/handler/api/surface.go`). Askıya alınmış işletmede **açıktır** (okuma; ADR 0028 §10, punch ve directory ile aynı).

- **Yazmaz.** `transactions`'a, `punch_events`'e ya da başka bir tabloya hiçbir satır yazılmaz; okuma tek bir `WithTenant` işleminde koşar ve ilk ifadesi `db.BoundStatements(APIReadTimeout)`'tur (diğer API okumalarının sınırı, `internal/domain/tenant/apiread.go:22`).
- **Migration yok.** İki yeni sqlc ifadesi (`db/queries/transactions.sql` → `ListTimesheetShiftEvents`, `db/queries/employees.sql` → `ListTimesheetEmployees`), şema değişikliği yok.
- **İkinci bir eşleştirme algoritması yok.** Satırlar `ledger.events()` ile eşlenir ve `ledger.accumulate()` ile toplanır — panel raporunun `Reader.Hours`'unun çağırdığı iki fonksiyonun aynısı (`report.go:528`, `report.go:538`). Yön zinciri (kişi başına sıralı tek geçiş, takvim günü değil — `report.go:919-923`), uç durumları (`endpointState`, `report.go:879`), çıkış sınıflandırması (ADR 0032 §A.6, `report.go:964-982`) ve gün ataması (`dayIndex(in.At, …)`, `report.go:1178`) değişmez.

### 2. Motorun genişletilmesi: 7 günden keyfi (≤ 31) tam yerel gün aralığına

- `Period`'a gün sayısı eklendi (`days`, dışa kapalı). `Days()` onu döndürür; sıfırsa (elle kurulmuş eski bir `Period{}` literal'i) `ReportDays`'i döndürür, yani panelin her yolu bit bit aynıdır. `WeekOf` gün sayısını açıkça 7 yazar.
- Yeni `ledger.RangeOf(first, last Date, zone)` dönemi `WeekOf`'un yaptığı gibi **yerel gece yarılarından** kurar: `From = first 00:00 (zone)`, `To = (last + 1 gün) 00:00 (zone)` — `AddDate` ile, `+24h` ile değil (DST günü 23/25 saattir). `last < first` ya da 31 günden uzun aralık `ErrTimesheetRange`. Aynı Pazartesi–Pazar için `RangeOf` ile `WeekOf` aynı `From`/`To`/`FirstDay`/`Days`'i verir (test: `TestRangeOf_AWeekIsWeekOf`).
- Okuma penceresi raporunkiyle aynı: `[From, To + closingTail)`, `closingTail = tap.StaleOpenIn` (`report.go:165`). Dönemin son gecesinde başlayan gece vardiyası böylece **kapanır** ve **giriş gününe** yazılır; dönemden önce başlayıp dönemde biten vardiya önceki dönemindir (`StartedEarlier`, API'de görünmez). Rapor kuralının kendisi — ikinci kural yok.
- Sayılan dakika kuralı değişmez: `ok` uçlar ve **onaylanmış** flag'ler sayılır; **bekleyen** flag sayılmaz, ayrı (`Awaiting`); **reddedilen** flag sayılmaz; practice satırları sorgudan düşer (`NOT t.practice`); manuel kayıt (`channel='manual'`) sıradan bir uç olarak sayılır.

### 3. Sözleşme

**İstek:**

| Parametre | Kural | Ret |
|---|---|---|
| `from`, `to` | İkisi de zorunlu. `YYYY-MM-DD`, gerçek bir takvim günü, işletmenin saat diliminde **yerel iş günü**; ikisi de **dahil**; `to ≥ from`; `to − from + 1 ≤ 31` | 400 `invalid_request`, `details.parameters: ["from","to"]`, `details.maxDays: 31` |
| `limit` | Sayfadaki **çalışan** sayısı; 1–1000, varsayılan 500 (§3.2, `parseLimit`) | 400 `invalid_request`, `details.parameter: "limit"` |
| `cursor` | Önceki sayfanın `nextCursor`'u, **aynı** `from`/`to` ile | 400 `invalid_request`, `details.parameter: "cursor"` |
| başka her ad, tekrar, çözülmeyen sorgu | — | 400 `invalid_request`, `details.allowed: ["from","to","limit","cursor"]` |

`x-error-codes`: `invalid_request`, `body_too_large`, `unauthorized`, `api_disabled`, `rate_limited`, `unavailable` — `GET /v1/directory`'ninkilerin aynısı. Yeni hata kodu yok; yeni `details` anahtarı tek: `maxDays`.

**Cevap 200** (issue'nun alan adları, tipleri birebir):

```json
{
  "period": {"from": "2026-11-02", "to": "2026-11-08", "timeZone": "Europe/Malta"},
  "complete": false,
  "truncated": false,
  "data": [
    {
      "employee": {"id": "emp_…", "externalRef": "E-1042", "employeeCode": "1042", "firstName": "Jane", "lastName": "Example", "fullName": "Jane Example"},
      "workedMinutes": 2400, "awaitingMinutes": 0, "shifts": 5, "openEntries": 0, "checkInMissing": 0, "manualShifts": 1,
      "days": [{"date": "2026-11-02", "workedMinutes": 480, "manual": false, "open": false, "checkInMissing": false}]
    }
  ],
  "nextCursor": null,
  "hasMore": false
}
```

| Alan | Kaynak (ledger) | Anlam |
|---|---|---|
| `period.from`, `period.to` | istek | dahil, yerel |
| `period.timeZone` | `tenants.timezone` (yüklenemezse raporun düşüşü: `UTC`) | günlerin okunduğu dilim |
| `workedMinutes` | `PersonHours.Worked` | sayılan süre, **tamsayı dakika, aşağı yuvarlanmış** (`Worked / time.Minute`); float yok |
| `awaitingMinutes` | `PersonHours.Awaiting` | müdür onayı bekleyen süre, aynı yuvarlama |
| `shifts` | `PersonHours.Shifts` | `workedMinutes`'ın arkasındaki aralık sayısı |
| `openEntries` | `PersonHours.Open` | çıkışsız girişler (raporun `Open`'ı); süreleri hiçbir alana girmez |
| `checkInMissing` | `PersonHours.OutWithoutIn` | girişi olmayan çıkışlar (raporun *Check-in missing*'i) |
| `manualShifts` | `PersonHours.ManualShifts` (**yeni**) | `shifts`'in, en az bir ucu müdürün yazdığı (`channel='manual'`) olanları |
| `days[]` | `Daily[d]`, `Marks[d]` | dönemin **her** yerel günü, sırayla (uzunluk = gün sayısı) |
| `days[].workedMinutes` | `Daily[d]` | o güne yazılan sayılan süre (vardiyanın **başladığı** gün), aşağı yuvarlanmış |
| `days[].manual` | `DayMarks.ManualShift` (**yeni**) | o güne yazılan sayılan vardiyalardan birinin bir ucu manuel |
| `days[].open` | `DayMarks.Open` | o gün açılmış bir giriş çıkışsız kaldı |
| `days[].checkInMissing` | `DayMarks.InMissing` | o günün bir çıkışının girişi yok |

- **Yuvarlama:** her rakam kendi kesin toplamının tabanıdır. Günlerin toplamı `workedMinutes`'tan gün başına bir dakikadan az eksik olabilir; bordronun ödeyeceği `workedMinutes`'tır.
- **`complete`** issue'nun kuralıdır, **sayfaya** uygulanır: bu sayfadaki hiçbir çalışanın onay bekleyen süresi, `openEntries > 0`'ı, `checkInMissing > 0`'ı yok **ve** `truncated` yanlış. Bekleyen süre **kesin** süreden okunur (`PersonHours.Awaiting > 0`), aşağı yuvarlanmış dakikadan değil: yarım dakikalık bekleyen bir onay `awaitingMinutes: 0` gösterir ama yok sayılmaz (`internal/handler/api/timesheets.go`, `listTimesheets`). Bir dönem, yürüyüşün **her** sayfası `complete: true` dediğinde tamdır. Okuma o anın kayıtlarını anlatır: sonradan yazılan bir kayıt (müdürün manuel kaydı, onaylanan flag, henüz bitmemiş bir dönemin yeni dokunuşu) cevabı değiştirir — bordro düzenlenmeden hemen önce yeniden okunur.
- **`truncated`** raporun `Truncated`'ıdır (okuma bütçesi `ReportEventCap`, `report.go:127`), çalışan başına daraltılmış: sayfa bir çalışanın kayıtları ortasında kesilirse o çalışan sayfadan **çıkarılır** ve sonraki sayfa ondan başlar (yarım zincir yanlış bir toplamdır — `report.go:58-63`). `truncated: true` yalnız **tek bir çalışanın** kayıtları tek başına bütçeyi aşarsa olur; o zaman o çalışanın rakamları bir **tabandır** ve `complete` yanlıştır (§4.6: kayıt atılmaz, gösterilir).
- **Kim listelenir:** dönem içinde raporun saydığı ya da listelediği bir kaydı olan kendi çalışanları — raporun `People`'ı. Dönemde kaydı olmayan çalışan listede yoktur (0 demektir). Deaktive edilmiş çalışan, dönemde kaydı varsa listelenir.
- **Sıra ve sayfalama:** Taptime çalışan `id`'sine göre artan. Sayfa en çok `limit` çalışan taşır; okuma bütçesi yüzünden daha azını taşıyıp `hasMore: true` diyebilir. `nextCursor` tam olarak `hasMore` doğruyken doludur (`GET /v1/directory`'nin kuralı). İmleç `tms_<uuid>_<16 hex>`: son çalışanın id'si ve `(işletme, from, to)`'nun özeti — başka bir işletmenin ya da başka bir aralığın imleci 400 (history ve directory imleçlerinin bağlama kuralı, `internal/handler/api/history.go`, `directory.go`).

### 4. Dışlamalar

- **Kanıt ve kişisel veri asla:** IP, GPS, not, kural gerekçesi (`matched_sid`/`policy_context`), güven puanı, e-posta. Sorgu bunların hiçbirini seçmez (`ListTimesheetShiftEvents`'in sütun listesi `ListWorkedShiftEvents`'inkiyle **aynıdır** — Go tarafında iki satır tipi arasındaki tür dönüşümü derlenmezse derleme kırılır), çalışan sorgusu `email` seçmez, cevap yapısında alanları yoktur.
- **Yalnız işletmenin kendi çalışanları.** Okuma anahtarın işletmesinin `transactions`'ını okur (`WithTenant` + açık `tenant_id`, RLS). Bir holding ortağının çalışanının bu işletmenin plaketindeki dokunuşu **işverenin** tenant'ına yazılır (ADR 0027 §9) ve burada hiç okunmaz; o süre işverenin `GET /v1/timesheets`'indedir. Bu işletmenin kendi çalışanının ortağın mekânındaki vardiyası (satırı bu işletmede, `site_location_id` dolu) **sayılır** — rapor gibi.
- **Rapordaki şu sayılar API'de yok:** `Refused` (reddedilen flag süresi), geç kalma, mekân bazlı toplamlar, `StartedEarlier`, practice sayısı, diğer mekân işaretleri. İhtiyaç olursa v1 içinde **alan olarak eklenebilir** (ADR 0028 §8, ileri uyum kuralı).

### 5. Issue sözleşmesinden farklar

Zorunlu fark yok. Issue'nun *"if it fits"* diye bıraktığı ve bu ADR'nin seçtiği:

1. **Sayfalama** (`limit`, `cursor`, `nextCursor`, `hasMore`) — rehber §3.2 *"Every list is paged"*. `truncated` ayrıca kalır ve raporun anlamını taşır (yukarıda).
2. **`complete` sayfanındır** — sayfalamanın doğal sonucu (tüm dönem için hesaplamak her sayfada bütün işletmeyi okumak olurdu).
3. **`manualShifts`'in anlamı** issue'da tanımlı değildi. Raporun `ManualArrivals`'ı (*"müdürün yazdığı girişler"*) **bilerek seçilmedi**: `report.go:242-261` o sayının "shift" diye adlandırılmasının iki yönde de yanlış olduğunu ölçmüştür (yazılmış çıkışı saymaz, kapanmamış yazılmış girişi sayar). Bu yüzden ad ne diyorsa o hesaplandı: sayılan vardiyalardan bir ucu manuel olanlar — `accumulate`'in **aynı geçişinde**, sayılan dalda bir sayaç (`report.go`'nun sayılan dalı), yeni bir eşleştirme değil. `days[].manual` aynı popülasyonun gün işaretidir.
4. **Eklenen alan:** `employee.fullName` (string, hiç null değil). Panelde açılmış çalışanın `firstName`/`lastName`'i `null`'dır (`GET /v1/directory` ile aynı); `fullName` olmadan bordro o kişiyi adsız görürdü. v1 kuralı gereği ekleme bir fark değildir.

## Değerlendirilen seçenekler

| Seçenek | Neden |
|---|---|
| **(a) Ledger motorunu keyfi aralığa aç, çalışan imleciyle sayfala** | **Seçildi.** Tek aritmetik; sayfa başına maliyet sınırlı; yarım zincir sayfa sınırına dönüşür. |
| (b) Bütün dönemi her sayfada oku, bellekte sayfala | Her sayfa bütün işletmeyi okur; 31 günlük büyük bir işletme `ReportEventCap`'i aşıp herkesi `truncated` yapardı. |
| (c) API'de punch'lardan ayrı bir eşleştirme | Yasak (ikinci algoritma); manuel kayıtları zaten göremez. |
| (d) Saklanan bir saat tablosu | §4.3: saklı toplam, kaydın belirlediği değerin ikinci kopyası; düzeltilmesi UPDATE ister (`report.go:5-11`). |

## Kırmızı çizgi analizi

§4.1/§4.2 dokunulmadı. §4.3: hiçbir yazma yok; `transactions` yalnız okunur. §4.4: dokunulmadı. §4.5: iki yeni ifade de açık `tenant_id` koşulu taşır, `WithTenant` altında RLS'lidir; her `JOIN` `tenant_id`'yi yeniden söyler; izolasyon RLS testiyle (açık filtresiz) kanıtlanır. §4.6: bekleyen flag süresi atılmaz, `awaitingMinutes` olarak görünür; bütçeyi aşan çalışan atılmaz, `truncated` ile taban olarak görünür; açık giriş ve girişsiz çıkış sayılır ve `complete`'i düşürür. §4.7: kanıt sütunu seçilmez, log'a çalışan verisi yazılmaz.

## Uygulama ve testler (adlarıyla)

**Kod:** `internal/domain/ledger/timesheet.go` (`RangeOf`, `DaysBetween`, `Reader.Timesheet`, `timesheetPage`), `internal/domain/ledger/report.go` (`Period.days`, `PersonHours.ManualShifts`, `DayMarks.ManualShift`, sayılan daldaki sayaç), `internal/handler/api/timesheets.go` (sorgu kuralı, imleç, cevap), `surface.go` (`Config.Timesheets`, rota), `cmd/tappa/api.go` + `main.go` (panelin `*ledger.Reader`'ı verilir), `db/queries/transactions.sql` → `ListTimesheetShiftEvents`, `db/queries/employees.sql` → `ListTimesheetEmployees`, `internal/handler/api/openapi.yaml` (yol, beş şema, `ErrorDetails.maxDays`, `x-limits.timesheetMaxDays: 31`), rehberin iki dili (4. baskı; yeni §9, eski §9–§12 artık §10–§13 — bağlantılar çapa üzerinden, değişmedi).

**Envanterler:** `internal/db/evidencereaders_test.go` (`ListTimesheetShiftEvents` → `residue`, `ListWorkedShiftEvents`'in sınıfı), `internal/db/m11schema_test.go` (`employeeLocationNonReaders`), `internal/domain/ledger/checkout_test.go` (M15-05 öncesi referans kopyaya aynı sayaç eklendi ki tüm-`Report` eşitliği sürsün).

- Parametre ve tel biçimi (DB'siz): `TestTimesheetsQuery_RefusesEveryMalformedRequest` (37 satır, tablo bazlı), `TestTimesheetsQuery_TheRangeRuleIsTheEngines` (handler'ın aralık kuralı = `RangeOf`'unki, −2…33 gün), `TestTimesheets_TheWireShapeIsTheContract`.
- Motor (DB'siz): `TestRangeOf_AWeekIsWeekOf` (olağan hafta ve iki DST haftası; bahar günü 23 sa), `TestRangeOf_RefusesAReversedOrTooLongRange`, `TestRangeOf_ANightShiftOnTheLastDayCountsInFullOnThatDay`, `TestAccumulate_ManualShiftsCountsCountedShiftsWithATypedEnd`, `TestTimesheetPage_TheBudgetIsAPageBoundaryNotAWrongTotal`.
- DB, gerçek zincir (`internal/handler/api/timesheets_db_test.go`): `TestTimesheets_AManualRecordIsCounted` · `TestTimesheets_AnOpenEntryIsOpenNotWorked` · `TestTimesheets_ANightShiftIsAttributedByTheReportsRule` (önceki/sonraki hafta dahil) · `TestTimesheets_AnAwaitingFlagIsNotWorked` (onaylı sayılır, reddedilen hiçbirinde, girişsiz çıkış) · `TestTimesheets_AnotherTenantsEmployeesNeverAppear` (API'de ve RLS'te, açık filtresiz) · `TestTimesheets_APartnersEmployeeIsTheEmployersNotTheVenues` · `TestTimesheets_TheWalkPagesEveryEmployeeOnceAndTheCursorIsBound` · `TestTimesheets_TheWeekIsThePanelReportsWeek` (`Reader.Hours` ile kişi kişi, gün gün: 6 kişi × 7 gün).
- Belge: `TestOpenAPI_TheExamplesAreTheHandlersAnswers` (hikâyenin `listTimesheets 200 kf` ve `km` cevapları), `TestOpenAPI_EveryRouteAndErrorDocumented` (rota sondaları, `maxDays`, `timesheetMaxDays`), `TestGuide_TheTwoLanguagesAreOneDocument`, `TestGuide_EveryEndpointCodeAndLimitIsTheDocuments`.
- **Mutantlar (ölçüldü, bu dalda):** `awaitingMinutes`'ı `workedMinutes`'a katmak → `TestTimesheets_AnAwaitingFlagIsNotWorked` ve `TestTimesheets_TheWeekIsThePanelReportsWeek` kırmızı; okumanın `closingTail`'ini kaldırmak (`UntilAt: period.To`) → `TestTimesheets_ANightShiftIsAttributedByTheReportsRule` ve çapraz kontrol kırmızı. İkisi de geri alındı.

**Seed (API-14 adım 4):** `make seed-timesheets` → `internal/domain/checkin/timesheetseed_db_test.go`, build tag `timesheetseed` (ne `make test` ne bir ikili onu içerir). Geçen haftayı karar motorundan geçirir (motorun saati dokunuşun anına kurulur; debounce'un veritabanı saati gerçek zamanda beklenir); dev/sandbox dışı veritabanını üç kapıyla reddeder; hiçbir şey silmez. Ayrıntı ve sandbox tarifi: `deploy/README.md` → *Sandbox (API-11)* §8.

## Düzeltme turu (2026-11-14, iki merceğin bulguları)

- **`Bound` zorunlu:** `ledger.Reader.Timesheet` sınırsız bir okumayı `ErrTimesheetUnbounded` ile reddeder (`timesheet.go`); handler `tenant.APIReadTimeout` verir. `TestAPIRead_EveryReadCarriesTheBound` artık `Timesheet`'i de sayar ve sıfır sınırın reddini sınar; `db.BoundStatements` çağrısını kaldıran mutant orada kırmızı.
- **`complete`'in her kolu tek başına:** `TestTimesheets_CompleteFallsForEachSignalAlone` (yalnız yarım dakika bekleyen onay, yalnız açık giriş, yalnız eksik giriş, yalnız `truncated`, ve temiz sayfa `true`). Ölçülen mutantlar: bekleyeni yok saymak, eksik girişi yok saymak, açık girişi yok saymak, `truncated`'a bakmamak — dördü de kırmızı.
- **`days[].manual` eşlemesi:** `TestTimesheets_TheDayMarkIsTheShiftsNotTheArrivals` (handler; ayrıca okuyucuya verilen sınır ve sayfa boyu) ve `TestTimesheets_AManualRecordIsCounted`'a dokunulmuş giriş + elle yazılmış çıkış günü; `ManualShift` yerine `Manual` okuyan mutant ikisinde de kırmızı.
- **Dönem sınırındaki girişsiz çıkış:** rehberin §9.1'ine (iki dilde) bir madde — iş günü `from`'dan önce başlamış girişsiz bir çıkış iki dönemin de rakamlarında yoktur; panelin anomali listesi onu satır olarak gösterir (`db/queries/anomalies.sql`, `ListCheckoutsWithoutCheckIn`'in sınır notu).
- **Seed:** bekleme kişi başına, o kişinin son `nfc`/`qr` kaydının **veritabanı saatiyle** yaşından ölçülür (65 sn; `seedWaitOut`); beklenmeyen bir kararda tek mesajla durur (`seedStopped`; 1 sn'lik bekleme mutantında ölçüldü: `ignored (sys:person-debounce)`, yazılan kayıt sayısı, işletmenin neden harcandığı, yeniden başlama yolu); sahip DSN'inin rolü süper kullanıcı ya da `BYPASSRLS` değilse başlamaz.
- **Yerel sandbox:** `make sandbox-local` / `sandbox-local-run` / `sandbox-local-flow` / `sandbox-local-seed` (`scripts/sandbox-local.sh`, `scripts/sandbox-local-flow.sh`); tarif `deploy/README.md` → *Sandbox (API-11)* §9.

## Sağlamlaştırma turu (2026-11-14, 2. denetim)

- **Linux:** `scripts/sandbox-local-flow.sh` `script(1)` kullanmaz; `opadmin`'in stderr'i `python3`'ün `os.openpty()`'siyle açılan bir sözde-terminaldir (`python:3.12-slim-bookworm` ve `python:3.12-alpine` içinde ölçüldü: rc 0, 67 satır SQL, kayıtta tek bağlantı; denetim: stderr bir dosyayken `opadmin` reddeder). Bütün ön koşullar hiçbir şey yazılmadan önce sınanır.
- **argv'de sır yok:** form değerleri `--data-urlencode ad@dosya`, operatör çerezi ve anahtar `-H @dosya`, jeton taşıyan URL'ler `curl -K`; rehber §9 adım 5 `-H @…/api-auth.header`. `ps -eo args` örneklemesinde (80 anlık görüntü) hiçbir sır değeri eşleşmedi.
- **Loopback kapısı:** iki yerde de (`sandbox-local.sh`'ın `loopback`'i, seed'in `seedLoopback`'i) sorgusunda `host`/`hostaddr` olan, birden çok host adlayan ya da `postgres://` olmayan DSN reddedilir; ret vakaları `TestSandboxLocal_RefusesADSNThatIsNotOneLoopbackHost` (cmd/tappa/scripttest; pozitif kontrol ve mutant ölçüldü) ve `TestSeedLoopback_RefusesAHostTheQueryOrAKeywordStringNames` (`-tags timesheetseed`).
