# ADR 0032 — İş günü ve yön zinciri: açık giriş yalnız kendi iş gününde kapanır

- **Durum:** Önerildi (2026-10-21)
- **Karar veren:** kullanıcı, 2026-10-21 — fazlar A/B/C ve iki CLAUDE.md §9 onayı (tap ekranında
  tahmini yön + düzeltme bağlantısı; sonuç ekranında eksik yarı talep kartı). Bu ADR'nin kullanıcıya
  ayrıca sunduğu üç uygulama önerisi: §A.5–§A.6 (iş günü anının satırda dondurulması ve onu okuyan
  tek eşleme yüklemi), §B.3 (her iki düğme açık yön gönderir) ve §B.3 (yorumlanamayan `want` 400 değil
  *"istek yok"*).
- **İlgili:** CLAUDE.md §4.2, §4.3, §4.5, §4.6, §5 (*"Yön"*, *"Unutulan çıkış"*), §6, §9 ·
  [ADR 0006](0006-debounce-iki-kosullu-zaman.md) (beyan edilen zaman, advisory kilit) ·
  [ADR 0008](0008-practice-satiri-ve-yon-zinciri.md) (yön zinciri, practice) ·
  [ADR 0009](0009-onay-karari-geri-alinamaz.md) (karar geri alınamaz) ·
  [ADR 0011](0011-duzeltme-satiri-yalnizca-kisaltir.md) (düzeltme yalnız kısaltır) ·
  [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) (tap/sonuç ekranı slotları) ·
  [ADR 0027](0027-holding-ve-capraz-tenant-dokunusu.md) §7 (zincir işverende, K-36) ·
  [ADR 0028](0028-dis-api-anahtar-akis-webhook.md) (punch akışı, K-6) · Q01, Q18 ·
  plan [M15](../plan/m15-is-gunu-ve-yon.md)

## Bağlam

### Ölçülen kusur

`internal/domain/tap/decide.go:533-544` `resolveDirection`:

```go
open := in.LastOpenIn
if open == nil || open.Practice {
    return TypeIn, ""
}
dir = TypeOut
if in.Now.Sub(open.OccurredAt) > StaleOpenIn {   // 18 sa
    note = staleOpenInNote
}
```

