# ADR 0008 — "Son açık giriş" sorusu practice satırını sorguda eler

- **Durum:** kabul edildi
- **Tarih:** 2026-08-02
- **Karar veren:** yapıcı ajan, [M5-11](../plan/m5-tap-akisi.md#m5-11--practice-satırı-açık-girişi-maskeliyor-5-yön-kuralı-ihlali).
  Kusuru M5-09'un yapıcısı buldu, iki denetçi doğruladı, **kullanıcı 2026-08-02'de
  "şimdi düzelt, kendi görevi olsun" dedi.**
- **Etkilenen:** [`db/queries/transactions.sql`](../../db/queries/transactions.sql)
  → `GetLastOpenTransaction` · `internal/domain/checkin` (gather) ·
  `internal/domain/tap` (`resolveDirection`, `isPracticeTap` — **davranış
  değişmedi, yorumlar ölçüme eşitlendi**) · [CLAUDE.md](../../CLAUDE.md) §5 yön
  kuralı · `internal/handler/day_db_test.go` (M5-09'un workaround'u **kaldırıldı**)
  · [M6-07 / M6-11](../plan/m6-dashboard.md) "unutulmuş çıkış" anomali listesi
- **Migration YOK.** `transactions.practice` sütunu 00005'ten beri var; bu bir
  **sorgu** kararıdır.

---

## Bağlam

[CLAUDE.md](../../CLAUDE.md) §5, yönü tek cümleyle tanımlar:

> **Yön (in/out):** kişinin **son açık girişine** göre toggle — takvim gününe göre
> DEĞİL.

Aynı bölüm practice tap'i de tanımlar: *"aktivasyon sonrası ilk kayıt
`practice=true`, TRAINING damgası, çalışılan saate **asla** sayılmaz."* Bir
training tap'in zinciri açık tutmaması bu ikinci cümlenin doğrudan sonucudur —
tutsaydı, sonraki gerçek tap bir **çıkış** olur ve kimsenin çalışmadığı bir aralık
saatlere girerdi (M4-06'nın "hours inflation" açığı).

İki cümle birlikte tek bir soru sorar: **"bu kişinin son açık, practice OLMAYAN
girişi hangisi?"** M5-11'e kadar bu soru iki yarıya bölünmüştü ve ikinci yarı
birinciyi göremiyordu.

## Ölçülen kusur

`GetLastOpenTransaction` **tek** satır döndürüyordu (`type='in'`, sonrasında `out`
yok, `ORDER BY occurred_at DESC LIMIT 1`) ve `practice`'e **kördü**. Tüketici
(`internal/domain/checkin`, `gather`) dönen satır practice ise onu **atıyor ve
altındakine bakmıyordu** — bakamazdı da, elinde tek satır vardı.

Sonuç: yalnızca **daha yeni sıralanan** bir practice satırı, gerçek ve hâlâ açık
bir girişi **görünmez** kılıyordu.

### Üç sonda

**(1) Kontrol kolu / bozuk kol.** Kişi başına üç kayıt; iki kol arasındaki **tek**
fark practice satırının `occurred_at`'i. Gerçek giriş her iki kolda da 3 saat
öncesine beyan edildi.

| kol | practice'in zamanı | üçüncü tap'in yönü | kalan açık giriş |
|---|---|---|---|
| kontrol | gerçek girişten **eski** (−4 sa) | `out` ✅ | **0** |
| bozuk | gerçek girişten **yeni** (−100 sn) | `in` 🔴 | **2** |

**(2) Gün testinin kendisi.** M5-09'un `TestSeedDB_ADayAtKFStJulians`'ı Ivan'ın
practice tap'ini `declaring(rbGPS, night.practice)` ile **17:50'ye beyan
ediyordu** — yani fixture, motorun etrafından dolaşıyordu. Bu ADR'nin
düzeltmesini geri alıp workaround'suz koştuğumuzda gün **63,99 sn**de kırmızıya
düşüyor:

```
--- FAIL: TestSeedDB_ADayAtKFStJulians (63.99s)
    day_db_test.go:579: Ivan's 02:10 tap = in, want out — the overnight shift is not a calendar day
```

**(3) Sorgunun kendi sınırında.** `gather`'a doğrudan üç geçmiş şekli verildi
(gerçek Postgres, `internal/domain/checkin`): practice **altta** → gerçek satır
teslim ediliyor; practice **üstte** → düzeltme öncesi `nil`; **iki** practice
üst üste → düzeltme öncesi yine `nil`.

### İhlal edilen cümle

§5'in *"son açık girişine göre toggle"* cümlesi. Bozuk kolda çıkış `in` olarak
yazıldı, gerçek giriş **hiç kapanmadı** — ve **hiçbir sinyal yok**: `verdict='ok'`,
not yok, flag yok. Müdüre bu, §5'in *"unutulmuş çıkış"* anomalisi gibi görünür,
yani **çalışanın hatası** gibi.

### Erişilebilirlik

Kötü niyet gerekmiyor. `occurred_at` **sevk edilmiş** bir form alanıdır ve
`sys:occurred-at-bound` tavanı **72 saattir** (`policy.OccurredAtSkewMaxSeconds`,
[ADR 0004](0004-policy-motoru-modeli.md) §11). Sıradan bir practice tap + geriye
tarihli tek bir giriş yeterli. Ve bu şekil tam olarak **M9-01 çevrimdışı
kuyruğunun** üreteceği şekildir.

### 🔴 Asıl kusur bir YORUMDU

`internal/domain/tap/decide.go`, `resolveDirection`'ın üstünde, M4'ten beri şunu
yazıyordu:

> *"Primary enforcement is **the caller's query, which excludes practice** so a
> practice record is never passed as LastOpenIn."*

Sorgu practice'i **dışlamıyordu**. Yani dosya, sistemin vermediği bir garantiyi
beyan ediyordu; "birincil koruma" diye tarif edilen şey **yoktu** ve gerçek koruma
(gather'daki `if !open.Practice`) tek satırlık bir pencereden bakıyordu. Bu, bu
projenin en pahalı bulgu sınıfıdır ([agent-brief](../plan/agent-brief.md) →
"Şimdiye kadar öğrenilenler") ve burada **gerçek bir hataya** yol açtı.

Düzeltme, o cümleyi **doğru hâle getirmeyi** seçti (bkz. aşağıdaki alternatifler).

### Kartın "gerçek hayat senaryosu" YANLIŞTI — ölçüldü

M5-11 kartı kusuru şöyle örnekliyordu: *"Çalışan giriş yapar → telefonunu kaybeder
→ yeni cihazda yeniden aktive olur → çıkış için dokunur. Aktivasyon sonrası ilk
kayıt **tanımı gereği** `practice=true`."*

Ölçüm (gerçek Postgres, gerçek HTTP aktivasyon akışı; 0,26 sn):

```
ilk kayıt:                                   practice=true  type=in
yeniden aktivasyon (M5-02 ikinci cihaz yolu) -> /activate/done, 200
activated_at  önce=2026-07-03T15:06:31.937191Z
              sonra=2026-07-03T15:06:31.937191Z   (hareket etmedi)
yeniden aktivasyondan sonraki ilk kayıt:     practice=FALSE type=in
```

`isPracticeTap` **"her aktivasyondan sonraki ilk kayıt"** değil, **"kişinin HİÇ
kaydı yok"** demektir (`in.LastForPerson == nil`). Üstelik `activated_at` ikinci
aktivasyonda **ezilmiyor** (`ConsumeInviteAndActivate` `COALESCE` kullanıyor,
[db/queries/invites.sql](../../db/queries/invites.sql) bunu zaten yazıyordu). İki
bağımsız sebep, her biri tek başına yeterli.

Bu, kusuru teorik yapmaz — kusur düz HTTP'den erişilebilirdi. **Kartın rotasını**
yanlış yapar, ve bir hata raporundaki yanlış rota, düzeltmenin hiç sorun olmayan
şeyi korumasıyla biter. Kart tarihli bir blokla düzeltildi; ölçüm
`TestSeedDB_ASecondActivationIsNotASecondPracticeRun` olarak pinlendi.

## Karar

**`GetLastOpenTransaction`'ın DIŞ `WHERE`'ine `AND NOT t.practice` eklenir.**
Kart üç seçenek veriyordu; seçilen 1. seçenektir.

```sql
WHERE t.tenant_id = @tenant_id
  AND t.employee_id = @employee_id
  AND t.type = 'in'
  AND NOT t.practice            -- ADR 0008
  AND NOT EXISTS ( ... o.type = 'out' AND o.occurred_at > t.occurred_at )
ORDER BY t.occurred_at DESC
LIMIT 1;
```

Dört bağlı karar:

1. **Yüklem yalnız DIŞ satırdadır.** `NOT EXISTS` practice'e **kör kalır**, çünkü
   başka bir soru sorar: *"bu giriş sonradan kapatıldı mı?"* — ve kapatan bir
   `out`, hangi bayrağı taşırsa taşısın kapatır. Bugün zaten hükümsüzdür: bir
   practice satırı **daima `type='in'`**'dir (aşağıdaki invaryant).

2. **Practice ⟹ `in` bir İNVARYANTTIR ve pinlendi.** `isPracticeTap`
   `LastOpenIn == nil` ister; `resolveDirection` `TypeOut`'u yalnız
   practice-olmayan bir `LastOpenIn` varken döndürür. İkisi de `Decide`'ın **aynı
   dalında**, **aynı `Input`**'tan hesaplanır → practice bir `out` olamaz.
   `TestDecide_PracticeIsAlwaysAnIn` bunu **liste değil özellik** olarak
   doğruluyor (72 kombinasyon: geçmiş şekli × aktivasyon × kanıt × kanal) ve iki
   **boş-değil (non-vacuity)** sayacı taşıyor — hiç practice üretilmezse veya hiç
   `out` üretilmezse test kendi kendini geçersiz ilan eder.

3. **Go tarafındaki iki koruma KALIYOR, ama yorumları ölçüme eşitlendi.**
   `gather`'daki `if !open.Practice` artık **yanlış olamaz** (sorgu o satırı
   döndürmüyor) — dosya bunu açıkça yazıyor ve *"düzeltmeyi pinleyen bu değil"*
   diyor. `resolveDirection`'ın `open.Practice` dalı ve `isPracticeTap`'in
   `LastOpenIn == nil` şartı, saf motorun **çağıran-bağımsız** sözleşmesidir ve
   elle kurulmuş bir `Input`'la hâlâ erişilebilir; testleri de bundan fazlasını
   iddia etmiyor.

4. **Sorgu artık anomali sayımıyla aynı şeyi söylüyor.**
   `internal/handler/seedflow_db_test.go` → `openCheckIns` zaten `AND NOT
   t.practice` taşıyordu; "açık giriş" iki farklı yerde iki farklı şey demekten
   çıktı.

## Bugün yazılmış, kapanmamış açık kayıtlara ne olacak?

**`transactions` immutable'dır (§4.3): hiçbir satır UPDATE veya DELETE
EDİLMEYECEK.** Düzeltme yalnızca **ileriye dönüktür**.

- **Geçmiş satırlar düzeltilmez.** Bu ADR'nin değiştirdiği şey bundan sonraki
  kararlardır; hâlihazırda `in` olarak yazılmış bir çıkış `in` kalır.
- **Düzeltme yolunun ŞEKLİ emredilmiştir; MEKANİZMASININ YARISI HENÜZ YOKTUR.**
  §5 ne yapılacağını söylüyor: sistem çıkış kaydı **üretmez**, açık kayıt bir
  **anomali** olarak listelenir, **müdür manuel bir kayıt girer**
  (`channel='manual'` + `entered_by`), ve bu **yeni bir satırdır** — UPDATE değil
  (§4.3). Bu ADR bunu yeni bir şey olarak icat etmiyor; **açıkça söylüyor**, çünkü
  söylenmezse bir sonraki okuyucu bir migration yazmaya kalkışır.

  🔴 **Ama "emredilmiş" ile "var" aynı cümle değildir, ve bu ADR'nin ilk taslağı
  ikisini birbirine karıştırdı** — yani yukarıdaki [Asıl kusur bir
  YORUMDU](#-asıl-kusur-bir-yorumdu) bölümünde *"bu projenin en pahalı bulgu
  sınıfı"* diye adlandırılan hatanın (sistemin vermediği bir garantiyi beyan
  etmek) **aynısını kendi içinde tekrarladı**. Taslak *"…ve bu yeni
  bir satırdır **+ `audit_log`**"* diyordu; `audit_log` tarafı **yoktu**. Ölçüldü
  (dev/seed veritabanı, 2026-08-02):

  | bileşen | bugün | kanıt |
  |---|---|---|
  | `channel='manual'` + `entered_by` **domain yolu** | ✅ **VAR** | `checkin.Service.Record`; `entered_by` boşsa `ErrEnteredByRequired` ile **reddediliyor** (`checkin.go`) |
  | manuel kayıt için **`audit_log` satırı** | 🔴 **YOK** | `manual` kanallı **408** satır var, bunları hedefleyen `audit_log` satırı **0**, manuel kayıtla ilgili `action` **0** (dağıtılmış action'lar yalnız `activation.*`, `tap.security_alert`, `tap.rate_limited`, `tap.unknown_tag`, `invite.*`) |
  | manuel giriş için **HTTP rotası** | 🔴 **YOK** | rotaların tamamı: `/healthz`, `/static/*`, `/t`, `/activate`, `/activate/tour`, `/activate/done`, `/api/activate`, `/api/checkin` |
  | **anomali listesi** | 🔴 **YOK** | M6-07 / M6-11 yazılmadı |

  `checkin.go` içinde `audit.Event` **yalnız iki** yerde yazılıyor
  (`tap.unknown_tag` ve `tap.security_alert`); manuel kayıt yolunda **hiç yok**.

  ⚠️ **M6-04'ü (manuel giriş ekranı) yazan kişiye:** `audit_log` yazımı ve HTTP
  yüzeyi **senin işin** — bu ADR'nin devrettiği bir varsayım değil. §4.3 *"düzeltme
  = yeni kayıt **+ `audit_log`**"* diyor; bugün o `+` işaretinin sağ tarafı boş.
  Ve bunu yakalayacak **mekanik bir kontrol yok**: `scripts/redline-check.sh`
  `audit_log`'a bakmıyor.
- **Kaç satır etkilendi (ölçüldü, dev/seed veritabanı, 2026-08-02):**

  | ölçüm | sayı |
  |---|---|
  | toplam `transactions` satırı | **24 561** |
  | `practice=true` satır | **4 555** |
  | açık (practice olmayan, kapatılmamış) giriş | **2 502** |
  | bunlardan **practice tarafından maskelenmiş** olanlar | **13** |
  | maskelenmişlerden **bu görevin sondalarından ÖNCE** yazılmış olanlar | **0** |

  On üçünün hepsi **bugün, bu görevin kendi ölçümleri** tarafından yazıldı ve
  adlarından tanınıyorlar: `M511 Probe …` (silinen geçici sonda), `Rita Zammit`
  (`chainFixture`, şekli **bilerek** elle yazan yeni test), `Practice Chain …` ve
  `Ivan Petrov [sim …]` (mutasyon koşusundaki gün testi). **Üretimde hiçbir
  dağıtım yok**, yani bugün gerçek bir çalışanın kaybolmuş saati yok.
  ⚠️ `chainFixture` her `make test`'te bu şekilden **iki satır daha** bırakır
  (§4.3 + `REVOKE DELETE` → temizlenemez). Bu, M3-02'de yazılmış olan sonucun
  aynısıdır ve M6-11'in anomali listesi yazılırken bilinmelidir.

  📌 **Bu sayılar bir ANLIK GÖRÜNTÜDÜR ve yalnız BÜYÜRLER — aşağıda 47 göreceksin,
  çelişki değil.** Aynı gün, birkaç `make test` sonra tekrar ölçüldü: toplam
  **28 689**, practice **5 297**, açık giriş **3 150**, maskelenmiş **47**. Artışın
  tamamı yukarıdaki `chainFixture` satırlarıdır — *"bu görevin sondalarından önce
  yazılmış"* sayısı **0** olarak kalır, ki tabloda hukuken önemli olan tek satır
  odur. Bir sayı tutmuyorsa önce kaç `make test` koştuğuna bak.
- **Ne garanti edilmiyor:** bu düzeltme, kapanmamış kayıtları **bulmayı**
  kolaylaştırmaz. `openCheckIns` şeklindeki sorgu bir kaydın *neden* açık
  kaldığını (unutulmuş çıkış mu, maskelenmiş giriş mi) **söyleyemez** — ikisi de
  aynı satır şeklidir. Ayırt etmek isteyen bir rapor, practice satırının
  `occurred_at`'ini ayrıca okumak zorundadır; bu M6-07/M6-11'in işidir ve o
  kartlara not düşüldü.

## Taranan satır maliyeti (ölçüldü)

`EXPLAIN (ANALYZE, BUFFERS)`, tek kişi için **5001 satır**, rollback'li
transaction, `transactions_tenant_employee_occurred_idx` yerinde:

| şekil | yüklem | buffers | taranan satır | süre |
|---|---|---|---|---|
| C — kişi şu an **içeride**, practice en üstte (**kusur şekli**) | yok | 7 | 1 (**yanlış satır**) | 0,389 ms |
| C — aynı | **var** | **9** | 2 (**doğru satır**) | 0,126 ms |
| A — kişi şu an **dışarıda**, practice en üstte | yok | 9 | 1 | 0,101 ms |
| A — aynı | **var** | **16 090** | 5001 | 13,310 ms |
| B — kişi **dışarıda**, practice satırı **YOK** | yok | 10 246 | 5000 | 12,223 ms |

**Yüklem taramayı DARALTMIYOR, yalnız sonucu filtreliyor.** `practice` indekste
olmadığı için plan onu her koşuda `Index Cond` değil **`Filter: ((NOT practice)
AND (type = 'in'::text))`** olarak uyguluyor — yani zaten orada olan `type`
filtresinin yanına. Bu, M5-08'in `SecondsSinceLastRecordedTap` için ölçtüğü
sonucun aynısıdır.

Değişen şey **`LIMIT 1`'in nerede durduğudur**. A satırındaki 9 → 16 090 sıçraması
yeni bir en-kötü durum **değildir**: practice satırı oraya **yanlış satırı
döndürerek** erken çıkış satın alıyordu, ve aynı kişinin practice satırı olmayan
hâli (B) **bugün zaten** 10 246 buffer ödüyor. Yani bu maliyet, sıradan her
çalışanın her check-in tap'inde **hâlihazırda** ödenen maliyettir.

### ⚠️ Yukarıdaki tablo KONTROLLÜ DEĞİL — kontrollü ölçüm ayrı

Bir denetim bulgusu: yukarıdaki tablo A ile B'yi karşılaştırarak *"yeni bir en-kötü
durum yok"* diyor, ama A (5001 satır) ile B (5000 satır) **iki farklı koldur**. İki
farklı kolun farkı bir **fixture artefaktı** olabilir; cümlenin kendi kanıtına
oturması için **aynı kolun** yüklemli ve yüklemsiz hâli lazım. Ölçüldü (2026-08-02,
ikinci koşu, büyümüş veritabanı — mutlak sayılar yukarıdakiyle
**karşılaştırılamaz**; karşılaştırılacak olan **satır içi** farktır):

| kol | yüklemsiz | yüklemli | fark |
|---|---|---|---|
| **B — kişi dışarıda, practice satırı YOK** | 11 300 buffer | **11 300 buffer** | **0 — yüklem bedava** |
| **D — sıradan çalışan, practice EN ALTTA (en eski)** | 10 249 buffer | **10 245 buffer** | **−4 — yüklem UCUZLATIYOR** |

D'de yüklem **kâr ettiriyor**, çünkü en alttaki practice satırı için `NOT EXISTS`
alt sorgusu artık hiç koşmuyor: taranan küme `rows=2501 → 2500`. B'de hiç practice
satırı olmadığı için yüklem **hiçbir şeyi** değiştirmiyor — buffer sayısı birebir
aynı. Yani "%57 daha pahalı" diye okunabilecek A/B farkı gerçekten bir **kol
farkıydı**, yüklemin bedeli değil.

### Üç ölçekli bağımsız doğrulama (güvenlik denetimi)

Plan her ölçekte aynı: `Nested Loop Anti Join`, iki taraf da
`transactions_tenant_employee_occurred_idx` üzerinde `Index Scan`; taranan küme
`rows=2501 → 2500`, yani **değişmiyor**. Süreler:

| N | SIRADAN önce | SIRADAN sonra | practice-EN ÜSTTE önce | sonra |
|---|---|---|---|---|
| 5 000 | 10,8 ms | **9,7 ms** | 2,1 ms | 11,6 ms |
| 20 000 | 57,3 ms | **60,6 ms** | 9,9 ms | 102,4 ms |
| 50 000 | 184,1 ms | **189,5 ms** | 19,3 ms | **164,5 ms** |

**Bu ADR'nin kendi tekrar ölçümü (N = 50 000, 2026-08-02)** aynı sonucu veriyor:

```
SIRADAN           yüklemsiz  150,45 ms / 101 443 buffer  (rows=0)
SIRADAN           yüklemli   150,79 ms / 101 443 buffer  (rows=0)  <- buffer BIREBIR AYNI
practice-EN USTTE yüklemsiz   16,19 ms /   2 675 buffer  (rows=1 — YANLIS SATIR)
practice-EN USTTE yüklemli   123,36 ms / 102 671 buffer  (rows=0 — DOGRU CEVAP)
```

🔑 **Kritik satır:** 50 000'de practice-en-üstte **yüklemli** hâl (164,5 ms;
tekrarda 123,4 ms) **sıradan çalışanın şeklinin ALTINDA** kalıyor (189,5 ms;
tekrarda 150,8 ms). Yani *"bu maliyet, sıradan her çalışanın zaten ödediği
maliyettir"* cümlesi **ölçülerek doğrulandı** — abartı değil. Yüklemsiz koldaki
16-19 ms'lik "ucuzluk" bir kazanç değil, **yanlış satırı döndürmenin** fiyatıdır
(`rows=1`, plan çıktısında görünüyor).

**Ve bu bir DoS vektörü DEĞİL.** Ölçek ~N^1,2, yani `middleware.Timeout(30s)`'e
ulaşmak **kişi başına ~3 milyon satır** ister. `BySession` limiti 300 istek/10 dk
olduğuna göre bu **iki aylık kesintisiz flood** demektir; üstelik zarar
**saldırganın kendi hesabıyla** sınırlıdır: sorgu tek bir `employee_id` için koşar
ve karar+kayıt **kişi başına advisory kilit** altındadır (ADR 0006). Kendi
tap'lerini yavaşlatmaktan başka bir şey yapamaz.

**Devredilen ölçüm:** taranan küme §4.3 gereği yalnız **büyür** (bugünkü dev
veritabanında kişi başına en çok **40** satır var, yani üretimde bu sayılar
tek/çift haneli). Ucuzlatmak bir **indeks** sorusudur — `(tenant_id, employee_id,
occurred_at DESC) WHERE NOT practice` gibi bir kısmi indeks veya `type`'ın indekse
alınması — ve indeks eklemek bir **migration**'dır, M5-11'in kapsamında bilinçli
olarak **değildir**.

## Değerlendirilen alternatifler

| Alternatif | Neden seçilmedi (ölçümle) |
|---|---|
| **(2) Sorgu N satır döndürsün, tüketici practice'leri atlasın** | Sorunu Go'ya taşır, çözmez: "kaç satır" sorusuna cevap yok. **Ölçüldü:** 5001 satırlık geçmişte açık girişi olmayan bir kişide sorgu **5000 satırın hepsini** üretmek zorunda (B kolu, 10 246 buffer) — çünkü "yok" cevabı ancak sonuna kadar bakınca verilir; `LIMIT N` konursa N'inci satırın altındaki gerçek giriş **yine kaçırılır**, yani aynı hatanın parametreli hâli olur. Ayrıca "son açık giriş" tanımı iki yere bölünmeye devam ederdi — kusurun kök sebebi tam olarak buydu. |
| **(3) Practice satırı hiç `type` taşımasın** | Üç ölçülebilir sonuç, üçü de olumsuz. (a) **Geçmişi düzeltmez** (§4.3): bugünkü 4 555 practice satırı `type='in'` taşımaya devam eder, yani kusur mevcut veri üzerinde **açık kalır** — oysa seçilen çözüm eski satırlarda da doğru cevabı verir. (b) **M5-07/M4-06 semantiğini kırar:** `TestSeedDB_ADayAtKFStJulians` (satır 291) ve `TestDecide_APracticeTapCanStillFlag` practice tap'in `ok/in` olmasını iddia ediyor; onay ekranı "Tapped **in**" yazıyor — yön taşımayan bir practice tap, çalışanın gördüğü ilk ekranı **anlamsızlaştırır**. (c) `transactions_ok_has_direction` CHECK'i (`verdict <> 'ok' OR type IS NOT NULL`) bir `ok` practice satırının `type`'ını **NULL yapmayı veritabanı düzeyinde reddediyor** — yani seçenek, ayrıca bir **migration** gerektirirdi. |
| **Yorumu gerçeğe indir, sorguyu bırak** (M5-01/M5-02'de birkaç kez doğru olan hamle) | Burada **yanlış** hamle: yorum bir *iddiayı* abartmıyordu, **var olması gereken bir korumayı** tarif ediyordu ve koruma gerçekten gerekliydi. Yorumu indirmek §5 ihlalini belgelenmiş bir özellik hâline getirirdi. |
| **Gather'da ikinci bir sorgu** ("practice geldiyse bir daha sor") | Kilit altında ikinci bir gidiş-dönüş (ADR 0006 4. katman: karar + kayıt tek transaction'da, kişi başına advisory kilit altında) ve yine **kaç kere** sorulacağı sorusu. Tek `WHERE` yüklemiyle aynı cevabı veriyor. |
| **Indeksi de aynı anda değiştir** | Kapsam dışı: yeni indeks bir migration'dır ve M5-11 bir sorgu görevidir. Ölçüm yukarıda, devredildi. |

## Sonuçlar

- **CLAUDE.md §5 değişmedi.** Bu ADR §5'i uygular; §5'in yön cümlesi zaten
  doğruydu, kod ona uymuyordu.
- **M5-09'un workaround'u kaldırıldı.** `day_db_test.go`'da Ivan'ın practice
  tap'i artık `declaring(...)` taşımıyor ve `nightTimes.practice` alanı **silindi**
  (kullanılmayan bir workaround, geri getirilmeye davetiyedir). Gün workaround'suz
  **64,98 sn**de yeşil; düzeltme geri alınınca **63,99 sn**de kırmızı.
- **LIMITS L3 kapandı** (`day_db_test.go` sonu) ve M5-09 kart düzeltmesi md. 6
  kapandı — ikisi de tarihli, ikisi de pinin nerede olduğunu adıyla söylüyor.
- **Yeni testler:** `TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn`
  (kontrol/bozuk, üretim yazma yolu) · `TestSeedDB_ASecondActivationIsNotASecondPracticeRun`
  · `TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn` (sorgunun sınırı, iki
  ardışık practice dâhil) · `TestDecide_PracticeIsAlwaysAnIn` (invaryant).
- **[M6-07 / M6-11](../plan/m6-dashboard.md):** "unutulmuş çıkış" anomali listesi
  bu düzeltmeden sonra **daha az** satır görecek; o kartlara not düşüldü.
- **[M9-01](../plan/m9-sonrasi.md):** çevrimdışı kuyruk tam da bu şekli
  ürettiği için, kuyruk yazıldığında bu ADR'nin testleri regresyon ağıdır.

## Ne garanti edilmiyor

- **Practice satırının kendisi hâlâ "açık" bir `in` olarak durur** ve hiçbir zaman
  bir `out` ile kapanmaz — ham SQL'de sonsuza dek açık görünür. Onu anomali
  saymayan tek şey `NOT practice` yüklemidir; yeni bir rapor sorgusu bunu unutursa
  her çalışan için bir sahte "unutulmuş çıkış" üretir. Bunu zorlayan **mekanik bir
  kontrol yok** — ve `make audit` de bulamaz: `scripts/redline-check.sh` **kaynağı**
  (`db/queries/`) tarar ama sqlc **çıktısını** (`internal/store/*.go`) `GEN_EXCLUDE`
  ile taramanın dışında tutar. Bu doğru bir tercihtir (üretilen dosya kaynağın
  kopyasıdır, ihlal kaynakta yakalanır), ama tarayıcı kalıpları **kırmızı çizgi**
  kalıplarıdır — "bu rapor sorgusu `NOT practice` taşımalı" gibi **anlamsal** bir
  kural hiçbir katmanda kontrol edilmiyor.
- **`gather`'daki koruma artık ölçülemez.** Onu tek başına silmek `-short` süitini
  **ve** simüle edilen günü (67,26 sn) **yeşil bırakıyor** (ölçüldü). Kalması bir
  tercih, kanıt değil.

  ⚠️ **Ve gerekçesi ilk taslakta ZAYIF yazılmıştı — düzeltildi (2. tur).** Taslak
  *"ileride yüklem düşerse bu koruma davranışı eski okumada tutar"* diyordu. Bu
  **geçersiz bir gerekçe**: yüklem sessizce düşemez. Ölçüldü — yüklemi `AND TRUE`
  ile değiştirmek **iki pakette üç adlandırılmış testi** kırmızıya çeviriyor:
  `TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn` (iki alt vaka),
  `TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn` ve
  `TestSeedDB_ADayAtKFStJulians` (*"Ivan's 02:10 tap = in, want out"*).

  Korumanın **gerçek** değeri bir güvenlik ağı değil, **paket sınırındaki
  SÖZLEŞME**: `tap.Input.LastOpenIn` belgelenmiş hâliyle *"son açık, practice
  OLMAYAN giriş"*tir ve saf motor kendi girdisini doğrulayamaz — o cümleyi doğru
  yapan şey **çağıranın** bu satırıdır. İlke: **erişilemez ama DOĞRU ETİKETLENMİŞ
  kod, yanlış etiketlenmiş erişilebilir koddan güvenlidir** — bu ADR zaten ikinci
  türün faturasıdır (`resolveDirection`'ın eski *"primary enforcement"* yorumu).
  Bu, "erişilemez kodu tut" için genel bir ruhsat **değildir**; yalnızca
  belgelenmiş bir sözleşmeyi tutan satır için geçerlidir.
- **İki ardışık practice satırı motordan üretilemez**, yalnız elle/ithalatla
  yazılabilir. Test o şekli **kasten** elle kuruyor; yani o vakanın kanıtladığı şey
  sorgunun davranışıdır, üretimde ortaya çıkabileceği değil.
- **Bu ADR bir zaman doğrulaması getirmez.** `occurred_at` hâlâ tek doğrulanmamış
  istemci girdisidir (M5-05 K1) ve tavanı `sys:occurred-at-bound`'dur. Geriye
  tarihlemenin **başka** sonuçları bu düzeltmenin kapsamında değildir.
- 🔴 **Geriye tarihli bir `out` HİÇBİR ZAMAN kapatmaz — ve bu practice'ten
  bağımsızdır.** `NOT EXISTS` testi `o.occurred_at > t.occurred_at` olduğu için,
  girişten **önceye** tarihlenmiş bir çıkış onu kapatmaz. Ölçüldü (rollback'li):
  `in @13:16` altına `out @12:16` **ve** `out @11:16` yazıldı, ikisi de dangling
  kaldı, sorgu hâlâ 13:16'daki `in`'i döndürüyor — giriş **sonsuza dek açık**.
  Bu şekil **bu diff'in eseri değildir**, M5-11'den önce de vardı; burada
  yazılmasının sebebi, bir sonraki okuyucunun "açık kayıt" listesindeki bir satırı
  otomatik olarak *unutulmuş çıkış* sanmasını engellemektir. Hafifletici: geriye
  tarihli her satır `base:queued-window` (120 sn) ile **`flag`** alır, yani sinyal
  vardır. Devredildi: **M5-05 K1** torbası.
- **Anomali listesi maskelenmiş girişleri KALICI OLARAK göstermez.** Tek bir `out`
  kendinden eski tüm açık girişleri kapattığı için, maskelenmiş bir giriş kişinin
  bir sonraki çıkışıyla listeden düşer. Ölçüldü (2026-08-02): bu şekilden **47**
  satır hâlâ açık, **43** satır ise sonradan gelen bir `out` yüzünden **artık
  görünmüyor**. Geriye dönük tespit `p.practice AND p.occurred_at > t.occurred_at`
  sorgusunu gerektirir; devredildi: **M6-11**. ⚠️ **§4.6 ihlali değil** — hiçbir
  satır kaybolmuyor, kaybolan **sinyaldir**.
