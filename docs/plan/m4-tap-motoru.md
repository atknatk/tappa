# M4 — Tap karar motoru (`internal/domain/tap`)

**Amaç.** Dört kanıtı **olguya** çevirip [policy motoruna](m3-policy-motoru.md)
sunmak, dönen effect'i bir tap kaydına çevirmek. Saf fonksiyon: DB bilmez, HTTP
bilmez, saat okumaz — hepsi girdi olarak gelir.

**Bittiğinde:** [CLAUDE.md](../../CLAUDE.md) §5 tablosunun **her satırı** için
`TestDecide_…` ile başlayan bir test var, kapsam **%90+**.

## Görev ayrımı — policy motoru vs tap motoru

M3 geldikten sonra §5'in yedi satırı artık bu pakette `if` zinciri değil:

| Kim | Ne yapar |
|---|---|
| **`internal/policy`** | *Karar verir.* §5 satır 1–5 guardrail, satır 6–7 baseline politikası. Effect döndürür: `allow`/`review`/`deny`/`ignore`. |
| **`internal/domain/tap`** | *Olguları hesaplar ve kararı uygular.* Bağlam kurar (IP eşleşti mi, GPS mesafesi, ctr sıçraması, çapraz lokasyon mu), motoru çağırır, effect'i `verdict`'e çevirir, **yön** ve **güven puanı** ve **geç kalma**yı hesaplar. |

**Hesap politika değildir.** Yön tayini (son açık girişe göre toggle), trust
formülü (`20+50+30`) ve geç kalma hesabı burada kalır — bunlar müşterinin
değiştireceği kurallar değil, ürünün aritmetiğidir.

> §5 bu paketin ve M3 baseline'ının **ortak normatif metnidir**. Kod ondan
> sapıyorsa kod yanlıştır. Spec'i değiştirmek gerekiyorsa önce CLAUDE.md
> güncellenir ve ADR yazılır.

---

## M4-01 — `internal/geo`: haversine ve yarıçap

- **Bağımlılık:** M0-02
- **Kırmızı çizgi:** §4.2 (GPS yalnızca tap anında)
- **Commit:** `feat(geo): add haversine distance and radius check`

**Amaç.** İki koordinat arasındaki mesafeyi metre cinsinden hesaplamak ve
yarıçap içinde olup olmadığını söylemek.

**Kabul kriterleri.**
- Bilinen mesafelerle test (Malta içi iki nokta, 0 m, antipot uç durum).
- Varsayılan yarıçap config'ten gelir (`TAPPA_GPS_RADIUS_M`, varsayılan 150) —
  paket içinde sabit gömülü değil.
- Sınır davranışı testli ve belgeli: tam 150 m içeride mi dışarıda mı.
- Bu paket **hiçbir** koordinatı log'lamaz.

**Tuzaklar.**
- `float64` mesafe hesabı için uygundur; **para/süre** için değil (§6).
- Enlem/boylam sırasını karıştırmak klasik hata — tipte adlandır (`Lat`, `Lng`),
  çıplak `float64, float64` imzası verme.

---

## M4-02 — Karar girdi/çıktı tipleri

- **Bağımlılık:** M2-07 (Result şekli) · M1-08
- **Commit:** `feat(tap): define decision input and output types`