Pencere **yok**: açık bir giriş varsa — ne kadar eski olursa olsun — bu dokunuş `out`'tur. `StaleOpenIn`
yalnız not düşer. Sorgu `GetLastOpenTransaction` (`db/queries/transactions.sql:16-84`) da tarihsizdir.
M4-04 bunu bilinçli seçmişti: *"never silently in"* — unutulan çıkışı not ve anomali listesiyle
yüzeye çıkarmak için. Bedeli ölçülmemişti; kullanıcı ölçtü (2026-10-21, *"girerken out
yapabiliyor"*):

| Gün | Gerçek | Kaydedilen |
|---|---|---|
| Pzt 09:12 | giriş | `in` |
| Pzt 17:30 | çıkış — dokunmadı | — |
| Sal 09:05 | giriş | **`out`** (Pzt'yi kapatır, stale notu) |
| Sal 17:40 | çıkış | **`in`** |
| Çar 09:00 | giriş | **`out`** |

**Bir** unutulan çıkış zinciri **kalıcı olarak** ters çevirir. Rapor (`internal/domain/ledger`
`accumulate`) kaydedilmiş yönü sırayla eşlediği için ters zincir gece aralıklarını sayar: Sal 17:40 →
Çar 09:00 = 15 sa 20 dk, `closingTail` (= `StaleOpenIn`, 18 sa) altında → **ödenir**. Hata kendiliğinden
düzelmez; ancak kişi bir gün bir dokunuşu daha unutursa zincir yeniden döner.

### Neden takvim günü bunu çözmüyordu

§5 takvim gününü bilerek reddetti: Rusty Bar 18:00–02:00 vardiyasında 02:10 çıkışı önceki akşamın
18:05 girişiyle eşleşmeli. Takvim günü filtresi bu eşleşmeyi koparır. Sorun "gün yok" değil, **günün
sınırının gece yarısında olmasıydı**.

## Karar

### Faz A — iş günü

**A.1 Kural.** Bir dokunuşun yönü şöyle hesaplanır. `at` dokunuşun anıdır (§A.4); `open` kişinin `at`
anında açık olan son girişidir (practice değil).

| Durum | Yön | Not |
|---|---|---|
| `open` yok (ya da practice) | `in` | — |
| `open.OccurredAt` > `at` (sıra dışı beyan) | `in` | — |
| `at − open.OccurredAt` > `StaleOpenIn` (18 sa) | **`in`** | *"previous check-in left open"* |
| `open.OccurredAt` < `at`'in iş günü başlangıcı | **`in`** | aynı not |
| aksi hâlde | `out` | — |

Eski giriş açık kalır ve *"çıkış eksik"* anomalisidir (§A.6). Hata **tek iş gününe** hapsolur: ertesi
dokunuş yeniden doğru `in`'dir.

**A.2 İş günü.** Bir iş günü, **gün dönümü saatinde** (yerel duvar saati) başlar ve bir sonraki gün
dönümüne kadar sürer. Kademe: **tenant varsayılanı → departman → çalışan**, en özel kazanır;
varsayılan **05:00**. Şema: `tenants.day_cutoff time NOT NULL DEFAULT '05:00'`,
`departments.day_cutoff` ve `employees.day_cutoff` (NULL = devral); etkin değer
`coalesce(çalışan, departman, tenant)` tek sorguda (`GetEmployeeForTap`) okunur. Saat dilimi
**tenant'ınkidir** (`tenants.timezone`): kodda tek saat dilimi o — Q01'in `locations.timezone`
override'ı kararlaştırılmış ama **hiç uygulanmamıştır** (sütun yok, 2026-10-21 ölçüldü). Zincir
kişinin zinciridir; holding'de **işverenin** kesimi ve saat dilimi uygulanır (ADR 0027 §7; K-36 iki
üyenin saat dilimini eşit tutar).

Rusty Bar 18:00–02:00 vardiyasında 05:00 dönümüyle 02:10 çıkışı Pzt 05:00'te başlayan iş gününün
içindedir → Pzt 18:05 girişini kapatır. Fabrika gece vardiyası 22:00–06:00 için 05:00 vardiyanın
**içine** düşer (06:05 çıkışı yeni iş günüdür → `in`); bu departman bir override ister (ör. 14:00).
Ayar ekranı kaydetmeden önce uyarır (§A.7).

**A.3 Saflık ve tek temsil.** Yön **hesaptır, politika değildir** (§5): `internal/policy`'e anahtar,
guardrail ya da baseline eklenmez. `tap.Decide` saf kalır. Brief *"saat dilimi/DST çözümü çağıranda"*
diyordu; ölçüm öncülü düzeltti: Decide **bugün de** saat dilimi çözüyor — geç kalma
`shiftStartInstant` içinde `time.LoadLocation` yapar ve `time/tzdata` paket içine gömülüdür
(`tzdata.go`). Saflık (saat yok, DB yok, deterministik) bundan etkilenmez. Bu yüzden `Input`'a
önhesaplı bir UTC an değil **eşdeğeri** girer: `DayCutoff{Cutoff, Timezone}`; sınır `tap` paketinde
**tek dışa açık saf fonksiyonla** hesaplanır (`at`'in yerel günü için `time.Date(y, m, d, hh, mm, 0,
0, loc)`, sonuç `at`'ten sonraysa bir gün geri). Gerekçe repodaki tekrar eden derstir (`MinutesLate`,
`StaleOpenIn`, `TrustBase`): aynı sınırı ortak eşleme yüklemi (§A.6), talep penceresi (§C.3), ayar uyarısı (§A.7) ve
`GET /t` tahmini (§B.2) de ister; çağıranda hesaplanan bir an her birinde ayrı bir kopya olurdu.
Çağıranın işi değeri **okumak**tır, hesaplamak değil.

`DayCutoff == nil` (kesim verilmemiş — M4 çağıranları ve mevcut testler) ya da bölge yüklenemiyor →
yalnız `StaleOpenIn` kuralı; motor 05:00 **uydurmaz** (varsayılan DB'nin DEFAULT'udur).

**A.4 Hangi an: `occurred_at`.** `at` = `tapInstant(in)`: istemci bir an beyan ettiyse
`OccurredAt`, etmediyse `Now` — geç kalmanın bugün kullandığı fonksiyon. Bugünkü `StaleOpenIn`
karşılaştırması `in.Now` ile yapılıyor; bu ADR onu da `at`'e taşır. Ve `GetLastOpenTransaction`
bir `@at` üst sınırı alır (*"o an açık olan giriş"*; canlı dokunuşta `@at = now` → sonuç bugünküyle
aynı).

*Neden beyan edilen an?* M9-01'in çevrimdışı kuyruğu (todo; ama `occurred_at` form alanı
`POST /api/checkin`'de **bugün canlı**) şu şekli üretir: Pzt 17:00 çıkışı çevrimdışı kuyruğa
girdi, Sal 09:00 kişi çevrimiçi `in` yaptı, kuyruk 09:01'de Pzt 17:00'yi gönderdi. Sunucu saatine
göre (`Now`) bu çıkış Sal'ın iş gününe düşer ve yanlış yönlenir; beyan edilen ana göre Pzt'nin açık
girişini kapatır — doğru. `@at` olmadan sorgu Sal 09:00 girişini döndürürdü (*"en son açık"*), dokunuş
ondan **önce** olduğu için §A.1'in sıra-dışı satırı `in` derdi.

*Saldırı/sapma yüzeyi (açıkça):*

| Hamle | Sonuç | Sınır |
|---|---|---|
| Geleceğe beyan | `sys:occurred-at-bound` → `reject`, yön yok | guardrail, kapatılamaz |
| > 72 sa geçmiş | aynı | guardrail |
| 120 sn – 72 sa geçmiş | `base:queued-window` → **`flag`**; müdür görür | baseline (tenant ayarlayabilir) |
| ≤ 120 sn geçmiş, kesimi geçecek biçimde | yön `auto` etiketiyle değişir | yalnız kesimin ±2 dk'sında; Faz B'de yön zaten seçilebilir (`employee` etiketiyle) — kazanç yalnız *etiket* |
| Seçimi yönlendirme (ADR 0006 SELECTION) — eski bir açık girişi seçtirmek | `@at` geçmişe çekilmeli → yukarıdaki satırlar | aynı |

Yönün kendisi saat **yaratmaz** (§A.6: rapor bir çıkışı yalnız kendi iş gününün girişiyle eşler); saat yaratan beyan
edilen anın kendisidir ve o yüzey değişmedi (ADR 0006, M5-05 K1). Sonuç: yeni bir yüzey açılmadı;
kalan pay 120 sn içinde ve yalnız `auto`/`employee` etiketinde.

**A.5 Dondurma: `transactions.business_day_start`.** Her yönlü nfc/qr kararının iş günü başlangıcı
(UTC) satıra yazılır; tarihsel ve manuel satırlarda NULL. Okuyucusu ortak eşleme yüklemidir (§A.6).
Neden dondurulur: kesim sonradan değişebilir, ve geçmişi bugünkü ayarla yeniden hesaplayan bir okuyucu
geçmiş haftanın eşleşmesini değiştirir — `00016`'nın fatura dönemlerinde saat dilimini dondurmasının
gerekçesinin aynısı. Bu yüzden S **yeniden hesaplanamaz** bir kanıttır (§*"Geri alma"*).

`insertParams` yönsüz bir kararda (`Type == nil`) `business_day_start`'ı ve (Faz B) `direction_source`'u
**kodda** NULL'a zorlar; sütunların tutarlılık CHECK'i (`… IS NULL OR type IS NOT NULL`) ikinci
emniyettir, birincisi değil — bir kod hatası CHECK'e çarpıp kaydı 23514 ile düşürmemeli (§4.6).

**A.6 Tek eşleme yüklemi — rapor, anomali ve talep kapısı aynı tanımı kullanır.** Bugün iki tanım var
ve ikisi de Faz A'dan sonra yanlış:

- `ListOpenCheckIns` / `CountMaskedOpenCheckIns`: *"sonrasında herhangi bir `out` yok"*
  (`db/queries/anomalies.sql:701-708`). *"Pzt `in`, Sal `in`, Sal `out`"* şeklinde Sal'ın çıkışı Pzt'nin
  girişini **da** kapatır; *"çıkış eksik"* listeden silinir — A.1'in vaadi tutmaz.
- `accumulate`: sıralı (*"two check-ins in a row leave the first open"*) ve 18 sa'i aşan çifti açık
  sayar (`ledger/report.go:980-988`) ama iş gününü bilmez; Faz B'de çalışanın seçtiği `out` önceki iş
  gününün girişini kapatır (Pzt 15:00 + Sal 07:00 = 16 sa → **ödenir**) ve aynı anda liste o girişi
  kapalı sayar, talep kapısı da onu reddederdi → **0 saat, müdüre hiçbir şey düşmez**.

**Yüklem.** Bir `out`, açık bir `in`'i **kapatır** ⇔ `out.At − in.At ≤ StaleOpenIn` **ve**
(`out.business_day_start IS NULL` **veya** `in.At ≥ out.business_day_start`). Bir `in` **açıktır** ⇔
kişinin ondan sonraki **ilk** yönlü, practice olmayan olayı (`(occurred_at, id)` sırasıyla) onu
kapatan bir `out` değildir. İlk koşul bugünkü `closingTail`'dir (ledger zaten böyle eşliyor); ikincisi
yalnız S'si dolu satırlara uygulanır (tarihsel ve manuel satırlar bugünkü kurala göre eşlenir; ADR
0011'in tablosu değişmez). Motorun `auto` çıkışları ikinci koşulu zaten sağlar (motor yalnız
`open.OccurredAt ≥ S` iken `out` der), yani Faz A'da ikinci koşul yalnız tutarlılık kontrolüdür;
Faz B'de çalışanın seçtiği çıkışlar için **kural** olur. Koruma Faz B'nin POST alanından **önce**
sevk edilir.

**Çıkışın sınıfı — dört ayrık sınıf, her biri tek sayaç.** *"Kapatmıyor"* tek bir şey değildir; bir
`out` (`P` = ondan önceki ilk yönlü, practice olmayan olay) tam olarak bir sınıftadır:

| Sınıf | Koşul | `in` | `out` | Rapor sayacı | Liste |
|---|---|---|---|---|---|
| **kapatır** | `P` bir `in`, S koşulu ve 18 sa koşulu sağlanıyor | kapalı | — | süre (bugünkü gibi) | — |
| **(a) çok uzun** | `P` bir `in`, S koşulu sağlanıyor (ya da S NULL), 18 sa koşulu **düşüyor** | açık | `in`'i tüketir, yetim **değil** | `TooLong` (bugünkü gibi) | *"çıkış eksik"*te `in` |
| **(b) başka iş günü** | `P` bir `in`, S dolu ve `in.At < S` | açık | **yetim** | `OutWithoutIn` | *"çıkış eksik"*te `in`, *"giriş eksik"*te `out` |
| **(c) önsüz, S dolu** | `P` `in` değil (bir `out` ya da yok), S dolu | — | **yetim** | `OutWithoutIn` | *"giriş eksik"*te `out` |
| **(d) önsüz, S NULL** | `P` `in` değil, S NULL | — | dikiş | `StartedEarlier` (bugünkü gibi) | — |

- **(a) ile (b) birlikte düşerse sınıf (b)'dir** (S dolu, `in.At < S` ve > 18 sa). Değerlendirilen
  öneri (a) idi — bugünkü davranışı korur ve saat yaratmaz. Seçilmedi, çünkü **bugünkü davranış bu
  hücrede yoktur**: S'si dolu satır M15'ten önce yazılmadı, yani (a ∧ b) yalnız yeni satırlarda, ve
  motor `auto` bir çıkışta S koşulunu hiç bozmadığı için pratikte yalnız **çalışanın seçtiği** bir
  çıkışta oluşur. İki sınıf da saat yaratmaz ve ikisinde de `in` açık listelenir; fark `out`'tadır:
  (a) seçilirse müdür o çıkışın kendi iş gününün girişsiz olduğunu **görmez**. S koşulu
  daha özgül nedendir: çıkış o girişin iş gününe hiç ait değildir, aradaki sürenin uzunluğu bunu
  değiştirmez.
- **Tarihsel satırlar değişmez:** S NULL bir çıkış yalnız *kapatır*, (a) ya da (d) olabilir — tam
  bugünkü üç dal (`ledger/report.go:964-988`: süre, `TooLong` + `openEntry`, `StartedEarlier`). (b)
  ve (c) yalnız S'si dolu satırlarda vardır; *"giriş eksik"* listesi tanım gereği yalnız onları gösterir.
  Ölçülen tarihsel şekil (Pzt 09:00 `in`, Sal 09:05 stale `out`, S NULL, 24 sa) → (a): `TooLong` 1,
  `StartedEarlier` değişmez, *"giriş eksik"*te değil.
- **Dönem sınırı (yalnız rapor):** ledger dönemden önceki olayları okumaz; dönem içindeki bir (c)
  çıkışının S'si dönem başından önceyse önündeki giriş okunmamış olabilir — rapor onu (d) gibi
  `StartedEarlier` dikişi sayar. Açık `in` başka bir dönemdeyse (bugünkü `continue` dalı) kapatır/(a)
  sınıfında bugünkü gibi hiçbir şey sayılmaz; (b) sınıfında çıkış kendi dönemindeyse `OutWithoutIn`
  sayılır. Anomali listeleri dönem değil satır sorusu sorar; bu ayrım onlarda yoktur.
- Örnek (Faz B): aynı iş gününde 05:30 `in` → ertesi gün 04:30 seçilmiş `out` (kesim 05:00; S koşulu
  sağlanıyor, 23 sa) → yalnız (a). Pzt 15:00 `in` → Sal 07:00 seçilmiş `out` (S = Sal 05:00) → (b).

**İki temsil, bir parite testi.** Sınıflandırma Go'da `tap` paketinde tek saf fonksiyondur (önceki
olayın anı ya da yokluğu, çıkışın anı ve S'si → sınıf; *"kapatır"* yüklemi onun bir sınıfıdır; ledger,
şerit, talep domain'i çağırır) ve SQL'de tek `IMMUTABLE`, `SECURITY INVOKER`, tablo okumayan bir
fonksiyondur (migration 00064; `ListOpenCheckIns`, `CountMaskedOpenCheckIns`, *"giriş eksik"* listesi ve
talep kapısının sorguları çağırır). SQL Go'yu çağıramaz; bu yüzden ikisi bir **parite testiyle**
bağlanır: beş sınıfın her hücresi ve sınırları (`in.At == S`, tam 18 sa) iki tarafta da değerlendirilir,
SQL'deki süre `tap.StaleOpenIn`'den türetilerek karşılaştırılır (`TestGuardrails_TagNotActiveIsAnAllowList`'in
şema-kod bağı deseni).

**`GetLastOpenTransaction` bu yüklemi kullanmaz — kesim sabitken gerekmez.** Yüklemin kapatmadığı
bir giriş, o çıkıştan sonraki her dokunuş için de önceki iş gününde ya da 18 sa'ten eskidir, motor onu
zaten kapatmaz. Bu **kesim sabitken** doğrudur: kesim iki dokunuş arasında değişirse (ör. 05:00 →
14:00) sorgunun *"sonrasında bir `out` var"* dediği bir giriş, yeni kesimle aynı iş gününe düşebilir ve
motor onu kapatmak yerine `in` der. Sınır **muhafazakârdır**: bir çıkış uydurulmaz, saat yaratılmaz,
giriş *"çıkış eksik"* listesinde kalır. Kart bunu özellik testine **kesim değişimi** vakasıyla ekler.

Yan etkiler (doğru yönde, ekran söyler): ADR 0008'in maskelediği girişler ve tarihsel *"stale →
`out`"* satırlarının kapattığı sanılan girişler pencerede **açık** görünür; `CountMaskedOpenCheckIns`'in
*"closed_later"* kolu anlamını yitirir — yeniden yazılır ya da emekliye ayrılır. Liste ile raporun
*"Open"* kümesi aynı pencere için ilk kez aynıdır (`anomalies.sql:647-652`'nin *"meşru fark"* cümlesi
yeniden yazılır); kalan tek fark raporun ilgilendiği dönem dışındaki girişleri raporlamamasıdır.

**A.7 Ayar ekranı.** Tenant varsayılanı Account'ta (owner), override'lar departman ve çalışan
formlarında. Bir kesim bir vardiyanın **`(başlangıç − 1 sa, bitiş + 2 sa]`** aralığına düşerse (gece
vardiyasında gece yarısını aşan aralık) kayıttan önce adlarıyla uyarı ve açık onay; her değişiklik
`audit_log`'a. Paylar erken gelen ve geç çıkan için adlandırılmış sabitlerdir (pilotta ayarlanır).
**Uçlar:** sınır anı **yeni** iş gününe aittir (`at` = kesim → o günün başlangıcı). Bu yüzden alt uç
**hariç**tir: kesim tam `başlangıç − 1 sa` ise en erken beklenen giriş kesimin kendisidir ve yeni güne
düşer — çakışma yok (*KM Central Kitchen* 06:00–14:00 ile 05:00 uyarı vermez). Üst uç **dahil**dir:
kesim tam `bitiş + 2 sa` ise en geç beklenen çıkış yeni güne düşer ve `in` olur — çakışma.
**Ölçülen sonuç:** seed'deki KM departmanları 05:00 ile çakışıyor — *KM Pastry & Bakery* 04:00–12:00
(05:00 vardiyanın içinde), *KM Meat Production* 05:00–13:00 (erken gelen önceki iş gününe düşer)
(`test/fixtures/seed.sql:195-211`). KF'nin vardiyaları (10:00–22:00, Rusty Bar 18:00–02:00) 05:00 ile
çakışmaz. → Q30.

**A.8 Geçmiş.** Geçmiş ters kayıtlar kendiliğinden düzelmez (`transactions` değişmez, §4.3); kural
yayından sonrasına uygulanır. Ters zincirde olan bir kişinin zinciri yayından sonraki **ilk iş günü
sınırında** kendiliğinden düzelir: akşamki yanlış `in` önceki iş gününden kalmıştır → sabah dokunuşu
`in`, akşamki `out`. Geride kalan yanlış `in` anomali listesinde durur; müdür isterse manuel kayıtla
kapatır.

### Faz B — tahmini yön ve düzeltme

**B.1 Ekran** (kullanıcının §9 onayı, 2026-10-21). Tek birincil düğme tahmini yönü söyler: *"Clock
in"* / *"Clock out"* (kullanıcı metinleri İngilizce; bugünkü düğme *"Tap"*, sonuç *"Tapped in /
out"*). Altında küçük metin bağlantısı görünümlü ikinci gönderim: *"Not right? Clock out"* (ya da
*in*). Tek **birincil** düğme kuralı korunur; tenant accent'i yalnız birincil düğmenin zeminidir (ADR
0023 §2 değişmez), bağlantı accent taşımaz. Tahmin yapılamazsa bugünkü *"Tap"* düğmesi, bağlantısız.

**B.2 Tahmin.** `GET /t` açık girişi (`@at = now`), etkin kesimi ve saat dilimini okur ve `tap`'in
dışa açık tahmin fonksiyonunu çağırır — `resolveDirection` de **aynı** fonksiyonu çağırır. Tahmin
tavsiyedir: karar `POST`'ta, advisory kilit altında verilir (ADR 0006). Eş mekân plaketinde tahmin
işverenin zinciridir.

**B.3 Düzeltmenin semantiği — her düğme açık bir yön gönderir.** Tahmin ile POST arasında durum
değişebilir: başka bir sekme ya da cihaz (60 sn sonrası; içinde debounce `ignored` der), iş günü
sınırının geçmesi (04:59:50 GET, 05:00:05 POST), `StaleOpenIn`'in dolması. Üç semantik değerlendirildi:

| Semantik | Bayat sayfada ana düğme | Bayat sayfada bağlantı | Seçildi mi |
|---|---|---|---|
| Ana düğme = *"motoru kabul et"* (alan yok), bağlantı = açık yön | motorun yeni `D`'si kaydedilir — kişi *"Clock out"* gördü, `in` yazıldı | açık yön | ❌ kişinin gördüğüne aykırı kayıt; düzeltmesi 60 sn debounce'a takılır |
| İmzalı tahmin (`ctx`'te `Pred`) + *"çevir"* | — | bayat `Pred`'in tersi = kişinin **istemediği** yön | ❌ çevirme bayat duruma göre niyeti tersine çevirir |
| **Her iki düğme açık yön** (`want=in|out`) | **basılan yön** kaydedilir, `D` farklıysa `employee` | basılan yön | ✅ |

Kural: ana düğme `want=<tahmin>`, bağlantı `want=<tersi>` gönderir (ikisi de `<button type="submit"
name="want">`). Kilit altında motor `D`'yi hesaplar; kaydedilen yön `want`'tır. **Değişmez:
`direction_source = 'employee'` ⇔ kaydedilen yön ≠ kilit altında hesaplanan `D`.** İstek `D` ile
aynıysa `auto` — bağlantıya basılmış olsa bile (durum değişmiş ve motor zaten kabul ediyor). `want`
yoksa (tahminsiz *"Tap"* düğmesi, eski bir sayfa, geliştirme aracı) motor karar verir, `auto`.

*İmzalı bağlama (`ctx`) ne girer?* **Hiçbir şey.** `want`'ın iki değeri de zaten serbesttir (bağlantı
herkese açıktır); imza bayat bir tahmini doğru yapmaz. `tapContextVersion` değişmez.

*Dördüncü seçenek — tahminin de gönderilmesi (`shown=<tahmin>`), değerlendirildi, seçilmedi.* Amacı
bayat sayfada **ana düğmeye** basan dürüst çalışanı (`want == shown ≠ D`) bilerek bağlantıya basan
çalışandan (`want ≠ shown`) ayırmaktı. **İmzasız** `shown` sahtelenebilir: bilerek yön değiştiren biri
`shown=want` gönderip kendini *"bayat sayfa"* gösterir — ayrım denetimde güvenilmez olur, yani hiçbir
şey ayırmaz. **İmzalı** biçimi (`ctx`'te sunucunun bastığı tahmin) güvenilir olurdu, ama bedeli bağlam
sürümünün değişmesi ve üçüncü bir `direction_source` değeri ya da notudur; kazancı, 60–180 sn'lik
bayatlık penceresinde ve çoğunlukla kesim anında oluşan nadir bir vakayı ayırmaktır. İki vakada da
müdürün bilmesi gereken aynıdır: *"kaydedilen yön, karar anında motorun hesapladığından farklı"*.
Pilot bu vakanın gürültü ürettiğini gösterirse imzalı tahmin eklemeli olarak sonradan eklenebilir (yeni
bağlam sürümü + not); M15'te yok.

*`want` alanının yorumu — asla 400 değil (§4.6).* Geçerli bir bağlamla gelen POST fiziksel bir
dokunuştur; `want` yalnız bir tercihtir. Yorumu: bütün `want` değerleri toplanır, **boş olanlar
atılır**; hiç kalmazsa → istek yok (`auto`); kalanların hepsi aynı ve `in`/`out` ise → o yön; çelişen
(`in` + `out`) ya da tanınmayan (`sideways`, `OUT`) değer → istek yok + bir uyarı log satırı (değer
iki-değerli sözlükte değilse loglanmaz, yalnız *"uninterpretable"* olduğu). Bozuk `occurred_at`'in 400
deseni (`handler/checkin.go:194-199`) burada **bilinçli olarak izlenmez**: `occurred_at` kaydın en
önemli alanını değiştirir ve tahmin edilemez; `want`'ın güvenli bir yorumu vardır — motorun kararı —
ve o yorum çalışana sahte bir seçim de yazmaz.

*Form ve `tap.js` (ölçüldü).* Form `preventDefault` + GPS + `form.submit()` ile gönderilir ve
`form.submit()` submitter'ın `name`/`value`'sunu **göndermez**; konum API'si olmayan tarayıcıda ise
betik doğal gönderime izin verir ama `markPending` düğmeyi **eşzamanlı** `disabled` yapar ve
devre dışı bir submitter da değer göndermez (`web/static/js/tap.js:97-103`, `:67-71`). Kural:
1. HTML'de **statik gizli `want` alanı yok** — yalnız iki `<button name="want" value="…">`. JS'siz
   telefonda tarayıcı tek bir `want` gönderir.
2. `tap.js`, `submit` işleyicisinin **ilk işi olarak** — `markPending`'den ve dal ayrımından önce —
   basılan düğmenin değerini (`event.submitter`; yoksa düğmelerin `click` işleyicisinin sakladığı
   değer) **dinamik olarak oluşturduğu** gizli bir `want` alanına yazar, sonra iki düğmeyi birlikte
   devre dışı bırakır. Devre dışı düğmeler değer göndermediği için iki yolda da (doğal gönderim ve
   `form.submit()`) tam bir `want` gider. Betiğin gizli alanı **tektir**: varsa yeniden kullanılır ve
   değeri güncellenir (bfcache'ten geri yüklenen sayfa ya da ikinci gönderim ikinci bir alan
   üretmez); yine de iki eşit değer gelirse yorum kuralı onu tek değer sayar.
3. Değer hiç yakalanamazsa (çok eski tarayıcı) `want` gitmez → motor karar verir; kayıt düşmez.
GPS çağrısı tek kalır (§4.2).

**B.4 Kayıt.** Verdict, güven puanı, kanıt değişmez — `ok` `ok` kalır, `flag`'e düşürülmez (kullanıcı
kararı). Nota (ve çalışanın ekranındaki `EmployeeNote`'a) *"direction chosen by employee"* eklenir.
`transactions.direction_source text` (`auto` | `employee`), yalnız INSERT. **Tarihsel satırlar NULL**,
geri doldurma yok: değişmez satıra yeni bir iddia eklenmez; okuyucu kuralı yönlü bir nfc/qr satırında
NULL'u *"M15 öncesi, motorun seçimi"* okur (o tarihten önce düzeltme yolu yoktu). Manuel satırlarda
NULL (yönü müdür yazdı, `channel` bunu söyler). İstek politika bağlamına girmez (kararı beslemez).
Reject / ignored / redirect'te istek düşer ve `insertParams` iki sütunu da NULL yazar (§A.5).

**B.5 Saat şişirme yolu ve sıra.** Faz B'nin açtığı tek yeni bordro riski çalışanın seçtiği bir
`out`'un önceki iş gününün açık girişini kapatmasıdır (Pzt 15:00 açık + Sal 07:00 seçilmiş `out` =
16 sa < 18 sa). Bunu kapatan şey arayüz değil §A.6'nın ortak yüklemidir ve **Faz A'da** sevk edilir;
yolu açan şey de arayüz değil `POST`'un `want` alanıdır. Sıra: yüklem (Faz A) → yetim çıkış anomalisi ve
rapor sayacı (Faz B'nin ilk kod kartı, `want`'tan önce) → motor → `POST` alanı → arayüz.

### Faz C — eksik yarı talebi

**C.1 Akış.** Talep kayıt **değildir**. Sonuç ekranında, dokunuş kaydı yazıldıktan sonra, isteğe
bağlı **tek** kart (kullanıcının §9 onayı — *"buton YOK"* kuralına istisna):

- *Çıkış eksik* — karar önceki iş gününün (ya da eski) açık girişini açık bıraktıysa: *"Yesterday you
  clocked in at 09:12 but there's no clock-out. What time did you leave?"*; saat seçici çapanın
  çözülmüş vardiya bitişiyle dolu.
- *Giriş eksik* — çalışan `out` seçti ve kapatılabilir giriş yoktu: *"What time did you come in?"*,
  vardiya başlangıcıyla dolu.
- İkisi birden mümkünse giriş talebi gösterilir (bugünkü, taze olan); dünkü açık giriş anomali
  listesinde kalır. Kart atlanabilir (*"You can skip this."*); atlama düğmesi yok.

**C.2 Veri.** İki append-only tablo, ikisi de §6'nın beşlisiyle (tenant_id, ENABLE + FORCE RLS,
`NULLIF` politikası, tenant_id önde indeks, GRANT SELECT/INSERT, REVOKE UPDATE/DELETE,
`tappa_forbid_mutation`):

- `punch_claims` — çalışan, **çapa** (`anchor_transaction_id`: açık `in` ya da yetim `out`, bileşik FK),
  `kind` (eksik yarı: `in` | `out`), `claimed_at`, `created_at`; `UNIQUE (tenant_id,
  anchor_transaction_id)` — kayıt başına bir talep.
- `punch_claim_decisions` — `claim_id` (UNIQUE), `outcome` (`approved` | `rejected`), `decided_by`,
  `approved_at` (*Edit* ile farklı olabilir), `transaction_id` (onayın yazdığı manuel satır), `note`.

Tek tabloda `status` UPDATE'i değil, ayrı karar tablosu: ADR 0009'un *"karar bir kez yazılır"* deseni
(`transaction_reviews`). `transaction_reviews`'in kendisi kullanılamaz — tetikleyicisi yalnız
`verdict='flag'` satırına karar kabul eder (`00005:347-352`) ve talep bir satır değildir.

**C.3 Kısıtlar.**
- **Oturuma ve tek kayda bağlı:** kart, yeni bir MAC etiketiyle imzalı bir *talep bağlamı* taşır
  (`tapcontext.go` deseni: oturum id'si ek doğrulanmış veri, sunucu saatiyle `IssuedAt`, 15 dk) —
  içinde çapa id'si ve tür. İstemcinin gönderdiği çapa id'sine güvenilmez; INSERT çapa satırından,
  `tenant_id` + `employee_id` yüklemiyle kurulur (yabancı çapa 0 satır).
- **Pencere:** çıkış talebi `(in.At, min(sonraki yönlü olay, çapanın iş gününün sonu, şimdi)]`;
  giriş talebi `[max(önceki yönlü olay, çapanın iş günü başlangıcı), out.At)`. İş günü `tap`'in tek
  fonksiyonundan. Pencere < 24 sa olduğundan bir duvar saati tek bir ana çözülür (sonbaharın iki kez
  yaşanan saatinde erken olan).
- **Çapa hâlâ eksik mi — §A.6'nın yüklemiyle**, başka bir tanımla değil: çıkış talebinin çapası
  *açık* bir `in` olmalı (sonraki ilk yönlü olay onu kapatan bir `out` değil), giriş talebinin çapası
  *yetim* bir `out`. Örnek: Pzt 15:00 `in`, Sal 07:00 çalışanın seçtiği `out` (S = Sal 05:00) → Pzt
  açık (talep edilebilir, pencere Sal 05:00'e kadar), Sal 07:00 yetim (o da talep edilebilir).
- **Gönderim ve karar advisory kilit altında** (`LockEmployeeForTap`), pencere ve çapanın durumu
  **iki kez** doğrulanır: gönderimde ve onayda (arada durum değişir).
- **Log:** talep yalnız opak id'lerle loglanır; e-posta, oturum, bağlam loglanmaz (§7).

**C.4 Karar.** Müdürün kuyruğunda (*"Ahmet: yesterday out 23:30 — [Approve] [Edit] [Reject]"*),
bugünkü flag kuyruğunun yanında ikinci bölüm. **Onay** (ya da *Edit*) tek işlemde: `channel='manual'`
satır (`internal/domain/manual`'ın yazımı — işlem alan bir varyanta bölünür, davranışı değişmez;
`entered_by` = onaylayan, mekân = çapanın mekânı), karar satırı, `audit_log`. Öz-onay yasak
(`00005`'in kuralının aynası).

**Askı ([ADR 0025](0025-tenant-askiya-alma.md)).** Karar yolu bir panel yazmasıdır ve `mountWriting`'in
**ana grubuna** (`ProtectWriting`, `internal/handler/dashboard.go:166`; flag kararı `:167`'nin yanı)
gider: askı talep kararını durdurur. ADR 0025'in askının durdurmadığı kapalı listesi (*"kaydın dört
yolu"*, `0025:46-53`, `:87`; kardeş grup `dashboard.go:309`, manuel kayıt `:330-331`) **genişletilmez** —
telafi yolu zaten oradadır: manuel kaydın iki adımı askıda açık kalır, talep satırı ve çapanın anomalisi
yerinde durur, hiçbir şey kaybolmaz. Çalışanın talep gönderimi (`POST /api/claim`) bir tap yüzeyidir ve
askıyı bilmez (`0025:49-53` — tap gibi).

**ADR 0011 ile çatışma yok — ölçümün kendisiyle:** çıkış talebi Q18'in kendi vakasıdır (*"kapanmamış
in 09:00 + müdür out 17:00 → 8 sa, açık 0"*, ADR 0011 *"Ürünün asıl senaryosu ETKİLENMİYOR"*). Giriş
talebi yetim bir çıkışın önüne bir `in` koyar; ADR 0011'in tuzağı ancak arada **yanlış bir `in`**
varsa kurulur ve pencere (*"önceki yönlü olaydan sonra"*) bunu dışarıda bırakır. Hiçbir onay bir
aralığı uzatamaz.

**C.5 Ret, süre, deaktivasyon.**
- **Ret:** karar + audit; anomali kalır, müdür yine manuel kayıt girebilir. Aynı çapaya ikinci talep
  yok.
- **Süre dolması:** talep, `claimed_at` `manual.MaxBackdate` (90 gün) dışına çıkınca karar verilemez —
  manuel yazım zaten reddeder (tek temsil); kuyruk *"expired"* der. Satır yazılmaz, iş (job) yok.
- **Artık gereksiz:** çapa başka yoldan kapandıysa onay reddedilir, kuyruk *"already resolved"* der,
  yalnız Reject sunar.
- **Deaktive çalışan:** bekleyen talep karara açık kalır (iş deaktivasyondan önce yapıldı; §4.6),
  kuyrukta işaretli. Deaktive çalışan yeni talep gönderemez (§5 satır 4 dokunuşu reddeder; kart çıkmaz;
  gönderim ayrıca durumu denetler).

## Geri alma (Down) — kanıt silinmez

`business_day_start` yeniden hesaplanamaz (§A.5: kesim değişmiş olabilir), `direction_source` hiç
hesaplanamaz (çalışanın seçimi yalnız o satırda yazılı), talepler ve kararları birer beyan ve karar
kaydıdır. Üç migration'ın `Down`'ı, düşüreceği sütunda NULL olmayan bir değer ya da tabloda bir satır
varsa **reddeder** (`RAISE EXCEPTION … USING ERRCODE = 'object_not_in_prerequisite_state'`, hiçbir şey
değişmez) — emsal `00043`'ün `Down`'ı (`00043_record_a_tap_on_a_peers_plaque.sql:430-433`: site
taşıyan satır varsa reddeder). Kesim ayar sütunları (`day_cutoff`) yapılandırmadır, kanıt değildir;
geçmişi `audit_log`'dadır ve `Down` onları düşürebilir. **İlk dokunuştan sonra geri dönüş yolu
şemayı değil binary'yi geri almaktır:** eski kod yeni sütunları yazmaz (NULL) ve okumaz; sütunların
CHECK'leri NULL'a izin verir.

## Değerlendirilen alternatifler

| Alternatif | Neden reddedildi |
|---|---|
| **Takvim günü** (gece yarısı sınırı) | Rusty Bar 02:10 çıkışını önceki akşamın girişinden koparır — §5'in reddettiği hata. Gün dönümü takvim gününün **sınırı kaydırılmış** hâlidir. |
| **Sabit 18 sa pencere** (`StaleOpenIn`'i tek kural yapmak: eşikten eski açık giriş → `in`) | Kullanıcının **tam** vakasını çözer (Pzt 09:12 → Sal 09:05 = 23 sa 53 dk > 18 sa), ama vardiyalar arası boşluk 18 sa'ten kısaysa çözmez: geç vardiya Pzt 16:00 girişi, unutulan çıkış, Sal 09:00 erken vardiya → 17 sa → yine `out` ve zincir yine döner. Pencereyi kısaltmak (ör. 12 sa) uzun çift vardiyayı keser ve yine bir vardiya ölçüsü varsayar. 18 sa iş gününün **ikinci** şartı olarak kalır (çok uzun aralık vardiya değildir). |
| **Vardiyaya göre en yakın eşleşme** (dokunuşu en yakın vardiya başı/sonuna ata) | Vardiyası olmayan çalışanda (Q17: departman > lokasyon, ikisi de boş olabilir) karar verilemez; esnek ve çift vardiyada en yakın uç sık yanlış; vardiya değişince geçmişin anlamı değişir; ve yön, geç kalmanın bugün yalnız rapor olan vardiya çözümüne bağımlı olur. Gün dönümü vardiyadan bağımsız tek bir ayardır; vardiya yalnız **uyarıda** kullanılır. |
| **Yönü politika yapmak** (`internal/policy`'e bir yön kuralı) | §5: yön hesaptır. Politika bir **verdict** üretir; yön her verdict'in altındaki aritmetiktir. Tenant'ın değiştirebildiği şey bir **parametredir** (kesim), kural değil. |
| **Decide'a önhesaplı UTC iş günü anı** | Geçerli ve saf; ama sınırı ledger, talep penceresi, ayar uyarısı ve `GET /t` da ister — dört çağıranda dört kopya. Decide zaten bölge çözdüğü için (geç kalma) eşdeğerini (kesim + bölge) vermek saflıktan bir şey kaybettirmez. |
| **Sunucu saatine (`Now`) göre iş günü** | Çevrimdışı kuyruğun dünkü çıkışını bugüne düşürür (§A.4). Beyan edilen anın riski zaten guardrail + baseline ile sınırlı. |
| **İş gününü raporda bugünkü ayardan yeniden hesaplamak** (sütun yok) | Kesim değişince geçmiş haftanın eşleşmesi değişir; `00016` aynı gerekçeyle saat dilimini donduruyor. |
| **Tarihsel satırlara `direction_source='auto'` doldurmak** (`DEFAULT`) | Yönsüz satırlarla çelişir, değişmez satırlara yeni bir iddia ekler; okuyucu kuralı aynı bilgiyi ücretsiz verir. |
| **Düzeltmeyi `flag`'e düşürmek** | Kullanıcı kararı (verdict değişmez). Saat şişirme riski §A.6'nın yüklemiyle ledger'da kapanır, kuyruğu doldurmadan. |
| **Talepleri `transaction_reviews`'e yazmak** | Tetikleyici yalnız `flag` satırı kabul eder; talep bir satır değildir. |
| **Onaylı talebi doğrudan kayıt saymak (müdürsüz)** | Q18: kayıt bir insan beyanına dayanır ama **çalışanın kendi** beyanı kayıt değildir — mesai kaydı hukuki delil olabilir. |

## Sonuçlar

**Olumlu.** Unutulan çıkış tek iş gününe hapsolur; ters zincir yayından sonraki ilk sınırda düzelir;
gece vardiyası (Rusty Bar) aynen çalışır; anomali listesi ile rapor ilk kez aynı *"açık"* tanımını
kullanır; çalışan yanlış tahmini dokunduğu anda düzeltir; eksik yarı, ertesi sabah taze bellekle ve
müdürün tek tıkıyla kapanır.

**Bedeller.**
- Kesim ayarı yanlışsa (vardiyanın içinde) Faz A o departmanda bugünkünden **kötü**dür: her gün bir
  yanlış `in`. Uyarı ve canlı çakışma raporu (M15-07) bunu yayından önce yakalamak içindir; KM'nin
  seed verisi zaten çakışıyor (Q30).
- Tap ekranının golden'ları bilinçli olarak yeniden üretilir (§9 kararı); sonuç ekranının kartsız
  golden'ları bayt-aynı kalır.
- `transactions`'a iki sütun (metadata-only `ADD COLUMN`), iki yeni tablo, bir yeni POST yüzeyi.
- `CountMaskedOpenCheckIns`'in anlamı değişir; anomali ekranındaki sayılar ilk yüklemede artar
  (maskelenmiş eski girişler görünür olur) — doğru yönde ama müdüre açıklanmalı.

## Kırmızı çizgi analizi

| Çizgi | Etki |
|---|---|
| **§4.1 biyometri** | Yok. |
| **§4.2 GPS** | **Değişmez.** `GET /t` tahmini ve sonuç ekranındaki talep kartı konum okumaz. Düzeltme bağlantısı aynı formun ikinci gönderimidir; `tap.js`'in **tek** `getCurrentPosition` çağrısından geçer. Talep POST'u konum alanı taşımaz. |
| **§4.3 immutable** | `transactions`'ta UPDATE/DELETE yok; yeni sütunlar yalnız INSERT'te yazılır, tarihsel satırlar NULL kalır (geri doldurma yok). `Down` migration'ları dolu bir kanıt sütununu ya da talep satırını düşürmeyi reddeder (§*"Geri alma"*). Onay = yeni manuel satır + `audit_log`. `punch_claims` ve kararları append-only (REVOKE + tetikleyici). Geçmiş ters kayıtlar düzeltilmez. |
| **§4.4 atomik ctr** | Dokunulmaz (`advance` aynı, kilitli bölümün dışında). |
| **§4.5 tenant izolasyonu** | İki yeni tablo §6 beşlisiyle; bütün yeni sorgular açık `tenant_id` filtreli; talep INSERT'i çapa satırından tenant + çalışan yüklemiyle; talep bağlamı oturuma bağlı MAC. Holding'de zincir, kesim ve talep işverenindir (ADR 0027 §7); saha tenant'ı talep görmez. `tenants.day_cutoff` için kolon düzeyinde GRANT (`00024`'ün daralttığı kolonlar kapalı kalır). |
| **§4.6 kayıt kaybolmaz** | Yön kuralı hiçbir dokunuşu düşürmez (yalnız `in`/`out` seçer). Yeni kısıtlar yalnız sözlük ve tutarlılık kontrolüdür — *"yönlü her satırda dolu"* CHECK'i **konmaz**, çünkü kod yolunda bir kez tutmayan CHECK INSERT'i 23514 ile düşürür (aynı şekil `transactions_tag_fk` için 23503 ile ölçülmüş — `internal/domain/tap/types.go:364-373`: 500, kayıt yok). Talep akışının hiçbir hatası yazılmış dokunuşu etkilemez; talep durumu okunamazsa kart çıkmaz, sonuç ekranı yine çizilir. Açık kalan giriş sessizce kapanmaz (Q18) ve artık listeden de silinmez (§A.6). Yorumlanamayan bir `want` alanı 400 değil *"istek yok"*tur (§B.3) — fiziksel dokunuş bir tercih alanı yüzünden düşmez. Yönsüz kararda yeni sütunlar kodda NULL'a zorlanır, CHECK ikinci emniyettir (§A.5). Sessiz onay yok: düzeltilmiş yön işaretli, talep müdür onayı ister. |
| **§4.7 sırlar** | Yeni sır yok. Talep bağlamı tap bağlamı gibi sır içermez (çapa id'si, tür, an); oturum token'ı ve e-posta loglanmaz; `audit_log` `detail`'i yalnız anlar ve id'ler. |

## CLAUDE.md metin önerileri

CLAUDE.md bu ADR ile **değişmez**; her paragraf onu uygulayan kodla **aynı commit'te** değişir
(plan M15'in kartları), çünkü §5 *"kod bundan sapıyorsa kod yanlıştır"* der.

**§5 — *"Yön (in/out)"* maddesinin yerine** (M15-03; ikinci cümle M15-10):

> - **Yön (in/out):** kişinin **son açık girişi** (dokunuşun anında açık olan) **aynı iş
>   günündeyse** ve `StaleOpenIn`'den (18 sa) yeniyse bu dokunuş onu kapatır (`out`); değilse bu
>   dokunuş `in`'dir ve eski giriş *"çıkış eksik"* anomalisi olarak kalır (notla) — hata tek iş
>   gününe hapsolur. **İş günü takvim günü DEĞİLDİR:** gün dönümü saatinde (yerel duvar saati,
>   varsayılan 05:00; kademe tenant → departman → çalışan, en özel kazanır) başlar; tenant saat
>   diliminde ve dokunuşun **beyan edilen** anında hesaplanır (ADR 0032). Rusty Bar 18:00–02:00
>   vardiyasında 05:00 dönümüyle 02:00 çıkışı akşamın girişini kapatır; dönümü kesen bir gece
>   vardiyası (fabrika 22:00–06:00) departman override'ı ister — ayar ekranı uyarır. Yön **hesaptır**,
>   politika değildir.
>   Çalışan tap ekranında tahmini yönü değiştirebilir: kayıt istediği yöndür, verdict değişmez,
>   `direction_source='employee'` + not ve raporda ayrı görünür; rapor bir çıkışı yalnız kendi iş
>   gününün girişiyle eşler.

**§5 — *"Unutulan çıkış"* maddesine eklenecek cümle** (M15-17):

> Çalışan ertesi iş gününün ilk dokunuşunda sonuç ekranından eksik yarıyı **talep** edebilir
> (`punch_claims`); talep kayıt değildir, müdürün onayı `channel='manual'` kayıt + `audit_log` yazar.

**§9 — *"Çalışan tap ekranı kutsaldır"* paragrafına** (M15-12 ilk cümle, M15-17 ikinci):

> Sayılı istisnalar (kullanıcı §9 kararı, 2026-10-21 — ADR 0032): tek birincil düğme tahmini yönü
> söyler (*"Clock in"* / *"Clock out"*) ve altında küçük bir *"Not right? …"* bağlantısı vardır;
> onay ekranında, yalnız eksik bir yarı varsa, tek bir talep kartı ve onun *"Send to my manager"*
> düğmesi vardır. Başka her değişiklik yine önce sorulur.

**§3 — dizin haritasına** (M15-16): `internal/domain/claim/` — *eksik yarı talebi: pencere, gönderim,
müdür kararı (onay → manuel kayıt). ADR 0032.*

## Bulunan riskler (brief'te yoktu)

1. **İki ayrı "açık" tanımı** (§A.6) — anomali listesi, rapor ve talep kapısı farklı tanım kullanırsa
   Faz A'nın *"anomali kalır"* vaadi tutmaz ve Faz B'de çalışanın seçtiği çıkış unutulan girişi
   listeden ve talepten siler (0 saat, müdüre hiçbir şey). Tek yüklem, iki temsil, parite testi.
2. **KM kesim çakışması** (§A.7, Q30) — Faz A KM'de ayarsız sevk edilirse zararlıdır.
3. **`tap.js` ve form** (§B.3) — `form.submit()` submitter'ı göndermez; konum API'siz yolda devre
   dışı submitter değer göndermez; statik bir gizli `want` alanı JS'siz telefonda ikinci bir `want`
   üretir. Ret kuralı bunu bir kayıt kaybına çevirirdi; yorum kuralı çevirmez.
4. **Override ile saat şişirme** (§A.6, §B.5) — yüklem Faz A'da, `POST` alanından önce.
5. **API akışı** (Q31) — çalışanın seçtiği yön `/v1` punch akışına **işaretsiz** gider ve dış sistem
   (KF-RMS / KM-ERP) kendi eşlemesini yapıyorsa §A.6'nın koruması orada yoktur; onaylı talepler
   (manuel) akışa hiç girmez (`punch_events_channel CHECK (channel IN ('nfc','qr'))`, K-6).
6. **Eski davranışı pinleyen testler** — yeni 18 sa kuralı kesim **yokken de** geçerlidir, bu yüzden
   *"stale → `out`"* şeklini pinleyen her test ters iddiayla yeniden yazılır: `decide_test.go:597-634`
   (iki test), `crosslocation_test.go:197-237` (not sırası *"kural; başka şube; yön"*u stale `out`
   üzerinden pinleyen iki test — sıra yeni notla yeniden pinlenir), `internal/handler/checkin_db_test.go:1808-1830`
   (*"ok out closing a stale check-in"* alt testi). `StaleOpenInNote` sabiti tarihsel notlar ve eş mekân
   notu (`ledger/site.go:164`) için korunur; onu sabit **görünüm girdisi** olarak kullanan testler
   (`result_test.go:1018-1032`, `unbranded_golden_test.go:152` ve golden'ı
   `result-ok-out-restaurant.golden`, `ledger/partnernote_test.go:36`, `ledger/holding_db_test.go:825-832`)
   motoru çalıştırmadığı için **değişmez** — tarihsel bir satırın notunu çizmeye devam ederler.
7. **Düzeltme debounce'a takılır** — kişi sonuç ekranında yanlış yönü görüp 60 sn içinde yeniden
   dokunursa `ignored` olur (ADR 0006); düzeltme **basmadan önce** tap ekranındadır, sonradan değil.
   Sonradan düzeltme müdürün manuel kaydıdır (ya da Faz C talebi).
8. **ADR numarası** — bu ADR ilk taslakta 0031'di; M14'ün dalı (`origin/docs/m14-identity-plan`,
   `2e593ef`, kabul `573731d`) `docs/adr/0031-tek-kimlik-cok-isletme.md`'yi taşıyor (2026-10-21
   ölçüldü). 0032 bütün dallarda boş. M14 migration numarası ayırmıyor (`000NN` yer tutucusu); M15'in
   00064–00066'sı hiçbir dalda yok.
9. **`Down` kanıtı silerdi** (§*"Geri alma"*) — reddeden `Down`.

## Ne garanti edilmiyor

- **"On the floor" ve saatlik şeridin okuma ufku** (`ledger/floor.go:122`, `tape.go:110`) 18 sa kalır
  (şeridin **eşlemesi** `tape.go:178` ortak yükleme geçer — §A.6):
  Pzt 12:00'de açık kalan giriş Sal 05:00 dönümünden sonra Sal 06:00'ya kadar *"zeminde"* sayılır.
  Sunum farkıdır; kayıt ve bordro etkisi yoktur.
- **Kesimin ±2 dk'sında** (`base:queued-window` altında) beyan edilen an yönü `auto` etiketiyle
  değiştirebilir (§A.4).
- **Yanlış ayar** uyarıyla onaylanırsa motor ona uyar.
- **Talep sonucu çalışana bildirilmez** (Q32).
- **Geçmiş** düzelmez (§A.8).