> **Kart düzeltmesi (2026-07-26, M4-03 uygulaması sırasında).** Bu kartın
> `Input`/`Decision` taslağı iki alan grubunu eksik gösteriyordu; M4-03 gerçek
> `Decide` gövdesi ikisini de gerektirdi:
>
> 1. **`Input`'a `PolicySet policy.Set` alanı eklendi** (ikinci `Decide`
>    parametresi DEĞİL). Neden alan: M4-02 imzası `func(Input) Decision` sabittir
>    ve `types_test.go`'da derleme-zamanı doğrulanır — Set'i Input içine koymak bu
>    imzayı korur ve `Decide`'ı tek değerin saf fonksiyonu bırakır. Set'i **M5
>    çağıranı** kurar: `Guardrails(tenantParams)` + `BaselinePolicies(gerçek
>    version id)` + tenant politikaları (gerçek append-only version id'lerle).
>    Guardrail'ler ve baseline koddur; yalnız version id'leri ve tenant katmanı
>    DB'den gelir → `Decide` DB'ye dokunmaz, saflık korunur (policy saf katman).
> 2. **`Decision`'a `MatchedSid string`, `Layer policy.Layer`, `PolicyVersionID
>    *uuid.UUID` eklendi** — M3-07 migration 0008'in `matched_sid`/`policy_layer`/
>    `policy_version_id` sütunları için (kayıt "neden FLAGGED" sorusunu sonsuza
>    dek cevaplasın). `Decide` bunları `policy.Evaluate`'ten taşır; M5-05 yazar.
> 3. **`Input.Debounce` süperseded** (N3): debounce **penceresi** artık `PolicySet`
>    içindeki `sys:person-debounce` guardrail'inin `Params`'ından uygulanır;
>    `Decide` yalnız **olguyu** (kişinin son tap'inden bu yana geçen saniye)
>    hesaplar ve bu alanı **okumaz**. Alan M4-02 sözleşmesi için korundu; M5
>    aynı değeri `policy.Params`'a da beslemeli (ikisi de bugün 60 sn, drift yok).
>
> `redirect` effect'i (yalnız `sys:no-session`/`sys:tenant-mismatch`) →
> `Redirect=RedirectActivation`, **Verdict boş** (kayıt yok, §5.3). Tenant-mismatch
> M4-03'te ateşlemez (Input tenant id taşımıyor — M5 devri).

> **Kart düzeltmesi (2026-07-26, M4-05 uygulaması sırasında).** Yukarıdaki
> `Decision` taslağına **iki rapor alanı eklendi**; M4-05 (vardiya çözümü + geç
> kalma + çapraz lokasyon) ikisini de gerektirdi:
>
> 1. **`MinutesLate *int`** — çalışanın çözülmüş vardiyasına (`Input.Shift`) göre
>    bir **check-IN**'in kaç dakika geç kaldığı. `nil` = **hesaplanmadı**: vardiya
>    yok, tap bir check-OUT (çıkış "geç" olmaz), ya da zaman dilimi çözülemedi.
>    Pozitif = geç, `<= 0` = zamanında/erken. **Rapor çıktısıdır, `Verdict`'i ASLA
>    etkilemez** (§5, geç gelen yine `ok`). `float` saat DEĞİL, dakika tamsayısı
>    (§6); pointer, çünkü "hesaplanmadı" (`nil`) ile "0 dk geç" ayrı olmalı. Geç
>    kalma **Evaluate'ten SONRA** hesaplanır (yöne bağlı) → bir `Decision` alanı,
>    `policy.Context` anahtarı değil (context Decide'dan dönmüyor; ayrıca hiçbir
>    baseline `time:minutesLate` okumuyor, doğrulandı — verdict'e sızmaz).
> 2. **`CrossLocation bool`** (Q17) — tap edilen lokasyon çalışanın profil
>    lokasyonundan farklı mı (`Employee.Location != Tag.Location`). Aynı olgu
>    `policy.Context`'e (`employee:crossLocation`) de **Evaluate'ten önce** konur ki
>    `base:cross-location-note` (allow) eşleşsin ve olgu `policy_context`'e donsun
>    (M3-07). **Decision alanı da** olmasının sebebi: o baseline allow'u
>    `base:ip-or-gps-ok`'a karşı sid berabere kaybeder, yani karar `Note`'u tek
>    başına çapraz-lokasyonu göstermez — rapor bu alanı doğrudan okur. Tap **tap
>    edilen** lokasyonun vardiyasıyla (`Input.Shift`) değerlendirildiği için
>    çapraz-lokasyon çalışanı "geç" damgası yemez.
>
> **tzdata:** `internal/domain/tap` `time.LoadLocation` çağırdığından paket
> `time/tzdata`'yı **blank import** eder (`tzdata.go`) — sistem tzdata'sı olmayan
> scratch imajda vardiya hesabı sessizce çökmesin. stdlib, yeni bağımlılık yok,
> saflık korunur; `cmd/tappa`'ya (M8) dokunulmadı, embed orada olursa idempotent.

**Amaç.** `Decide`'ın imzasını ve saflığını sabitlemek.

**Tasarım.**
```go
type Input struct {
    Now          time.Time      // cagiran verir; paket icinde time.Now() YOK
    Channel      Channel        // nfc | qr | manual
    SUN          SUNResult      // gecerli mi, supheli sicrama var mi
    Tag          *Tag           // status, location
    Employee     *Employee      // nil = oturum yok; status
    SourceIP     netip.Addr
    LocationIPs  []netip.Prefix
    GPS          *geo.Point
    LocationGPS  *geo.Point
    GPSRadiusM   float64
    LastForPerson *Transaction  // debounce icin
    LastOpenIn    *Transaction  // yon tayini icin
    Shift         *Shift        // departman > lokasyon, cagiran cozer
    Debounce      time.Duration
}

type Decision struct {
    Verdict   Verdict   // ok | flag | reject | ignored
    Type      *Type     // in | out | nil
    Trust     int
    IPMatch   bool
    GPSMatch  bool
    Note      string
    Practice  bool
    Redirect  Redirect  // aktivasyon sayfasi gibi ozel yonlendirmeler
    Security  bool      // guvenlik uyarisi uretilmeli mi
}
```

**Kabul kriterleri.**
- Paket `time.Now()`, `rand`, DB veya HTTP **kullanmıyor** (import listesiyle
  kanıtlanır).
- `Now` girdi olarak geliyor — gece vardiyası testleri deterministik olsun.
- `Decision` bir karar açıklıyor, kayıt **yazmıyor**.

**Tuzaklar.**
- Girdi şişer diye alanları `interface{}` ardına gizleme; açık alan listesi bu
  motorun test edilebilirliğinin tamamı.
- `Employee == nil` ile `Employee.Status == deactivated` **farklı** kararlar
  üretir (§5 satır 3 ve 4). Tek alana sıkıştırma.

---

## M4-03 — `Decide()`: bağlam kurma ve kararın uygulanması

- **Bağımlılık:** M4-02 · **M3-04** (değerlendirici) · **M3-06** (baseline)
- **Kırmızı çizgi:** §4.6 (kayıt kaybolmaz) · §5
- **Commit:** `feat(tap): build policy context and apply the decision`

**Amaç.** Kanıtları `policy.Context`'e çevirmek, `policy.Evaluate` çağırmak,
dönen effect'i `verdict`'e eşlemek.

**Effect → verdict eşlemesi.** `allow → ok` · `review → flag` · `deny → reject` ·
`ignore → ignored`. `sys:no-session` kararı özel: **kayıt yazılmaz**, aktivasyon
sayfasına yönlendirilir (§5 satır 3 — tek istisna).

Karar `MatchedSid`, `Layer` ve `PolicyVersionID` ile birlikte kayda geçer
([M3-07](m3-policy-motoru.md)) — docket "neden FLAGGED" sorusunu cevaplayabilsin.

**Motorun uyguladığı §5 tablosu** (referans; normatif metin CLAUDE.md §5):

| # | Koşul | Sonuç | Katman |
|---|---|---|---|
| 1 | Etiket `lost` veya `retired` | `reject` | guardrail `sys:tag-not-active` |
| 2 | SUN geçersiz (CMAC uyuşmuyor **veya** `ctr` artmadı) | `reject` | guardrail `sys:sun-invalid` |
| 3 | Oturum yok / geçersiz | aktivasyon sayfası (**kayıt yok**) | guardrail `sys:no-session` |
| 4 | Çalışan `deactivated` | `reject` + denemeyi logla + güvenlik uyarısı | guardrail `sys:employee-deactivated` |
| 5 | Aynı **kişi** 60 sn içinde tekrar | `ignored` | guardrail `sys:person-debounce` |
| 6 | IP eşleşti **veya** GPS < 150 m | `ok` (IP yoksa not: `verified via GPS`) | **baseline** `base:ip-or-gps-ok` |
| 7 | ikisi de yok | `flag` — kayıt alınır, onay kuyruğuna düşer | **baseline** `base:no-evidence-review` |

Satır 1–5 **kapatılamaz**. Satır 6–7 tenant tarafından değiştirilebilir; hiç
politika eşleşmezse motor `review` döner — sessiz onay imkânsız (§4.6).

**Kabul kriterleri.**
- Satır 1–5 guardrail olarak, 6–7 baseline olarak çözülüyor; her satır için ayrı
  `TestDecide_…` (M4-07).
- Satır 3 **kayıt yazdırmaz** — tek istisna budur, `Redirect` döner.
- Satır 7 asla `reject` değildir: kanıt yetersizse kayıt alınır (§4.6).
- Hiçbir dalda "sessiz onay" yok, hiçbir dalda kayıt düşmüyor.

**Tuzaklar — sıralama sömürülebilir.**
- Deaktive hesap kontrolü SUN'dan **önce** gelirse bilgi sızar (geçersiz linkle
  hesap durumu öğrenilir).
- Debounce SUN'dan **önce** gelirse replay penceresi açılır.
- Bu iki hatayı `tappa-security-auditor` R8 arar.

---

## M4-04 — Yön tayini (in/out)

- **Bağımlılık:** M4-03
- **Commit:** `feat(tap): resolve direction from last open check-in`

**Amaç.** Bir tap'in giriş mi çıkış mı olduğunu doğru belirlemek.

**Kural.** Kişinin **son açık girişine** göre toggle — **takvim gününe göre
DEĞİL**. Açık giriş varsa `out`, yoksa `in`.

**Kabul kriterleri.**
- Rusty Bar senaryosu testli: 18:05 `in` → ertesi gün 02:10 `out`, doğru girişle
  eşleşiyor.
- Gün ortasında birden çok in/out çifti doğru sıralanıyor.
- Açık giriş çok eskiyse (ör. çalışan çıkış yapmayı unutmuş, 3 gün önce) davranış
  **belirlenmiş ve testli**: öneri → yine `out` üretilir ama `note` ile işaretlenir;
  rapor bunu anomali olarak gösterir. Sessizce `in` üretme.
- `practice = true` kayıtlar yön zincirine **katılmaz**.
  - 🔴 **BU KRİTER M4-04'te KARŞILANMIŞ SAYILDI AMA KARŞILANMAMIŞTI** (eklendi
    2026-08-02, M5-11 / [ADR 0008](../adr/0008-practice-satiri-ve-yon-zinciri.md)).
    Practice satırı zincire **katılmıyordu**, doğru; ama zincirin geri kalanını
    **gizliyordu**: `GetLastOpenTransaction` tek satır döndürüp practice'e kör
    olduğu için, sadece daha yeni sıralanan bir practice satırı altındaki gerçek
    açık girişi görünmez yapıyor ve sonraki tap `out` yerine `in` oluyordu — hiçbir
    sinyal olmadan. Kusur M5-09'da bulundu, M5-11'de sorguya `AND NOT t.practice`
    eklenerek kapatıldı. Kriter değişmedi; **artık gerçekten doğru.**

**Tuzaklar.**
- "Bugünün kayıtları" filtresiyle çalışmak en yaygın gece vardiyası bug'ıdır.
  Sorgu `GetLastOpenTransaction` — tarih penceresi yok, sıra `occurred_at DESC`
  (ve **2026-08-02'den beri** `AND NOT t.practice`, ADR 0008).
- Tüm karşılaştırmalar UTC; yerel saate çevirme yalnızca render'da (§6).

---

## M4-05 — Vardiya çözümü ve geç kalma

- **Bağımlılık:** M4-04 · Q01
- **Commit:** `feat(tap): resolve shift and compute lateness`

**Amaç.** Geç kalmayı çalışanın **kendi** vardiyasına göre hesaplamak.

**Kural.** Departman vardiyası varsa **o**, yoksa **tap edilen lokasyonun**
vardiyası (Q17 — profildeki lokasyon değil). KM'de departman bazlı (Meat
Production 05:00, Pastry 04:00…), KF'de lokasyon bazlı.

**Çapraz lokasyon (Q17).** Tap lokasyonu çalışanın profil lokasyonundan farklıysa
kayıt yine `allow` alır, `employee:crossLocation = true` bağlamıyla
`base:cross-location-note` politikası nota işler ve rapor bunu ayrı gösterir.
Zincirde şube değişimi normaldir — St Julians (10:00) personeli Paceville'de
(11:00) çalışırken "geç kaldı" damgası yememeli.

**Kabul kriterleri.**
- KM Pastry & Bakery çalışanı 04:00 vardiyasına göre değerlendiriliyor, KM
  General'in 09:00'una göre değil.
- `overnight = true` vardiyada (Rusty Bar 18:00–02:00) geç kalma doğru hesaplanıyor.
- Vardiya saatleri `time` + tenant/lokasyon zaman dilimi (Q01) ile o günün
  UTC anına çevriliyor; DST geçişi testli (Malta'da mart/ekim).
- Geç kalma bir **rapor** çıktısıdır — `verdict`'i etkilemez. Geç gelen kayıt
  yine `ok`'tur.

**Tuzaklar.**
- Yaz saati: `Europe/Malta` UTC+1/+2. Sabit ofset gömme, `time.LoadLocation`
  kullan (Go'nun gömülü tzdata'sı için `time/tzdata` import'u gerekebilir — tek
  binary'de sistem tzdata'sı olmayabilir).
- Geç kalmayı `float` saat olarak tutma (§6); süre `time.Duration` veya dakika
  tamsayısı.

---

## M4-06 — Trust puanı, QR kanalı, practice tap

- **Bağımlılık:** M4-03
- **Commit:** `feat(tap): score trust and handle qr and practice channels`

> **Kart düzeltmesi (2026-07-26, M4-06 uygulaması sırasında).** `entered_by`
> doğrulaması **M5 yazma yoluna devredildi** — `Input`'a `EnteredBy` alanı
> EKLENMEDİ. Gerekçe: (1) `Decide`'ın imzası sabittir (`func(Input) Decision`,
> `types_test.go`'da derleme-zamanı doğrulanır) → **hata döndüremez**; (2) `entered_by`
> bir *köken* (provenance) alanıdır, karar girdisi değil — verdict/trust/yön/geç
> kalmanın hiçbirini etkilemez; (3) CLAUDE.md §7: dış girdi **handler sınırında**
> doğrulanır, domain zaten geçerli veri görür. "Sessiz kabul yok" kuralı korunur:
> `channel='manual'` + boş `entered_by` **M5-05 (orkestrasyon)** yazma yolunda
> reddedilir ve entered_by giriş yapan admin'den otomatik doldurulur
> (**M6-04**, m6-dashboard.md §165 ✓). M4-06 yalnızca manual kanalın **SUN
> aranmadan** (sys:sun-invalid NFC-only) karara bağlandığını doğrular
> (`TestDecide_ManualChannelSkipsSUN`). Trust ve practice `Decision`'a `decide.go`'da
> eklendi (`trustScore`, `isPracticeTap` saf yardımcılar); practice **yalnız**
> `Employee.ActivatedAt` + `LastForPerson`'dan türer, `Input`'ta istemci practice
> alanı **yoktur** (yapısal kanıt: `TestInput_HasNoClientPracticeField`).

**Amaç.** Kalan üç kuralı uygulamak.

**Kurallar.**
- **Trust:** `20 (taban) + 50 (IP eşleşti) + 30 (GPS eşleşti)` → 20/50/70/100.
- **QR kanalı** (`channel='qr'`): SUN yok → **IP zorunlu**, GPS tek başına
  yetmez → `flag` (Q15, `base:qr-requires-ip`). QR fotoğraflanır ve süresiz
  geçerlidir; hiçbir fiziksel dokunuş kanıtı taşımaz.
- **Manuel** (`channel='manual'`): `entered_by` dolu, SUN aranmaz, raporlarda ayrı.
- **Practice tap:** aktivasyon sonrası ilk kayıt `practice=true`, TRAINING
  damgası, çalışılan saate **asla** sayılmaz.
- **`practice` ve `channel` SUNUCUDA türetilir, istemciden okunmaz.** handoff §8
  API taslağı `POST /api/checkin` gövdesinde `practice?` gösteriyor — bu
  sömürülebilir: çalışan 09:00'da giriş yapar, 12:00'de çıkarken gövdeye
  `practice=true` koyar → kayıt yön zincirine girmez → 09:00 girişi **açık
  kalır** → 20:00'deki gerçek çıkış onunla eşleşir → 3 saat çalışıp 11 saat
  raporlanır. Doğrusu: `practice` `employees.activated_at` sonrası ilk kayıttan,
  `channel` `ctr`/`cmac` varlığından türetilir. Aksi hâlde `channel='nfc'`
  iddiasıyla `base:qr-requires-ip` de atlatılır.

**Kabul kriterleri.**
- Dört trust değeri de testli.
- **QR + IP yok → `flag`, GPS eşleşse bile** (Q15). Ayrı vaka: QR + IP yok +
  GPS var → `flag`; QR + IP var → `ok`.
- `practice` ve `channel` istemci gövdesinden okunmuyor — testte gövdeye
  `practice=true` konsa bile sunucu kendi türettiğini kullanıyor.
- `practice=true` kayıt yön zincirine girmiyor ve saat toplamına katılmıyor.
- `manual` kayıtta `entered_by` boşsa **hata** — sessiz kabul yok.

**Tuzaklar.**
- Trust'ı verdict'ten türetme; ikisi bağımsız. `flag` bir kayıt trust 20 taşır,
  `ok` bir kayıt 50 taşıyabilir (GPS ile doğrulanmış).
- Practice tap'i ayrı bir "eğitim modu" state'ine bağlama; sadece bir bayrak.

---

## M4-07 — Tablo bazlı test seti

- **Bağımlılık:** M4-03 … M4-06
- **Kırmızı çizgi:** §8 (kapsam %90+)
- **Commit:** `test(tap): cover every decision row`

**Amaç.** §5 tablosunun her satırının ve her kenar durumun kanıtı.

**Kabul kriterleri.**
- §5'teki **yedi satırın her biri** için `TestDecide_…` ile başlayan en az bir vaka.
- Tablo bazlı yapı: `tests := []struct{ name string; in Input; want Decision }`.
- Ek zorunlu vakalar:
  - Ardışık **farklı kişiler**, aynı plaket, 10 sn arayla → **hepsi** `ok`
    (debounce kişi bazlı, etiket bazlı değil).
  - Aynı kişi 20 sn içinde → `ignored`.
  - Mobil veri: IP eşleşmiyor, GPS ✓ → `ok`, trust 50, not `verified via GPS`.
  - Rusty Bar gece vardiyası tam turu.
  - Deaktive çalışan → `reject` + `Security = true`.
- `internal/domain/tap` kapsamı **%90+** (`make cover`).
- Denetim: agent `tappa-security-auditor` R6 ve R8 temiz.

**Tuzaklar.**
- Testleri `Now` sabitleyerek yaz; `time.Now()` kullanan test gece yarısı kırılır.
- "Aynı plaket art arda farklı kişiler" vakası olmadan etiket bazlı debounce
  hatası fark edilmez — ve bu hata kuyruktaki ikinci kişinin mesaisini düşürür.
